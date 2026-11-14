package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/workspacelock"
	"github.com/go-chi/chi/v5"
)

type forkSyncStore struct {
	Store // Embeds the interface; unused methods remain unimplemented.

	mu           sync.RWMutex
	sandboxes    map[string]*db.Sandbox
	repositories map[string]*db.Repository
}

func newForkSyncStore(user *db.User, parent *db.Sandbox) *forkSyncStore {
	parent.RepositoryID = "repo-sync-1"

	return &forkSyncStore{
		sandboxes: map[string]*db.Sandbox{
			parent.ID: parent,
		},
		repositories: map[string]*db.Repository{
			parent.RepositoryID: {
				ID:     parent.RepositoryID,
				UserID: user.ID,
			},
		},
	}
}

func (s *forkSyncStore) OwnsSandbox(sandboxID, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sb, ok := s.sandboxes[sandboxID]
	if !ok {
		return false, nil
	}

	repo, ok := s.repositories[sb.RepositoryID]
	if !ok {
		return false, nil
	}

	return repo.UserID == userID, nil
}

func (s *forkSyncStore) GetSandbox(id string) (*db.Sandbox, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sb, ok := s.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("sandbox %s not found", id)
	}
	cp := *sb
	return &cp, nil
}

func (s *forkSyncStore) GetSandboxBySubdomain(subdomain string) (*db.Sandbox, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sb := range s.sandboxes {
		if sb.Subdomain == subdomain {
			cp := *sb
			return &cp, nil
		}
	}

	return nil, fmt.Errorf("sandbox subdomain %s not found", subdomain)
}

func (s *forkSyncStore) CreateSandboxWithinLimits(
	sb *db.Sandbox,
	userID string,
	maxActive, maxRunning int,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	repo, ok := s.repositories[sb.RepositoryID]
	if !ok || repo.UserID != userID {
		return errors.New("repository ownership mismatch")
	}

	cp := *sb
	s.sandboxes[sb.ID] = &cp
	return nil
}

func (s *forkSyncStore) GetRepository(id string) (*db.Repository, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	repo, ok := s.repositories[id]
	if !ok {
		return nil, fmt.Errorf("repository %s not found", id)
	}
	return repo, nil
}

func (s *forkSyncStore) RecordFileEdit(sandboxID, relPath, userID string) error {
	return nil
}

func (s *forkSyncStore) ListSandboxDatabases(
	sandboxID string,
) ([]*db.SandboxDatabase, error) {
	return nil, nil
}

func (s *forkSyncStore) UpdateSandbox(id string, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sb, ok := s.sandboxes[id]
	if !ok {
		return fmt.Errorf("sandbox %s not found", id)
	}

	if status, ok := fields["status"].(string); ok {
		sb.Status = db.SandboxStatus(status)
	}

	return nil
}

func setupForkSyncServer(t *testing.T) (*Server, string, *db.User, *db.Sandbox) {
	t.Helper()

	workRoot := t.TempDir()

	cfg := &config.Config{
		SandboxWorkDir:     workRoot,
		SandboxBaseDomain:  "sandbox.localhost",
		MaxLifetimeSeconds: 86400,
	}

	user := &db.User{
		ID:       "test-user-sync-1",
		Username: "sync_tester",
	}

	parent := &db.Sandbox{
		ID:                 "sb-parent-sync-1",
		Subdomain:          "parent-subdomain-sync",
		BranchName:         "main",
		Status:             db.StatusRunning,
		LifetimeSeconds:    3600,
		ExpiresAt:          time.Now().Add(time.Hour),
		IdleTimeoutSeconds: 600,
	}

	parentDir := filepath.Join(workRoot, parent.Subdomain)

	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("failed to create parent directory: %v", err)
	}

	initialContent := []byte("console.log('parent initial content');")

	if err := os.WriteFile(
		filepath.Join(parentDir, "app.js"),
		initialContent,
		0o644,
	); err != nil {
		t.Fatalf("failed to write initial app.js: %v", err)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	srv := &Server{
		cfg:            cfg,
		store:          newForkSyncStore(user, parent),
		workspaceLocks: workspacelock.NewManager(),
		forkHooks:      &forkHooks{},
		bgCtx:          bgCtx,
		bgCancel:       bgCancel,
	}

	return srv, workRoot, user, parent
}

func newAuthRequest(method, target string, body []byte, user *db.User, sandboxID string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), ctxUserKey, user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", sandboxID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func waitForBackgroundTasks(t *testing.T, srv *Server) {
	t.Helper()

	done := make(chan struct{})

	go func() {
		srv.bgWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("background tasks did not finish within 10 seconds")
	}
}

