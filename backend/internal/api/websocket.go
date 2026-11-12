package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"nhooyr.io/websocket"
)

const (
	// wsWriteTimeout bounds a single frame write so one wedged browser cannot
	// pin a Docker log stream open indefinitely.
	wsWriteTimeout = 5 * time.Second
	// wsStatusPollInterval is how often a live stream re-reads the sandbox row
	// to notice a hibernate or an expiry. The reapers write straight to
	// Postgres and publish nothing, so polling is the only signal available.
	wsStatusPollInterval = 5 * time.Second
	// wsSampleInterval replays the collector's last sample so a container that
	// generates no events still animates its charts at a steady rate.
	wsSampleInterval = 2 * time.Second
	// maxLogLineBytes caps a single log line so an unterminated write cannot
	// grow the scanner buffer without bound.
	maxLogLineBytes = 1 << 20
	// logTimestampLayout is the RFC3339 stamp Docker prefixes each line with
	// when the log request sets Timestamps: true.
	logTimestampLayout = "2006-01-02T15:04:05.999999999Z07:00"
)

// wsEvent is the single envelope both live sockets push. `type` tells the
// browser what to do; the remaining fields are the payload for that type.
//
// The four types are:
//   - "log"         one container line: stream, timestamp, line
//   - "stats"       one resource sample, flattened (no nested object)
//   - "unavailable" the sandbox is not running; nothing will be streamed
//   - "closed"      the stream ended while the sandbox was being watched
//
// The last two exist so the UI can show a real state instead of a frozen panel.
// The server never retries: reconnection with backoff is a frontend concern.
//
// Every payload field is optional on the wire because `type` decides which ones
// are meaningful — but the *numeric* ones are pointers for a sharper reason: with
// a plain `float64 json:"...,omitempty"`, a legitimate 0% CPU or 0 bytes of
// traffic disappears from the payload entirely and arrives at the browser as
// `undefined`. Those are the most common values there are (an idle container,
// and the first sample of any stream), so the chart would crash on
// `cpu_percent.toFixed()` the moment it opened. A nil pointer means "this event
// type has no such field"; a non-nil pointer to 0 means "zero, genuinely".
type wsEvent struct {
	Type      string     `json:"type"`
	Stream    string     `json:"stream,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	Line      string     `json:"line,omitempty"`
	Reason    string     `json:"reason,omitempty"`

	CPUPercent       *float64   `json:"cpu_percent,omitempty"`
	MemoryUsedBytes  *int64     `json:"memory_used_bytes,omitempty"`
	MemoryLimitBytes *int64     `json:"memory_limit_bytes,omitempty"`
	NetworkRxBytes   *int64     `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes   *int64     `json:"network_tx_bytes,omitempty"`
	RecordedAt       *time.Time `json:"recorded_at,omitempty"`
}

// handleLogsWS streams a running sandbox container's stdout and stderr to the
// browser over GET /api/sandboxes/{id}/logs/ws.
//
// The stream ends for exactly four reasons, three of which are reported as a
// message rather than a dropped connection: the client disconnected, the
// sandbox left the running state (polled while streaming), Docker closed the
// stream, or the log stream could not be opened at all. Every exit path closes
// the Docker stream and waits for its pumps, so closing the panel repeatedly
// cannot accumulate goroutines.
func (s *Server) handleLogsWS(w http.ResponseWriter, r *http.Request) {
	ws, sb := s.upgrade(w, r)
	if ws == nil {
		return
	}
	defer ws.shutdown()

	if reason := unavailableReason(sb); reason != "" {
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: reason})
		return
	}
	s.touchSandbox(ws.ctx, sb)

	stream, err := s.orch.StreamContainerLogs(ws.ctx, sb.ContainerID)
	if err != nil {
		s.logger.Warn("logs: cannot open container log stream",
			slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: "could not open the container log stream"})
		return
	}

	// One pump per stream so stdout and stderr interleave as the container
	// produced them rather than being buffered and re-ordered.
	var pumps sync.WaitGroup
	for _, src := range []struct {
		name   string
		reader io.ReadCloser
	}{
		{"stdout", stream.Stdout()},
		{"stderr", stream.Stderr()},
	} {
		pumps.Add(1)
		go func(name string, reader io.ReadCloser) {
			defer pumps.Done()
			s.pumpLogLines(ws, name, reader)
		}(src.name, src.reader)
	}

	// Cleanup order matters: closing the stream unblocks any pump parked on a
	// pipe, and only then is it safe to claim no goroutine outlives us.
	defer func() {
		_ = stream.Close()
		pumps.Wait()
	}()

	drained := make(chan struct{})
	go func() {
		pumps.Wait()
		close(drained)
	}()

	// The sandbox can hibernate or expire while the panel is open. Docker keeps
	// streaming (or silently stalls) in that case, so watch the row and end the
	// stream ourselves with an explanation.
	status := time.NewTicker(wsStatusPollInterval)
	defer status.Stop()

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-drained:
			ws.sendTerminal(wsEvent{Type: "closed", Reason: "log stream ended"})
			return
		case <-status.C:
			s.touchSandbox(ws.ctx, sb)
			if reason := s.stoppedReason(sb.ID); reason != "" {
				ws.sendTerminal(wsEvent{Type: "closed", Reason: reason})
				return
			}
		}
	}
}

