package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// logTail is how much history a newly connected client receives before the
// stream starts following. Without it, a user who opens the log panel an hour
// into a container's life stares at a blank terminal until the app happens to
// print something.
const logTail = "200"

// logPipeBuffer is how much of one stream is held in memory before the demuxer
// has to wait on that stream's consumer.
//
// This buffer is the whole point of the type below. stdcopy.StdCopy is a single
// goroutine that writes each frame to stdout or stderr in turn, so with an
// unbuffered io.Pipe a client that stalls while reading stdout blocks the
// demuxer, and stderr lines sit in the Docker socket until stdout catches up.
// Since the log socket writes to the browser under a write deadline, "stalls"
// is a normal occurrence rather than an exotic one, and stderr arriving minutes
// after the fact is worse than no stderr at all. Buffering each stream
// independently means a slow consumer only applies back-pressure once its own
// buffer is full.
//
// Bounded rather than unbounded because the source is a container we do not
// control: a log-spamming process must not be able to grow this without limit.
const logPipeBuffer = 256 * 1024

// asyncPipe is a bounded, buffered, one-directional pipe: a writer that returns
// immediately while there is room, and a reader that blocks for data.
//
// It replaces io.Pipe for the demuxer's output, where io.Pipe's zero buffering is
// actively harmful. Closing is one-sided so the two halves can be torn down
// independently and in either order: CloseWrite is the writer signalling EOF,
// CloseRead is a consumer that has gone away and needs a parked reader released.
type asyncPipe struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  bytes.Buffer
	cap  int

	writeClosed bool
	readErr     error // set by CloseRead
}

func newAsyncPipe(size int) *asyncPipe {
	p := &asyncPipe{cap: size}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Write appends b, blocking only while the buffer is full.
func (p *asyncPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A single write larger than the whole buffer can never fit, so waiting for
	// room would deadlock rather than apply back-pressure: the condition below
	// would stay true no matter how much the reader drained. Callers chunk their
	// writes (io.Copy uses 32 KiB against a 256 KiB buffer), so this is a guard
	// against a future caller, and an error is a far better outcome than a
	// permanently parked goroutine.
	if len(b) > p.cap {
		return 0, fmt.Errorf("orchestrator: log pipe write of %d bytes exceeds its %d byte buffer", len(b), p.cap)
	}
	for {
		if p.readErr != nil {
			return 0, p.readErr
		}
		if p.writeClosed {
			return 0, io.ErrClosedPipe
		}
		if p.buf.Len()+len(b) <= p.cap {
			break
		}
		p.cond.Wait()
	}
	p.buf.Write(b)
	p.cond.Broadcast()
	return len(b), nil
}

// Read returns buffered data, blocking until some is available. A final short
// read is normal and not a sign of EOF; only an empty buffer after CloseWrite
// is EOF.
func (p *asyncPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if p.buf.Len() > 0 {
			n, _ := p.buf.Read(b)
			// Draining frees room, so a blocked writer can make progress.
			p.cond.Broadcast()
			return n, nil
		}
		if p.readErr != nil {
			return 0, p.readErr
		}
		if p.writeClosed {
			return 0, io.EOF
		}
		p.cond.Wait()
	}
}

// CloseWrite signals end-of-stream to the reader after it drains what is left.
func (p *asyncPipe) CloseWrite() error {
	p.mu.Lock()
	p.writeClosed = true
	p.mu.Unlock()
	p.cond.Broadcast()
	return nil
}

// Close releases a reader parked in Read and fails any subsequent writer, so
// a consumer that walked away cannot strand the demuxer mid-write. This is the
// io.ReadCloser half; the write side is closed by CloseWrite.
func (p *asyncPipe) Close() error {
	p.mu.Lock()
	p.readErr = errors.New("orchestrator: log pipe reader closed")
	p.mu.Unlock()
	p.cond.Broadcast()
	return nil
}

// LogStreamer is a demultiplexed view of one container's log output.
//
// Docker interleaves stdout and stderr into a single byte stream with an
// 8-byte header per frame whenever the container has no TTY (ours never do), so
// the raw reader is useless to callers. Hand-parsing the header is a reliable
// source of garbled output, so it is deliberately not an option; the frames are
// read with stdcopy instead.
//
// A LogStreamer owns two goroutines, one per source. Close stops them and
// releases both Docker connections, so callers must Close — otherwise every
// browser tab that opens and closes the log panel leaks two live ContainerLogs
// streams and two parked goroutines.
type LogStreamer struct {
	stdout *asyncPipe
	stderr *asyncPipe

	// One Docker connection per fd, opened separately so that Tail applies to
	// each stream on its own. See StreamContainerLogs for why a single
	// multiplexed connection cannot deliver a per-stream backfill.
	stdoutSrc io.ReadCloser
	stderrSrc io.ReadCloser

	closeOnce sync.Once
	closeErr  error
	pumps     sync.WaitGroup
	pumpDone  chan struct{}
}

