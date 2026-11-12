package orchestrator

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestAsyncPipeRoundTrip(t *testing.T) {
	p := newAsyncPipe(1024)
	go func() {
		p.Write([]byte("hello "))
		p.Write([]byte("world"))
		p.CloseWrite()
	}()

	var got bytes.Buffer
	if _, err := io.Copy(&got, p); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if got.String() != "hello world" {
		t.Errorf("read %q, want %q", got.String(), "hello world")
	}
}

func TestAsyncPipeReadBlocksUntilDataArrives(t *testing.T) {
	p := newAsyncPipe(64)
	read := make(chan string, 1)
	go func() {
		b := make([]byte, 16)
		n, _ := p.Read(b)
		read <- string(b[:n])
	}()

	select {
	case s := <-read:
		t.Fatalf("Read returned %q before any data was written", s)
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := p.Write([]byte("later")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	select {
	case s := <-read:
		if s != "later" {
			t.Errorf("read %q, want %q", s, "later")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read never returned after a write")
	}
}

// A reader that walks away must not strand the demuxer blocked in Write.
func TestAsyncPipeCloseUnblocksParkedWriter(t *testing.T) {
	p := newAsyncPipe(8)
	if _, err := p.Write(bytes.Repeat([]byte("x"), 8)); err != nil {
		t.Fatalf("filling the buffer: %v", err)
	}

	writeErr := make(chan error, 1)
	go func() {
		_, err := p.Write([]byte("overflow"))
		writeErr <- err
	}()

	select {
	case err := <-writeErr:
		t.Fatalf("Write returned %v, expected it to block on the full buffer", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-writeErr:
		if err == nil {
			t.Error("expected the parked Write to fail after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not release the parked writer")
	}
}

func TestAsyncPipeCloseUnblocksParkedReader(t *testing.T) {
	p := newAsyncPipe(64)
	readErr := make(chan error, 1)
	go func() {
		b := make([]byte, 16)
		_, err := p.Read(b)
		readErr <- err
	}()

	select {
	case err := <-readErr:
		t.Fatalf("Read returned %v early", err)
	case <-time.After(50 * time.Millisecond):
	}

	_ = p.Close()
	select {
	case err := <-readErr:
		if err == nil || errors.Is(err, io.EOF) {
			t.Errorf("Read err = %v, want the close error rather than EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not release the parked reader")
	}
}

// Draining must free room, otherwise a full buffer deadlocks the demuxer against
// its own consumer.
func TestAsyncPipeDrainUnblocksWriter(t *testing.T) {
	p := newAsyncPipe(8)
	if _, err := p.Write(bytes.Repeat([]byte("x"), 8)); err != nil {
		t.Fatalf("filling the buffer: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.Write([]byte("more"))
		done <- err
	}()

	b := make([]byte, 4)
	if _, err := p.Read(b); err != nil {
		t.Fatalf("Read: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Write after drain: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("draining did not let the blocked writer proceed")
	}
}

// The point of buffering: a stalled consumer of one stream must not stop the
// other. This is the regression guard for stderr being held hostage by a slow
// stdout reader, which an unbuffered io.Pipe cannot avoid because StdCopy is one
// goroutine writing to both.
func TestStalledReaderDoesNotBlockOtherStream(t *testing.T) {
	out := newAsyncPipe(1024)
	errPipe := newAsyncPipe(1024)

	// Stand in for StdCopy: writes both, exactly as the demuxer does.
	go func() {
		out.Write(bytes.Repeat([]byte("o"), 512))
		errPipe.Write([]byte("critical error"))
		out.CloseWrite()
		errPipe.CloseWrite()
	}()

	// Nobody is reading out; stderr must still be fully readable.
	got, err := io.ReadAll(errPipe)
	if err != nil {
		t.Fatalf("reading stderr: %v", err)
	}
	if string(got) != "critical error" {
		t.Errorf("stderr = %q", got)
	}
}

func TestAsyncPipeWriteAfterCloseWriteFails(t *testing.T) {
	p := newAsyncPipe(64)
	_ = p.CloseWrite()
	if _, err := p.Write([]byte("x")); err == nil {
		t.Error("expected Write to fail after CloseWrite")
	}
}

// Close is called from both a defer and an explicit early return, so it has to
// be safe to call repeatedly — and it must keep reporting the same error rather
// than a misleading nil on the second call. Both connections have to be closed,
// so the error is checked against either of them.
func TestLogStreamerCloseIsIdempotentAndReportsItsError(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stdoutErr  error
		stderrErr  error
		wantReport error
	}{
		{"stdout fails", errors.New("connection reset"), nil, nil},
		{"stderr fails", nil, errors.New("broken pipe"), nil},
	} {
		want := tc.stdoutErr
		if want == nil {
			want = tc.stderrErr
		}
		stream := &LogStreamer{
			stdout:    newAsyncPipe(64),
			stderr:    newAsyncPipe(64),
			stdoutSrc: &failingCloser{err: tc.stdoutErr},
			stderrSrc: &failingCloser{err: tc.stderrErr},
			pumpDone:  make(chan struct{}),
		}
		close(stream.pumpDone)

		for i := range 3 {
			err := stream.Close()
			if err == nil {
				t.Errorf("%s: Close call %d returned nil, want %v", tc.name, i+1, want)
			}
			if want != nil && !errors.Is(err, want) {
				t.Errorf("%s: Close call %d returned %v, want %v", tc.name, i+1, err, want)
			}
		}
	}
}

type failingCloser struct{ err error }

func (f *failingCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (f *failingCloser) Close() error             { return f.err }

// Concurrency guard for the pipe's internal locking.
func TestAsyncPipeConcurrentReadWrite(t *testing.T) {
	p := newAsyncPipe(4096)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			p.Write(bytes.Repeat([]byte{byte('a' + i)}, 512))
			p.CloseWrite()
		}(i)
		go func() {
			defer wg.Done()
			buf := make([]byte, 128)
			for {
				if _, err := p.Read(buf); err != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
}