// pumpLogLines forwards one demultiplexed stream to the browser line by line.
// A dead connection just ends the pump; the status loop notices and cleans up.
//
// This reads with a bufio.Reader rather than a bufio.Scanner on purpose. A
// Scanner that meets a line longer than its buffer stops for good, and since it
// never reports why, one oversized line — a minified bundle, a base64 blob, a
// runaway stack trace — would silently truncate the rest of the container's
// output and tear the whole panel down. Here an over-long line is truncated to
// the cap, marked, and the pump carries on.
func (s *Server) pumpLogLines(ws *wsStream, name string, reader io.Reader) {
	br := bufio.NewReaderSize(reader, 64*1024)
	for {
		line, truncated, err := readLogLine(br, maxLogLineBytes)
		if err != nil {
			return // EOF, or the stream was closed under us
		}
		if len(line) == 0 && !truncated {
			continue // a bare newline: nothing to show
		}
		at, text := splitLogTimestamp(string(line))
		if text == "" && !truncated {
			continue
		}
		if truncated {
			text += " …[line truncated]"
		}
		if err := ws.send(wsEvent{Type: "log", Stream: name, Timestamp: &at, Line: text}); err != nil {
			// This is the end of the stream for this client, and it used to be
			// completely invisible: the pump returned, both pumps finished, the
			// terminal frame failed for the same reason, and the socket closed
			// with 1011. The browser then reported an indistinguishable
			// "connection lost" and reconnected in a loop. The close code is the
			// only thing that separates this from a real network drop, so record
			// it where a server log can be asked for it.
			s.logger.Warn("logs: dropping client that stopped reading",
				slog.String("sandbox", ws.sandboxID),
				slog.String("stream", name),
				slog.String("err", err.Error()))
			return
		}
	}
}

// readLogLine reads one newline-terminated line, discarding any bytes past max.
// It reports whether the line was truncated, and returns io.EOF only when there
// is nothing left to read.
func readLogLine(br *bufio.Reader, max int) (line []byte, truncated bool, err error) {
	var buf []byte
	for {
		chunk, isPrefix, readErr := br.ReadLine()
		if len(chunk) > 0 {
			if room := max - len(buf); room > 0 {
				if len(chunk) > room {
					buf = append(buf, chunk[:room]...)
					truncated = true
				} else {
					buf = append(buf, chunk...)
				}
			} else {
				truncated = true
			}
		}
		if readErr != nil {
			if len(buf) == 0 {
				return nil, false, readErr
			}
			// A final line with no trailing newline is still a line.
			return buf, truncated, nil
		}
		if !isPrefix {
			return buf, truncated, nil
		}
	}
}

// statsReplayGuard keeps one client from being sent the same sample twice.
//
// A stats handler has two sources feeding it: the collector's channel, and a
// ticker that replays the newest sample so an idle container still looks live.
// Both can be ready in the same select iteration and Go picks pseudo-randomly
// among ready cases, so the replay can land straight after the very sample that
// was already queued. Sent unguarded, the browser receives it twice roughly half
// the time and its rolling window fills with repeated timestamps, so the chart
// looks like it is sampling faster than the container is.
//
// Only strictly-newer samples pass. The zero value is ready to use, which is why
// the handler declares a bare one.
type statsReplayGuard struct {
	lastSent time.Time
}

// accept reports whether sample is newer than everything already accepted, and
// records it when it is.
func (g *statsReplayGuard) accept(sample orchestrator.ResourceSample) bool {
	if !sample.RecordedAt.After(g.lastSent) {
		return false
	}
	g.lastSent = sample.RecordedAt
	return true
}