// StreamContainerLogs opens a following stdout+stderr stream for a container and
// returns it already demultiplexed. ctx bounds the Docker request itself;
// closing the returned stream is the caller's job.
//
// This opens one connection per fd with a different tail policy on each, which is
// not the obvious way to write it, so the reasoning is worth stating. Docker's
// tail option is applied by the daemon to the *combined* log, before the result
// is filtered down to the requested streams. The combined backfill is ordered
// stderr first and stdout second.
//
// That makes tail a per-stream trap in both directions. Measured against Docker
// v28 on a container that writes 400 stdout lines and 9 stderr lines:
//
//	stdout-only, tail=200  -> the last 200 stdout lines          (correct)
//	stderr-only, tail=200  -> nothing at all                     (wrong)
//	stderr-only, tail=all  -> all 9 stderr lines                (correct)
//
// So stdout, which is the high-volume stream and sits last in the combined
// order, is the one tail can safely cut; stderr sits first, so any tail smaller
// than the whole log discards all of it. The obvious single-connection version
// of this is worse still: tail=200 there returns 200 stdout lines and no stderr,
// which silently hides the one error line a user opened the log panel to find.
//
// The residual cost of tail=all on stderr is an unbounded replay of that stream,
// which is acceptable here for two reasons: stderr is the low-volume stream in
// practice, and the replay is not buffered without limit — the pump writes into
// a bounded pipe, so a container with a huge stderr history applies back-pressure
// and parks the pump rather than growing memory.
func (o *Orchestrator) StreamContainerLogs(ctx context.Context, containerID string) (*LogStreamer, error) {
	opts := func(stderr bool) container.LogsOptions {
		o := container.LogsOptions{
			ShowStdout: !stderr,
			ShowStderr: stderr,
			Follow:     true,
			// Timestamps prefix every line with its RFC3339Nano stamp, which is
			// what lets the browser date the Tail backfill honestly. See
			// splitLogTimestamp in the API layer.
			Timestamps: true,
		}
		if stderr {
			o.Tail = "all"
		} else {
			o.Tail = logTail
		}
		return o
	}

	stdoutSrc, err := o.docker.ContainerLogs(ctx, containerID, opts(false))
	if err != nil {
		return nil, fmt.Errorf("orchestrator: opening container stdout for %s: %w", shortContainerID(containerID), err)
	}
	// A container with no stderr still yields a valid empty stream, so a failure
	// here is a real fault rather than "nothing to read".
	stderrSrc, err := o.docker.ContainerLogs(ctx, containerID, opts(true))
	if err != nil {
		_ = stdoutSrc.Close()
		return nil, fmt.Errorf("orchestrator: opening container stderr for %s: %w", shortContainerID(containerID), err)
	}

	stream := &LogStreamer{
		stdout:    newAsyncPipe(logPipeBuffer),
		stderr:    newAsyncPipe(logPipeBuffer),
		stdoutSrc: stdoutSrc,
		stderrSrc: stderrSrc,
		pumpDone:  make(chan struct{}),
	}

	// One pump per connection. Each carries only its own fd, so the other output
	// goes nowhere: routing it to io.Discard rather than to a pipe means a frame
	// the daemon should not have sent cannot land in the other stream.
	stream.pumps.Add(2)
	go stream.pump(stream.stdout, stream.stdout, io.Discard, stdoutSrc, "stdout", o.logger, containerID)
	go stream.pump(stream.stderr, io.Discard, stream.stderr, stderrSrc, "stderr", o.logger, containerID)
	go func() {
		stream.pumps.Wait()
		close(stream.pumpDone)
	}()

	return stream, nil
}

// pump copies one Docker connection into its pipe until the source ends.
// pipe is the half this connection owns, dstout and dsterr are what
// stdcopy.StdCopy should write stdout and stderr frames to. StdCopy blocks for
// the container's whole remaining life, which is the point.
func (l *LogStreamer) pump(pipe *asyncPipe, dstout, dsterr io.Writer, src io.Reader, name string, logger *slog.Logger, containerID string) {
	defer l.pumps.Done()
	// This is what unblocks a reader once Docker hits EOF (container stopped)
	// or the stream is closed underneath us.
	defer pipe.CloseWrite()

	// A source closed by Close is the ordinary end of this pump, not a fault
	// worth a log line.
	if _, err := stdcopy.StdCopy(dstout, dsterr, src); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		logger.Debug("logs: demux ended",
			slog.String("container", shortContainerID(containerID)),
			slog.String("stream", name),
			slog.String("err", err.Error()))
	}
}

// Stdout yields the container's standard output, already split from stderr.
func (l *LogStreamer) Stdout() io.ReadCloser { return l.stdout }

// Stderr yields the container's standard error, already split from stdout.
func (l *LogStreamer) Stderr() io.ReadCloser { return l.stderr }

// Close stops the pumps and releases both Docker connections and both pipes.
//
// It is idempotent, because handlers defer it and also call it explicitly when
// they end a stream early — but it reports the same underlying error every time
// rather than nil on the second call, so a caller that only inspects the
// return value cannot be misled into thinking the cleanup succeeded.
func (l *LogStreamer) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = errors.Join(l.stdoutSrc.Close(), l.stderrSrc.Close())
		// Release readers parked in Read, and any writer blocked on a full
		// buffer, before waiting on the pumps.
		_ = l.stdout.Close()
		_ = l.stderr.Close()
		<-l.pumpDone
	})
	return l.closeErr
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// GetRecentContainerLogs reads the most recent tail lines from the container's stdout and stderr.
func (o *Orchestrator) GetRecentContainerLogs(ctx context.Context, containerID string, tail int) (string, error) {
	if containerID == "" {
		return "", errors.New("empty container id")
	}
	if tail <= 0 {
		tail = 100
	}
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     false,
		Tail:       strconv.Itoa(tail),
	}
	rc, err := o.docker.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return "", fmt.Errorf("orchestrator: reading container logs: %w", err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	_, _ = stdcopy.StdCopy(&buf, &buf, rc)
	return buf.String(), nil
}