// 1. Fork holds the workspace lock; a file save receives HTTP 409.
func TestForkHoldsLockSaveReceives409(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	forkCopyStarted := make(chan struct{})
	forkCopyBarrier := make(chan struct{})

	srv.forkHooks.onBeforeForkCopy = func(parentID string) {
		close(forkCopyStarted)
		<-forkCopyBarrier
	}

	// 1. Issue fork request via HTTP handler
	forkReq := newAuthRequest(http.MethodPost, "/api/sandboxes/"+parent.ID+"/fork", nil, user, parent.ID)
	forkRec := httptest.NewRecorder()
	srv.handleForkSandbox(forkRec, forkReq)

	if forkRec.Code != http.StatusAccepted {
		t.Fatalf("expected fork status 202 Accepted, got %d: %s", forkRec.Code, forkRec.Body.String())
	}

	// Wait until background fork is actively in critical section holding lock
	select {
	case <-forkCopyStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fork copy to begin")
	}

	// 2. While fork is copying, issue a file save via HTTP handler
	saveBody, _ := json.Marshal(map[string]any{"path": "app.js", "content": "racing edit"})
	saveReq := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", saveBody, user, parent.ID)
	saveRec := httptest.NewRecorder()
	srv.handleSaveFile(saveRec, saveReq)

	// Must receive 409 Conflict!
	if saveRec.Code != http.StatusConflict {
		t.Fatalf("expected save to return 409 Conflict while forking, got %d: %s", saveRec.Code, saveRec.Body.String())
	}
	if !strings.Contains(saveRec.Body.String(), "sandbox is currently being forked") {
		t.Fatalf("expected conflict message about forking, got: %s", saveRec.Body.String())
	}

	// 3. Release fork
	close(forkCopyBarrier)
	waitForBackgroundTasks(t, srv)
}

// 2. Fork completes; a subsequent file save succeeds.
func TestForkCompletesSubsequentSaveSucceeds(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	forkDone := make(chan struct{})
	srv.forkHooks.onAfterForkCopy = func(parentID string) {
		close(forkDone)
	}

	forkReq := newAuthRequest(http.MethodPost, "/api/sandboxes/"+parent.ID+"/fork", nil, user, parent.ID)
	forkRec := httptest.NewRecorder()
	srv.handleForkSandbox(forkRec, forkReq)

	if forkRec.Code != http.StatusAccepted {
		t.Fatalf("expected fork status 202, got %d", forkRec.Code)
	}

	// Wait for fork copy to finish
	select {
	case <-forkDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fork copy completion")
	}

	// Now file save must succeed with 200 OK
	saveBody, _ := json.Marshal(map[string]any{"path": "app.js", "content": "post-fork edit"})
	saveReq := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", saveBody, user, parent.ID)
	saveRec := httptest.NewRecorder()
	srv.handleSaveFile(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected save to succeed with 200 OK after fork completes, got %d: %s", saveRec.Code, saveRec.Body.String())
	}
	waitForBackgroundTasks(t, srv)
}

// 3. A save starts first; fork cannot copy until the save finishes.
func TestSaveStartsFirstForkWaits(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	saveWriting := make(chan struct{})
	saveBarrier := make(chan struct{})
	var saveFinished atomic.Bool

	srv.forkHooks.onBeforeSaveWrite = func(sandboxID string) {
		close(saveWriting)
		<-saveBarrier
	}

	forkCopyAttempted := make(chan struct{})
	srv.forkHooks.onBeforeForkCopy = func(parentID string) {
		if !saveFinished.Load() {
			t.Errorf("CRITICAL SECURITY FLAW: fork began copying before in-progress save finished!")
		}
		close(forkCopyAttempted)
	}

	// Start save in a goroutine
	saveDone := make(chan struct{})
	go func() {
		saveBody, _ := json.Marshal(map[string]any{"path": "app.js", "content": "first save content"})
		saveReq := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", saveBody, user, parent.ID)
		saveRec := httptest.NewRecorder()
		srv.handleSaveFile(saveRec, saveReq)
		if saveRec.Code != http.StatusOK {
			t.Errorf("save failed: %d: %s", saveRec.Code, saveRec.Body.String())
		}
		close(saveDone)
	}()

	// Wait until save is holding the lock inside onBeforeSaveWrite
	<-saveWriting

	// Start fork
	forkReq := newAuthRequest(http.MethodPost, "/api/sandboxes/"+parent.ID+"/fork", nil, user, parent.ID)
	forkRec := httptest.NewRecorder()
	srv.handleForkSandbox(forkRec, forkReq)

	if forkRec.Code != http.StatusAccepted {
		t.Fatalf("expected fork 202 Accepted, got %d", forkRec.Code)
	}

	// Let save finish
	saveFinished.Store(true)
	close(saveBarrier)

	<-saveDone

	// Fork copy should unblock and run only AFTER save completed
	select {
	case <-forkCopyAttempted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fork copy to execute after save finished")
	}
	waitForBackgroundTasks(t, srv)
}

