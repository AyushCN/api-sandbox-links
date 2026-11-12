package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/docker/docker/api/types/container"
)

// These exercise the log demuxer against a real Docker daemon and a real
// container, which is the only way to catch the failures that matter here: a
// wrong stdcopy frame size, a Tail that silently returns nothing, timestamps
// that are not parseable, and teardown that leaks a goroutine. They skip when no
// daemon is reachable so the suite still runs on a machine without Docker.
//
// testStampLayout mirrors the layout the API layer parses Docker's timestamp
// prefix with. It is duplicated rather than imported because this test is in
// the orchestrator package, which must not depend on the API layer; if the two
// ever drift, the timestamp assertions below fail, which is the point.
const testStampLayout = "2006-01-02T15:04:05.999999999Z07:00"

// Set ASL_TEST_IMAGE to override the throwaway image (default node:20-alpine,
// which is small and present in most dev environments).
func testOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	cfg := &config.Config{DockerHost: host}
	var w io.Writer = io.Discard
	if os.Getenv("ASL_TEST_LOG") != "" {
		w = os.Stderr
	}
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
	o, err := New(context.Background(), cfg, nil, nil, nil, logger)
	if err != nil {
		t.Skipf("skipping: no usable Docker daemon (%v)", err)
	}
	t.Cleanup(func() { _ = o.Close() })
	return o
}

func testImage() string {
	if img := os.Getenv("ASL_TEST_IMAGE"); img != "" {
		return img
	}
	return "node:20-alpine"
}

// startLogContainer runs a container that emits a known, interleaved set of
// stdout and stderr lines and then idles, so the stream stays open for follow.
func startLogContainer(t *testing.T, o *Orchestrator, script string) string {
	t.Helper()
	ctx := context.Background()

	resp, err := o.docker.ContainerCreate(ctx, &container.Config{
		Image: testImage(),
		Cmd:   []string{"sh", "-c", script},
	}, nil, nil, nil, "")
	if err != nil {
		t.Skipf("skipping: cannot create container from %s (%v)", testImage(), err)
	}
	if err := o.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = o.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		t.Skipf("skipping: cannot start container (%v)", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = o.docker.ContainerRemove(c, resp.ID, container.RemoveOptions{Force: true})
	})
	return resp.ID
}

// The demux is the whole reason this type exists: without splitting on Docker's
// 8-byte frame header, stdout and stderr arrive interleaved into one unusable
// blob, and every line is corrupted.
func TestStreamContainerLogsDemuxesStdoutAndStderr(t *testing.T) {
	o := testOrchestrator(t)
	id := startLogContainer(t, o,
		`echo out-one; echo err-one >&2; echo out-two; echo err-two >&2; sleep 30`)

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	defer stream.Close()

	stdout := collectLines(t, stream, 2)
	stderr := collectLinesFrom(t, stream.Stderr(), 2)
	if !equalLines(stdout, "out-one", "out-two") {
		t.Errorf("stdout = %q, want [out-one out-two]", stdout)
	}
	if !equalLines(stderr, "err-one", "err-two") {
		t.Errorf("stderr = %q, want [err-one err-two]", stderr)
	}
}

// Timestamps are what let the browser date the Tail backfill honestly, so the
// prefix must be real RFC3339 and the line must survive stripping.
func TestStreamContainerLogsStampsEveryLine(t *testing.T) {
	o := testOrchestrator(t)
	id := startLogContainer(t, o, `echo hello-stamped; sleep 30`)

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	defer stream.Close()

	lines := collectRawLines(t, stream.Stdout(), 1)
	raw := lines[0]

	fields := strings.SplitN(raw, " ", 2)
	if len(fields) != 2 {
		t.Fatalf("line %q is not <timestamp> <text>", raw)
	}
	if _, err := time.Parse(testStampLayout, fields[0]); err != nil {
		t.Errorf("timestamp %q not parseable as %s: %v", fields[0], testStampLayout, err)
	}
	if fields[1] != "hello-stamped" {
		t.Errorf("line text = %q, want %q", fields[1], "hello-stamped")
	}
}

