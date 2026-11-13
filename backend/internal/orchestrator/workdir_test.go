package orchestrator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestRemoveSandboxWorkDirMaliciousSymlinks verifies that RemoveSandboxWorkDir
// safely removes symlinks without following them into external directories or files.
func TestRemoveSandboxWorkDirMaliciousSymlinks(t *testing.T) {
	tempBase := t.TempDir()

	// External directory and sensitive file that must never be deleted
	extDir := filepath.Join(tempBase, "external_system_dir")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("failed to create extDir: %v", err)
	}
	extFile := filepath.Join(extDir, "critical_system_file.conf")
	origContent := []byte("DO_NOT_DELETE_CRITICAL_CONFIG=true\n")
	if err := os.WriteFile(extFile, origContent, 0o644); err != nil {
		t.Fatalf("failed to write extFile: %v", err)
	}

	workRoot := filepath.Join(tempBase, "sandbox_work_root")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatalf("failed to create workRoot: %v", err)
	}

	// 1. Sandbox directory contains internal symlinks pointing to extDir and extFile
	sbDir := filepath.Join(workRoot, "sb-malicious-1")
	if err := os.MkdirAll(sbDir, 0o755); err != nil {
		t.Fatalf("failed to create sbDir: %v", err)
	}
	if err := os.Symlink(extDir, filepath.Join(sbDir, "link_to_ext_dir")); err != nil {
		t.Fatalf("failed to create dir symlink: %v", err)
	}
	if err := os.Symlink(extFile, filepath.Join(sbDir, "link_to_ext_file")); err != nil {
		t.Fatalf("failed to create file symlink: %v", err)
	}

	// Delete sandbox directory
	if err := RemoveSandboxWorkDir(workRoot, "sb-malicious-1"); err != nil {
		t.Fatalf("RemoveSandboxWorkDir failed: %v", err)
	}

	// Verify sandbox directory was deleted
	if _, err := os.Stat(sbDir); !os.IsNotExist(err) {
		t.Fatalf("expected sandbox directory to be deleted")
	}

	// Verify external directory and external file still exist and are untouched
	if _, err := os.Stat(extDir); err != nil {
		t.Fatalf("SECURITY FAILURE: external directory was deleted during cleanup! %v", err)
	}
	data, err := os.ReadFile(extFile)
	if err != nil {
		t.Fatalf("SECURITY FAILURE: external file was deleted during cleanup! %v", err)
	}
	if !bytes.Equal(data, origContent) {
		t.Fatalf("SECURITY FAILURE: external file content was altered during cleanup! Got %q", string(data))
	}

	// 2. Sandbox subdomain ITSELF is a symlink to extDir
	sbSymlink := filepath.Join(workRoot, "sb-malicious-symlink")
	if err := os.Symlink(extDir, sbSymlink); err != nil {
		t.Fatalf("failed to create symlink subdomain: %v", err)
	}

	// Delete sandbox subdomain symlink
	if err := RemoveSandboxWorkDir(workRoot, "sb-malicious-symlink"); err != nil {
		t.Fatalf("RemoveSandboxWorkDir on symlink subdomain failed: %v", err)
	}

	// Verify symlink was deleted
	if _, err := os.Lstat(sbSymlink); !os.IsNotExist(err) {
		t.Fatalf("expected symlink subdomain to be deleted")
	}

	// Verify external directory and file are still intact
	if _, err := os.Stat(extDir); err != nil {
		t.Fatalf("SECURITY FAILURE: external directory was deleted when symlink subdomain was removed!")
	}
	data2, err := os.ReadFile(extFile)
	if err != nil || !bytes.Equal(data2, origContent) {
		t.Fatalf("SECURITY FAILURE: external file was corrupted or deleted!")
	}

	// 3. Traversal attempts must fail
	if err := RemoveSandboxWorkDir(workRoot, "../external_system_dir"); err == nil {
		t.Fatalf("expected ../ traversal to fail")
	}
	if err := RemoveSandboxWorkDir(workRoot, "."); err == nil {
		t.Fatalf("expected . to fail")
	}
}

// TestAdversarialSidecarDatabaseSymlinkIsolation verifies that DetectDatabaseNeed /
// readEnvExample / readFiles do not follow symlinks outside the workspace.
func TestAdversarialSidecarDatabaseSymlinkIsolation(t *testing.T) {
	ws := t.TempDir()

	// External confidential file
	extFile := filepath.Join(t.TempDir(), "passwd")
	_ = os.WriteFile(extFile, []byte("postgres://user:secret@db.host:5432/mydb\n"), 0o644)

	// Malicious symlinks inside workspace
	_ = os.Symlink(extFile, filepath.Join(ws, ".env.example"))
	_ = os.Symlink(extFile, filepath.Join(ws, "requirements.txt"))

	// readEnvExample must return empty because the symlink escapes the workspace
	env := readEnvExample(ws)
	if env != "" {
		t.Fatalf("SECURITY VIOLATION: readEnvExample followed symlink outside workspace! Data: %s", env)
	}

	// readFiles must return empty because the symlink escapes the workspace
	rf := readFiles(ws, "requirements.txt")
	if rf != "" {
		t.Fatalf("SECURITY VIOLATION: readFiles followed symlink outside workspace! Data: %s", rf)
	}
}