// 4. Two simultaneous fork requests cannot copy the same parent concurrently.
func TestTwoSimultaneousForkRequestsRejected(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	fork1Started := make(chan struct{})
	fork1Barrier := make(chan struct{})

	srv.forkHooks.onBeforeForkCopy = func(parentID string) {
		close(fork1Started)
		<-fork1Barrier
	}

	// Fork 1 starts
	fork1Req := newAuthRequest(http.MethodPost, "/api/sandboxes/"+parent.ID+"/fork", nil, user, parent.ID)
	fork1Rec := httptest.NewRecorder()
	srv.handleForkSandbox(fork1Rec, fork1Req)

	if fork1Rec.Code != http.StatusAccepted {
		t.Fatalf("expected fork 1 Accepted, got %d", fork1Rec.Code)
	}

	<-fork1Started

	// Fork 2 attempts while fork 1 is holding the lock
	fork2Req := newAuthRequest(http.MethodPost, "/api/sandboxes/"+parent.ID+"/fork", nil, user, parent.ID)
	fork2Rec := httptest.NewRecorder()
	srv.handleForkSandbox(fork2Rec, fork2Req)

	if fork2Rec.Code != http.StatusConflict {
		t.Fatalf("expected fork 2 to return 409 Conflict, got %d: %s", fork2Rec.Code, fork2Rec.Body.String())
	}
	if !strings.Contains(fork2Rec.Body.String(), "fork already in progress") {
		t.Fatalf("expected error message about fork already in progress, got: %s", fork2Rec.Body.String())
	}

	close(fork1Barrier)
	waitForBackgroundTasks(t, srv)
}

// 5. A failed fork releases its lock.
func TestFailedForkReleasesLock(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	forkLock, err := srv.locks().BeginFork(parent.ID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}

	// Simulate failure in deferred release scope
	func() {
		defer forkLock.Release()
		// Simulated copy error
	}()

	// Subsequent save must succeed with 200 OK
	saveBody, _ := json.Marshal(map[string]any{"path": "app.js", "content": "post-failed-fork edit"})
	saveReq := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", saveBody, user, parent.ID)
	saveRec := httptest.NewRecorder()
	srv.handleSaveFile(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected save to succeed with 200 OK after failed fork, got %d: %s", saveRec.Code, saveRec.Body.String())
	}
}

// 6. A canceled or failed copy does not leave the workspace permanently locked.
func TestCanceledForkDoesNotLeaveWorkspaceLocked(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	forkLock, err := srv.locks().BeginFork(parent.ID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled immediately

	err = forkLock.WaitExclusive(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	forkLock.Release()

	// Subsequent save must succeed immediately
	saveBody, _ := json.Marshal(map[string]any{"path": "app.js", "content": "post-cancel save"})
	saveReq := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", saveBody, user, parent.ID)
	saveRec := httptest.NewRecorder()
	srv.handleSaveFile(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected save to succeed with 200 OK after cancelled fork, got %d: %s", saveRec.Code, saveRec.Body.String())
	}
}

// 7. Concurrent saves to the same file do not produce partial or corrupted content.
func TestConcurrentSavesNoCorruptedContent(t *testing.T) {
	srv, workRoot, user, parent := setupForkSyncServer(t)

	const n = 15
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		content := fmt.Sprintf("PAYLOAD_VERSION_%04d_REPEAT_%s", i, strings.Repeat("X", 2048))
		go func(c string) {
			defer wg.Done()
			<-startBarrier

			body, _ := json.Marshal(map[string]any{"path": "app.js", "content": c})
			req := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", body, user, parent.ID)
			rec := httptest.NewRecorder()
			srv.handleSaveFile(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("save failed with code %d: %s", rec.Code, rec.Body.String())
			}
		}(content)
	}

	close(startBarrier)
	wg.Wait()

	// Read final file content on disk
	finalBytes, err := os.ReadFile(filepath.Join(workRoot, parent.Subdomain, "app.js"))
	if err != nil {
		t.Fatalf("failed to read final app.js: %v", err)
	}
	finalStr := string(finalBytes)

	// Verify that the final content matches EXACTLY one of the valid full payloads,
	// and is not a corrupted, partial, or mixed payload.
	foundMatch := false
	for i := 0; i < n; i++ {
		expected := fmt.Sprintf("PAYLOAD_VERSION_%04d_REPEAT_%s", i, strings.Repeat("X", 2048))
		if finalStr == expected {
			foundMatch = true
			break
		}
	}
	if !foundMatch {
		t.Fatalf("CRITICAL DATA CORRUPTION: app.js contains partial or mixed write! Length=%d", len(finalStr))
	}
}

// 8. Lock registry cleanup does not allow two independent locks for one sandbox ID.
func TestLockRegistrySingleActiveEntry(t *testing.T) {
	srv, _, _, parent := setupForkSyncServer(t)

	mgr := srv.locks()
	const n = 30
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			lock, err := mgr.AcquireSave(context.Background(), parent.ID)
			if err != nil {
				t.Errorf("AcquireSave error: %v", err)
				return
			}
			// While lock is held, active entries must be exactly 1
			if active := mgr.ActiveEntries(); active != 1 {
				t.Errorf("expected exactly 1 active entry while held, got %d", active)
			}
			lock.Release()
		}()
	}

	close(start)
	wg.Wait()

	if active := mgr.ActiveEntries(); active != 0 {
		t.Fatalf("expected 0 active entries after all goroutines finish, got %d", active)
	}
}