// handleStatsWS streams live CPU/memory/network samples for a running sandbox
// over GET /api/sandboxes/{id}/stats/ws. It shares every access and lifecycle
// decision with the log socket; only the payload differs.
func (s *Server) handleStatsWS(w http.ResponseWriter, r *http.Request) {
	ws, sb := s.upgrade(w, r)
	if ws == nil {
		return
	}
	defer ws.shutdown()

	if reason := unavailableReason(sb); reason != "" {
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: reason})
		return
	}
	s.touchSandbox(ws.ctx, sb)

	collector, err := s.orch.EnsureStatsCollector(sb.ID, sb.ContainerID)
	if err != nil {
		s.logger.Warn("stats: cannot start collector",
			slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: "could not start the resource collector"})
		return
	}
	samples := collector.Subscribe()
	defer collector.Unsubscribe(samples)

	// Two sources: fresh frames from the collector, and a ticker replaying the
	// last frame so an idle container still looks live.
	fresh := time.NewTicker(wsSampleInterval)
	defer fresh.Stop()
	status := time.NewTicker(wsStatusPollInterval)
	defer status.Stop()

	// Both sources can be ready in the same select, and Go picks pseudo-randomly
	// among ready cases. The replay returns the collector's newest sample, which
	// is the very one already queued on the channel, so without this guard the
	// browser would receive that sample twice about half the time and the
	// rolling window would fill with repeated timestamps. Sending strictly-newer
	// samples only also makes the window honest: its length is real samples, not
	// samples-plus-replays.
	var guard statsReplayGuard
	sendNewer := func(sample orchestrator.ResourceSample) error {
		if !guard.accept(sample) {
			return nil
		}
		// Accepting before the send is equivalent to recording afterwards: a
		// failed send ends this handler, so the recorded value is never reused.
		return ws.send(statsEvent(sample))
	}

	for {
		select {
		case <-ws.ctx.Done():
			return
		case sample, ok := <-samples:
			if !ok {
				ws.sendTerminal(wsEvent{Type: "closed", Reason: "resource collector stopped"})
				return
			}
			if err := sendNewer(sample); err != nil {
				return
			}
		case <-fresh.C:
			if last := collector.Last(); last != nil {
				if err := sendNewer(*last); err != nil {
					return
				}
			}
		case <-status.C:
			s.touchSandbox(ws.ctx, sb)
			if reason := s.stoppedReason(sb.ID); reason != "" {
				ws.sendTerminal(wsEvent{Type: "closed", Reason: reason})
				return
			}
		}
	}
}

// statsEvent flattens a sample onto the wire. The browser reads one message
// shape rather than reaching into a nested object, and every measurement is
// always present — see the wsEvent note on why these are pointers.
func statsEvent(sample orchestrator.ResourceSample) wsEvent {
	at := sample.RecordedAt
	return wsEvent{
		Type:             "stats",
		CPUPercent:       &sample.CPUPercent,
		MemoryUsedBytes:  &sample.MemoryUsedBytes,
		MemoryLimitBytes: &sample.MemoryLimitBytes,
		NetworkRxBytes:   &sample.NetworkRxBytes,
		NetworkTxBytes:   &sample.NetworkTxBytes,
		RecordedAt:       &at,
	}
}

// splitLogTimestamp peels Docker's leading RFC3339 stamp off a line. Parsing it
// rather than stamping time.Now() on arrival matters because the stream opens
// with logTail lines of history: a receive-time stamp would label an hour-old
// line with the current time. An unparseable prefix is kept as part of the line
// so nothing is silently dropped.
func splitLogTimestamp(line string) (time.Time, string) {
	stamp, rest, found := strings.Cut(line, " ")
	if !found {
		return time.Now(), line
	}
	at, err := time.Parse(logTimestampLayout, stamp)
	if err != nil {
		return time.Now(), line
	}
	return at, rest
}

// unavailableReason explains why a live stream cannot start for this sandbox,
// or returns "" when it is running. Both live sockets share it so the log panel
// and the metrics chart always word the same state the same way.
func unavailableReason(sb *db.Sandbox) string {
	switch {
	case sb.DestroyedAt != nil:
		return "this sandbox has been destroyed"
	case sb.Status != db.StatusRunning:
		return fmt.Sprintf("sandbox is %s — live output resumes when it wakes", sb.Status)
	case sb.ContainerID == "":
		return "sandbox has no container yet"
	default:
		return ""
	}
}