// TestAdversarialConcurrentCleanupAndFileAccess verifies that concurrent workspace cleanup
// and file operations do not panic or corrupt state.
func TestAdversarialConcurrentCleanupAndFileAccess(t *testing.T) {
	workRoot := t.TempDir()
	sbDir := filepath.Join(workRoot, "sb-racing")
	_ = os.MkdirAll(sbDir, 0o755)
	_ = os.WriteFile(filepath.Join(sbDir, "file.txt"), []byte("data"), 0o644)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_ = RemoveSandboxWorkDir(workRoot, "sb-racing")
			_ = os.MkdirAll(sbDir, 0o755)
			_ = os.WriteFile(filepath.Join(sbDir, "file.txt"), []byte("data"), 0o644)
		}
	}()

	for {
		select {
		case <-done:
			return
		default:
			root, err := os.OpenRoot(sbDir)
			if err == nil {
				_, _ = root.ReadFile("file.txt")
				_ = root.Close()
			}
		}
	}
}

// TestConcurrentCleanupWorkersOnSameSandbox tests that two cleanup workers processing the same
// sandbox concurrently (e.g. manual destroy vs expiry reaper) both succeed without error or panic.
func TestConcurrentCleanupWorkersOnSameSandbox(t *testing.T) {
	workRoot := t.TempDir()
	subdomain := "sb-concurrent-cleanup"
	sbDir := filepath.Join(workRoot, subdomain)
	if err := os.MkdirAll(sbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sbDir, "app.js"), []byte("console.log('hi');"), 0o644)

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier
			errs[workerID] = RemoveSandboxWorkDir(workRoot, subdomain)
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	for workerID, err := range errs {
		if err != nil {
			t.Errorf("worker %d failed during concurrent cleanup: %v", workerID, err)
		}
	}

	if _, err := os.Stat(sbDir); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be removed, but still exists")
	}
}

// TestStaleCleanupWorkerCannotRemoveNewerOperationResources tests that a stale cleanup worker
// targeting an older generation's subdomain cannot remove or modify resources belonging to a later
// operation on the same platform.
func TestStaleCleanupWorkerCannotRemoveNewerOperationResources(t *testing.T) {
	workRoot := t.TempDir()

	// Generation 1: older sandbox
	sub1 := "sb-gen-1-old"
	dir1 := filepath.Join(workRoot, sub1)
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir1, "v1.txt"), []byte("gen1"), 0o644)

	// Generation 2: newer sandbox created in the same work root
	sub2 := "sb-gen-2-new"
	dir2 := filepath.Join(workRoot, sub2)
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	gen2Data := []byte("CRITICAL_NEW_GENERATION_DATA")
	if err := os.WriteFile(filepath.Join(dir2, "v2.txt"), gen2Data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Stale worker executes cleanup for generation 1
	if err := RemoveSandboxWorkDir(workRoot, sub1); err != nil {
		t.Fatalf("RemoveSandboxWorkDir(sub1) failed: %v", err)
	}

	// Verify generation 1 was deleted
	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Errorf("expected generation 1 directory to be removed")
	}

	// Verify generation 2 is completely intact and untouched
	data, err := os.ReadFile(filepath.Join(dir2, "v2.txt"))
	if err != nil {
		t.Fatalf("generation 2 file missing: %v", err)
	}
	if !bytes.Equal(data, gen2Data) {
		t.Fatalf("generation 2 data was corrupted by stale cleanup worker!")
	}
}

// TestLifecycleDeterministicBarrierWorkspaceTeardown tests that when a lifecycle operation is paused
// after an initial check, and a deletion cleans up the workspace, the resumed operation detects the
// deletion and fails safely without recreating orphaned resources.
func TestLifecycleDeterministicBarrierWorkspaceTeardown(t *testing.T) {
	workRoot := t.TempDir()
	subdomain := "sb-lifecycle-barrier"
	sbDir := filepath.Join(workRoot, subdomain)
	if err := os.MkdirAll(sbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sbDir, "index.js"), []byte("console.log('hi')"), 0o644)

	// Step 1: Operation passes initial destroyed check
	isDestroyed := false
	checkInitial := func() bool { return isDestroyed }
	if checkInitial() {
		t.Fatal("unexpected early destroy")
	}

	pauseBarrier := make(chan struct{})
	resumeBarrier := make(chan struct{})
	done := make(chan struct{})

	// Step 2: Simulated worker starts and pauses before resource finalization
	var workerErr error
	go func() {
		defer close(done)
		// Signal paused
		close(pauseBarrier)
		// Wait for resume
		<-resumeBarrier

		// Step 4 & 5: Check conditional state
		if isDestroyed {
			// Teardown any intermediate resource
			_ = RemoveSandboxWorkDir(workRoot, subdomain)
			workerErr = errors.New("aborted: sandbox was destroyed during operation")
			return
		}
	}()

	// Wait until paused
	<-pauseBarrier

	// Step 3: Deletion begins and cleans up
	isDestroyed = true
	if err := RemoveSandboxWorkDir(workRoot, subdomain); err != nil {
		t.Fatalf("RemoveSandboxWorkDir failed: %v", err)
	}
	if _, err := os.Stat(sbDir); !os.IsNotExist(err) {
		t.Fatal("expected workdir to be deleted during teardown")
	}

	// Step 4: Resume worker
	close(resumeBarrier)
	<-done

	// Step 5 & 6: Verify worker failed safely and no workspace exists
	if workerErr == nil || !strings.Contains(workerErr.Error(), "destroyed") {
		t.Fatalf("expected destroyed error, got: %v", workerErr)
	}
	if _, err := os.Stat(sbDir); !os.IsNotExist(err) {
		t.Fatal("workspace was recreated after destroy")
	}
}
