//go:build unix

package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
)

// A hung nixpacks build must release the deploy slot and leave no children
// behind. The fake build spawns a child sleep, records its pid, then hangs; the
// process-group kill has to take both down.
func TestBuildImageKillsWholeProcessGroupOnCancel(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "child.pid")
	bin := filepath.Join(tmp, "nixpacks")
	script := "#!/bin/sh\nsleep 300 &\necho $! > '" + pidFile + "'\nwait\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	o := &Orchestrator{cfg: &config.Config{NixpacksBin: bin}}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := o.BuildImage(ctx, t.TempDir(), "test", nil)
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("BuildImage error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("BuildImage returned after %s; the process group was not killed", elapsed)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child process %d survived the process-group kill", pid)
}
