package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
)

func TestSubdomainFromHost(t *testing.T) {
	g := &Gateway{
		cfg: &config.Config{
			SandboxBaseDomain: "sandbox.localhost",
		},
	}

	tests := []struct {
		host      string
		expected  string
		expectErr bool
	}{
		{"demo.sandbox.localhost", "demo", false},
		{"demo.sandbox.localhost:8090", "demo", false},
		{"  my-app.sandbox.localhost  ", "my-app", false},
		{"invalid.otherdomain.com", "", true},
		{"sandbox.localhost", "", true},
		{"", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			sub, err := g.subdomainFromHost(tc.host)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for host %q, got sub %q", tc.host, sub)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for host %q: %v", tc.host, err)
				}
				if sub != tc.expected {
					t.Errorf("expected subdomain %q, got %q", tc.expected, sub)
				}
			}
		})
	}
}

func TestGatewayWakeSingleFlightDeduplication(t *testing.T) {
	var wakeExecutions int64

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g := &Gateway{
		cfg:    &config.Config{SandboxBaseDomain: "sandbox.localhost"},
		logger: logger,
		waking: map[string]*wakeCall{},
	}

	sandbox := &db.Sandbox{
		ID:        "test-sandbox-dedup",
		Subdomain: "test-dedup",
	}

	// We create a mock wake trigger by manipulating the internal waking map
	call := &wakeCall{done: make(chan struct{})}
	g.waking[sandbox.ID] = call

	const callers = 10
	var wg sync.WaitGroup
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			errs[idx] = g.wake(ctx, sandbox)
		}(i)
	}

	// Simulate wake completion with intentional error
	time.Sleep(50 * time.Millisecond)
	expectedErr := fmt.Errorf("container failed to become healthy")
	g.wakingMu.Lock()
	call.err = expectedErr
	delete(g.waking, sandbox.ID)
	close(call.done)
	g.wakingMu.Unlock()

	wg.Wait()

	// Verify all callers received the exact same error
	for i, err := range errs {
		if err == nil {
			t.Errorf("caller %d received nil error; expected %v", i, expectedErr)
		} else if err.Error() != expectedErr.Error() {
			t.Errorf("caller %d received err %v; want %v", i, err, expectedErr)
		}
	}

	_ = atomic.LoadInt64(&wakeExecutions)
}

func TestGatewayWakeCallerContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g := &Gateway{
		cfg:    &config.Config{SandboxBaseDomain: "sandbox.localhost"},
		logger: logger,
		waking: map[string]*wakeCall{},
	}

	sandbox := &db.Sandbox{
		ID:        "test-sandbox-cancel",
		Subdomain: "test-cancel",
	}

	// Active in-flight wake that has not finished
	call := &wakeCall{done: make(chan struct{})}
	g.waking[sandbox.ID] = call

	// Caller with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := g.wake(ctx, sandbox)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected caller to abort promptly, took %v", elapsed)
	}

	// Clean up lingering in-flight call
	g.wakingMu.Lock()
	delete(g.waking, sandbox.ID)
	close(call.done)
	g.wakingMu.Unlock()
}