// 9. Shutdown boundary rejects new background tasks and safely cancels in-flight tasks.
func TestShutdownRejectsNewBackgroundTasks(t *testing.T) {
	srv, _, _, _ := setupForkSyncServer(t)

	taskStarted := make(chan struct{})
	taskCancelled := make(chan struct{})

	// Start an in-flight background task
	ok := srv.goBackground(func(ctx context.Context) {
		close(taskStarted)
		<-ctx.Done()
		close(taskCancelled)
	})
	if !ok {
		t.Fatal("expected initial goBackground to succeed")
	}

	<-taskStarted

	// Initiate server shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	shutdownDone := make(chan struct{})
	go func() {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			t.Errorf("srv.Shutdown error: %v", err)
		}
		close(shutdownDone)
	}()

	// The in-flight task must be cancelled by Shutdown
	select {
	case <-taskCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight task was not cancelled by Shutdown")
	}

	<-shutdownDone

	// Once shutdown has begun, any new goBackground call must be rejected immediately
	rejected := srv.goBackground(func(ctx context.Context) {
		t.Error("rejected task should never execute")
	})
	if rejected {
		t.Fatal("expected goBackground to be rejected after shutdown, but it returned true")
	}
}

// 10. Concurrent background task launches racing with Shutdown do not cause data races or panics.
func TestConcurrentBackgroundTasksAndShutdownRace(t *testing.T) {
	srv, _, _, _ := setupForkSyncServer(t)

	const workers = 30
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			srv.goBackground(func(ctx context.Context) {
				// short-lived task
			})
		}()
	}

	close(start)
	_ = srv.Shutdown(context.Background())
	wg.Wait()
}

// 11. Panic in forkDeploy is caught, releases lock, marks child failed, and does not crash process.
func TestForkDeployPanicRecoveryMarksChildFailed(t *testing.T) {
	srv, _, user, parent := setupForkSyncServer(t)

	panicTriggered := make(chan struct{})
	srv.forkHooks.onBeforeForkCopy = func(parentID string) {
		close(panicTriggered)
		panic("simulated catastrophic panic during fork copy")
	}

	forkReq := newAuthRequest(http.MethodPost, "/api/sandboxes/"+parent.ID+"/fork", nil, user, parent.ID)
	forkRec := httptest.NewRecorder()
	srv.handleForkSandbox(forkRec, forkReq)

	if forkRec.Code != http.StatusAccepted {
		t.Fatalf("expected fork status 202 Accepted, got %d", forkRec.Code)
	}

	var child db.Sandbox
	if err := json.Unmarshal(forkRec.Body.Bytes(), &child); err != nil {
		t.Fatalf("failed to decode child sandbox JSON: %v", err)
	}

	<-panicTriggered
	waitForBackgroundTasks(t, srv)

	// Verify child status was updated to failed
	updatedChild, err := srv.store.GetSandbox(child.ID)
	if err != nil {
		t.Fatalf("failed to get child sandbox: %v", err)
	}
	if updatedChild.Status != db.StatusFailed {
		t.Fatalf("expected child status to be failed after panic, got %s", updatedChild.Status)
	}

	// Verify workspace lock was released by panic recovery: subsequent save must succeed
	saveBody, _ := json.Marshal(map[string]any{"path": "app.js", "content": "edit after recovered panic"})
	saveReq := newAuthRequest(http.MethodPut, "/api/sandboxes/"+parent.ID+"/file", saveBody, user, parent.ID)
	saveRec := httptest.NewRecorder()
	srv.handleSaveFile(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected save to succeed after panic recovery, got %d: %s", saveRec.Code, saveRec.Body.String())
	}
}