// Tail must replay history, otherwise a panel opened long after the container
// started shows nothing until the app happens to print again.
func TestStreamContainerLogsReplaysTailBeforeFollowing(t *testing.T) {
	o := testOrchestrator(t)
	// Emit 12 lines and exit well before the stream is opened.
	id := startLogContainer(t, o, `for i in $(seq 1 12); do echo line-$i; done`)
	waitForExit(t, o, id)

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	defer stream.Close()

	got := collectLines(t, stream, 12)
	if len(got) < 12 {
		t.Fatalf("replayed %d lines, want at least 12", len(got))
	}
	if got[0] != "line-1" || got[11] != "line-12" {
		t.Errorf("replay = %v, want line-1..line-12 in order", got)
	}
}

// The regression guard for a bug this suite found by accident: Docker applies
// Tail to the combined multiplexed stream and emits all stdout frames before any
// stderr frame, so on one connection a container with more than Tail lines of
// stdout got a backfill containing no stderr whatsoever. A chatty container
// hid its one real error line from the log panel.
func TestStreamContainerLogsBackfillsStderrDespiteChattyStdout(t *testing.T) {
	o := testOrchestrator(t)
	// Far more stdout than the tail limit, then a single stderr line. The
	// container is allowed to finish writing before the stream is opened, so
	// this exercises the backfill path and not the live-follow path.
	id := startLogContainer(t, o,
		`for i in $(seq 1 400); do echo stdout-line-$i; done; echo THE-ERROR >&2`)
	waitForExit(t, o, id)

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	defer stream.Close()

	// stderr must not be starved by stdout, even though the backfill is cut.
	if got := collectLinesFrom(t, stream.Stderr(), 1); len(got) != 1 || got[0] != "THE-ERROR" {
		t.Errorf("stderr backfill = %v, want [THE-ERROR]", got)
	}

	// And stdout must still be capped near the tail limit rather than replayed
	// in full, which is the other half of the same behaviour.
	//
	// The exact count is not asserted: the daemon flushes the log as the
	// container exits, so where the tail boundary lands can shift by a line or
	// two depending on when the replay was cut. What must hold is that the
	// backfill is capped and ends at the newest output.
	stdout := collectLinesUpTo(t, stream, 200)
	if len(stdout) == 0 {
		t.Fatal("stdout backfill was empty")
	}
	if len(stdout) > 200 {
		t.Errorf("stdout backfill = %d lines, want at most the 200 line tail", len(stdout))
	}
	last := stdout[len(stdout)-1]
	if n := trailingIndex(t, last, "stdout-line-"); n < 390 {
		t.Errorf("last stdout line = %q, want the newest output", last)
	}
}

// collectLinesUpTo reads up to n lines and tolerates the source ending early,
// unlike collectLines which insists on getting all n.
func collectLinesUpTo(t *testing.T, s *LogStreamer, n int) []string {
	t.Helper()
	out := make(chan string, n)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(s.Stdout())
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for i := 0; i < n && sc.Scan(); i++ {
			out <- sc.Text()
		}
	}()

	var lines []string
	timeout := time.After(45 * time.Second)
	for {
		select {
		case l, ok := <-out:
			if !ok {
				return stripStamps(lines)
			}
			lines = append(lines, l)
		case <-timeout:
			t.Fatalf("timed out reading the stdout backfill: %v", trunc(lines))
		}
	}
}

// trailingIndex returns the number after the last occurrence of prefix.
func trailingIndex(t *testing.T, s, prefix string) int {
	t.Helper()
	i := strings.LastIndex(s, prefix)
	if i < 0 {
		t.Fatalf("%q does not contain %q", s, prefix)
	}
	n, err := strconv.Atoi(s[i+len(prefix):])
	if err != nil {
		t.Fatalf("%q has a non-numeric suffix: %v", s, err)
	}
	return n
}

// The regression guard for the buffered pipes: a consumer that stops reading
// stdout must not stop stderr from being delivered. With an unbuffered io.Pipe
// the demuxer is a single goroutine, so a stalled stdout reader strands stderr
// in the Docker socket.
func TestStalledStdoutConsumerDoesNotStrandStderr(t *testing.T) {
	o := testOrchestrator(t)
	id := startLogContainer(t, o,
		`for i in $(seq 1 400); do echo stdout-line-$i; done; echo THE-ERROR >&2; sleep 30`)

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	defer stream.Close()

	// Deliberately never read stdout.
	got := collectLinesFrom(t, stream.Stderr(), 1)
	if len(got) == 0 || got[0] != "THE-ERROR" {
		t.Errorf("stderr = %v, want [THE-ERROR] while stdout is unread", got)
	}
}