// stoppedReason re-reads the sandbox row and reports why an in-flight stream
// should end, or "" while it is still running.
func (s *Server) stoppedReason(sandboxID string) string {
	sb, err := s.store.GetSandbox(sandboxID)
	if err != nil {
		s.logger.Debug("websocket: re-reading sandbox status",
			slog.String("sandbox", sandboxID), slog.String("err", err.Error()))
		return "sandbox is no longer available"
	}
	return unavailableReason(sb)
}

// touchSandbox refreshes the Redis idle-timeout marker so an active interactive
// session (logs stream, stats monitor, file editing) prevents the idle reaper
// from hibernating the container while the user is actively working.
func (s *Server) touchSandbox(ctx context.Context, sb *db.Sandbox) {
	if s == nil || s.cache == nil || sb == nil || sb.ID == "" {
		return
	}
	ttl := sb.IdleTimeoutSeconds
	if ttl <= 0 {
		ttl = 900 // default 15 minutes
	}
	_ = s.cache.Touch(ctx, sb.ID, ttl)
}

// --- connection plumbing --------------------------------------------------

// wsStream is one live browser connection: the socket, a context that dies the
// moment the client does, and a mutex so the concurrent senders on the log
// socket (two pumps plus the status poller) never interleave frames.
type wsStream struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc

	// sandboxID is carried so a goroutine that has lost its *Server (the log
	// pumps run long after the handler's locals are gone) can still say which
	// sandbox a failure belonged to.
	sandboxID string

	writeMu  sync.Mutex
	terminal atomic.Bool
}

// upgrade authorizes the caller and completes the handshake. It returns
// (nil, nil) after reporting the failure, so handlers start with
// `if ws == nil { return }`.
//
// Authorization deliberately happens before the upgrade, and a failure is
// reported as a 1008 policy-violation close rather than an HTTP status: the
// frontend has to be able to tell "you may not watch this sandbox" apart from
// "the network is down", and a refused handshake looks identical to an outage.
func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) (*wsStream, *db.Sandbox) {
	sb, err := s.authorizeSandboxAccess(r)
	if err != nil {
		s.rejectWebSocket(w, r, websocket.StatusPolicyViolation, "you do not have access to this sandbox")
		return nil, nil
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.logger.Debug("websocket: handshake failed",
			slog.String("path", r.URL.Path), slog.String("err", err.Error()))
		return nil, nil
	}
	ctx, cancel := context.WithCancel(r.Context())
	ws := &wsStream{conn: conn, ctx: ctx, cancel: cancel, sandboxID: sb.ID}
	go ws.watchClient()
	return ws, sb
}

// rejectWebSocket answers a request the caller may not have. It completes the
// handshake when it can, because that is the only way to deliver a close code;
// a plain request with no Upgrade header gets an ordinary 403 instead.
//
// Note that websocket.Accept has already written its own HTTP error response by
// the time it returns an error, so there is no second response to send here —
// writing one would only produce a "superfluous WriteHeader" log line.
func (s *Server) rejectWebSocket(w http.ResponseWriter, r *http.Request, code websocket.StatusCode, reason string) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.logger.Debug("websocket: refused a non-upgrade request",
			slog.String("path", r.URL.Path), slog.String("err", err.Error()))
		return
	}
	_ = conn.Close(code, reason)
}

// watchClient turns "the browser went away" into a cancelled context. Every
// Docker read, tick and sleep in these handlers is bound to ws.ctx, so closing
// the panel tears down its own backend stream instead of orphaning it.
func (w *wsStream) watchClient() {
	for {
		if _, _, err := w.conn.Read(w.ctx); err != nil {
			w.cancel()
			return
		}
	}
}

// send marshals and writes one frame under a short deadline, serialising the
// concurrent senders.
func (w *wsStream) send(ev wsEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(w.ctx, wsWriteTimeout)
	defer cancel()
	return w.conn.Write(ctx, websocket.MessageText, data)
}

// sendTerminal sends the last frame of a stream and records that the client was
// told why, so shutdown closes normally instead of reporting an error.
func (w *wsStream) sendTerminal(ev wsEvent) {
	if err := w.send(ev); err != nil {
		return
	}
	w.terminal.Store(true)
}

// shutdown cancels the stream context and closes the socket. A stream that
// already delivered a terminal event closes normally; anything else is an
// internal error the client should see as a dropped connection.
func (w *wsStream) shutdown() {
	w.cancel()
	if w.terminal.Load() {
		_ = w.conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	_ = w.conn.Close(websocket.StatusInternalError, "stream ended")
}