// Closing is what stops a browser tab from leaking a Docker stream and a parked
// goroutine, so it has to actually tear everything down.
func TestStreamContainerLogsCloseIsPromptAndLeakFree(t *testing.T) {
	o := testOrchestrator(t)
	id := startLogContainer(t, o, `echo one; sleep 60`)

	before := runtimeGoroutines()

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	// Consume a little so both pumps are live and parked in Read.
	_ = collectLines(t, stream, 1)

	if err := stream.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// A second Close must be safe and must report the same outcome.
	if err := stream.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	// Reads after Close must fail promptly rather than hang.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.ReadAll(stream.Stdout())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reading a closed stream did not return")
	}

	// Allow the demux goroutine a moment to unwind before counting.
	settle := time.Now()
	for runtimeGoroutines() > before && time.Since(settle) < 5*time.Second {
		time.Sleep(50 * time.Millisecond)
	}
	if after := runtimeGoroutines(); after > before+1 {
		t.Errorf("goroutines went from %d to %d across open/close", before, after)
	}
}

// A line larger than any sane buffer must not be able to wedge the demuxer.
func TestStreamContainerLogsHandlesVeryLongLine(t *testing.T) {
	o := testOrchestrator(t)
	id := startLogContainer(t, o,
		`head -c 300000 /dev/zero | tr '\0' 'x'; echo; echo after-the-blob; sleep 30`)

	stream, err := o.StreamContainerLogs(context.Background(), id)
	if err != nil {
		t.Fatalf("StreamContainerLogs: %v", err)
	}
	defer stream.Close()

	lines := collectLines(t, stream, 2)
	found := false
	for _, l := range lines {
		if l == "after-the-blob" {
			found = true
		}
	}
	if !found {
		t.Errorf("lines after a 300KB write were lost; got %d lines, first=%q",
			len(lines), trunc(lines))
	}
}

// --- helpers ---------------------------------------------------------------

// collectLines reads n lines from the stream's stdout, returning them with any
// Docker timestamp prefix removed.
func collectLines(t *testing.T, s *LogStreamer, n int) []string {
	t.Helper()
	return stripStamps(collectRawLines(t, s.Stdout(), n))
}

// collectLinesFrom is collectLines for a bare reader, used for stderr.
func collectLinesFrom(t *testing.T, r io.Reader, n int) []string {
	t.Helper()
	return stripStamps(collectRawLines(t, r, n))
}

func collectRawLines(t *testing.T, r io.Reader, n int) []string {
	t.Helper()
	out := make(chan string, n)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for i := 0; i < n && sc.Scan(); i++ {
			out <- sc.Text()
		}
	}()

	var lines []string
	timeout := time.After(45 * time.Second)
	for len(lines) < n {
		select {
		case l, ok := <-out:
			if !ok {
				t.Fatalf("stream ended after %d of %d lines: %v", len(lines), n, trunc(lines))
			}
			lines = append(lines, l)
		case <-timeout:
			t.Fatalf("timed out after %d of %d lines: %v", len(lines), n, trunc(lines))
		}
	}
	return lines
}

func stripStamps(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		stamp, rest, found := strings.Cut(l, " ")
		if found {
			if _, err := time.Parse(testStampLayout, stamp); err == nil {
				l = rest
			}
		}
		out = append(out, l)
	}
	return out
}

func equalLines(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// runtimeGoroutines reports the live goroutine count, used to assert that
// opening and closing a log stream does not leak one.
func runtimeGoroutines() int { return runtime.NumGoroutine() }

func waitForExit(t *testing.T, o *Orchestrator, id string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		c, err := o.docker.ContainerInspect(context.Background(), id)
		if err == nil && !c.State.Running {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("skipping: container did not exit in time")
}

func trunc(lines []string) string {
	if len(lines) == 0 {
		return "[]"
	}
	if len(lines) > 3 {
		return fmt.Sprintf("%v...(%d more)", lines[:3], len(lines)-3)
	}
	return fmt.Sprintf("%v", lines)
}
