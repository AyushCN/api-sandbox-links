package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func lifecycleEnv(t *testing.T) (*db.Store, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ASL_TEST_DATABASE_URL to run deploy lifecycle tests")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("disposable test database unavailable: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db.NewStore(gdb), gdb
}

func lifecycleSandbox(t *testing.T, store *db.Store, gdb *gorm.DB, name string, tag int64) *db.Sandbox {
	t.Helper()
	purge := func() { _ = gdb.Exec("DELETE FROM users WHERE github_id = ?", tag).Error }
	purge()
	t.Cleanup(purge)
	u := &db.User{GithubID: tag, Username: "lc-" + name, Email: name + "@example.test", GithubToken: []byte("dev")}
	if err := store.UpsertUser(u); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	repo, err := store.FindOrCreateRepository(u.ID, fmt.Sprintf("https://github.com/lifecycle-%s/app", name), "main")
	if err != nil {
		t.Fatalf("FindOrCreateRepository: %v", err)
	}
	now := time.Now().UTC()
	sb := &db.Sandbox{
		RepositoryID:    repo.ID,
		BranchName:      "main",
		Subdomain:       "lc-" + name,
		Status:          db.StatusBuilding,
		LifetimeSeconds: 3600,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.CreateSandbox(sb); err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	return sb
}

func lifecycleNixpacks(t *testing.T, buildBody string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture requires POSIX shell")
	}
	bin := filepath.Join(t.TempDir(), "nixpacks")
	script := "#!/bin/sh\nif [ \"$1\" = \"plan\" ]; then\n" +
		"echo '{\"providers\":[\"node\"],\"start\":{\"cmd\":\"npm start\"},\"variables\":{}}'\nexit 0\nfi\n" + buildBody + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake nixpacks: %v", err)
	}
	return bin
}

func lifecycleDeployer(t *testing.T, store *db.Store, bin string) (*Deployer, string) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{SandboxWorkDir: root, NixpacksBin: bin, Builder: "nixpacks"}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	o := &Orchestrator{cfg: cfg, store: store, logger: l}
	b := NewNixpacksBuilder(bin, l)
	o.SetBuilder(b)
	return &Deployer{cfg: cfg, store: store, orch: o}, root
}

func lifecycleSeedWorkDir(t *testing.T, root string, sb *db.Sandbox, file, content string) string {
	t.Helper()
	dir := filepath.Join(root, sb.Subdomain)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if file != "" {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func waitForFileDeploy(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s not created within %s", path, within)
}

// A second deploy must block on the semaphore until the first releases it, and
// a cancelled waiter must return instead of hanging.
func TestAcquireBuildSlotQueuesAndHonoursCancellation(t *testing.T) {
	d := &Deployer{buildSem: make(chan struct{}, 1)}
	release, err := d.acquireBuildSlot(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := d.acquireBuildSlot(waitCtx); err == nil {
		t.Fatal("second acquire succeeded while the only slot was held")
	}

	release()
	thirdCtx, thirdCancel := context.WithTimeout(context.Background(), time.Second)
	defer thirdCancel()
	releaseThird, err := d.acquireBuildSlot(thirdCtx)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	releaseThird()
}

func TestCleanupWorkDirIfDestroyedOnlyTouchesTombstones(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "cleanup-unit", 880004)
	d, root := lifecycleDeployer(t, store, lifecycleNixpacks(t, "exit 0"))
	workDir := lifecycleSeedWorkDir(t, root, sb, "keep.txt", "x")
	if d.cleanupWorkDirIfDestroyed(sb.ID, sb.Subdomain) {
		t.Fatal("cleanup fired for live sandbox")
	}
	if err := store.UpdateSandbox(sb.ID, map[string]any{"status": string(db.StatusDeleted), "destroyed_at": time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if !d.cleanupWorkDirIfDestroyed(sb.ID, sb.Subdomain) {
		t.Fatal("cleanup did not fire for tombstone")
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("tombstone workdir remains: %v", err)
	}
	if !d.cleanupWorkDirIfDestroyed(sb.ID, sb.Subdomain) {
		t.Error("repeated cleanup did not converge")
	}
}

func TestRemoveSandboxWorkDirPathSafety(t *testing.T) {
	root := t.TempDir()

	// 1. Refuses empty subdomain
	if err := RemoveSandboxWorkDir(root, ""); err == nil {
		t.Error("expected error for empty subdomain")
	}
	if err := RemoveSandboxWorkDir(root, "   "); err == nil {
		t.Error("expected error for blank subdomain")
	}

	// 2. Refuses path traversal attempts
	if err := RemoveSandboxWorkDir(root, "../outside"); err == nil {
		t.Error("expected error for traversal attempt")
	}
	if err := RemoveSandboxWorkDir(root, "sub/dir"); err == nil {
		t.Error("expected error for path containing separator")
	}

	// 3. Successfully deletes valid child directory
	validDir := filepath.Join(root, "valid-subdomain")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSandboxWorkDir(root, "valid-subdomain"); err != nil {
		t.Fatalf("unexpected error removing valid dir: %v", err)
	}
	if _, err := os.Stat(validDir); !os.IsNotExist(err) {
		t.Error("validDir was not deleted")
	}
}

func TestDeployLifecycleRefusesAlreadyDestroyedSandbox(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "refuse", 880001)
	if err := store.UpdateSandbox(sb.ID, map[string]any{"status": string(db.StatusDeleted), "destroyed_at": time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	d, workRoot := lifecycleDeployer(t, store, lifecycleNixpacks(t, "exit 0"))
	if err := d.DeploySandbox(context.Background(), sb, DeployOptions{Trigger: db.TriggerSaveRedeploy}); err == nil || !strings.Contains(err.Error(), "destroyed") {
		t.Fatalf("DeploySandbox error = %v, want destroyed refusal", err)
	}
	if _, err := os.Stat(filepath.Join(workRoot, sb.Subdomain)); !os.IsNotExist(err) {
		t.Errorf("workdir created for destroyed sandbox: %v", err)
	}
	rows, err := store.ListDeployments(sb.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deployment rows %d, err %v; want none", len(rows), err)
	}
}

func TestDeployLifecycleCancelledBuildKeepsLiveWorkDir(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "cancel", 880003)
	started := filepath.Join(t.TempDir(), "build-started")
	d, root := lifecycleDeployer(t, store, lifecycleNixpacks(t, fmt.Sprintf(": > '%s'\nsleep 30\nexit 1", started)))
	workDir := lifecycleSeedWorkDir(t, root, sb, "user-edit.txt", "precious edits")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.DeploySandbox(ctx, sb, DeployOptions{Trigger: db.TriggerSaveRedeploy}) }()
	waitForFileDeploy(t, started, 10*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("cancelled deploy did not return promptly")
	}
	if _, err := os.Stat(filepath.Join(workDir, "user-edit.txt")); err != nil {
		t.Errorf("live sandbox edit lost: %v", err)
	}
	fresh, err := store.GetSandbox(sb.ID)
	if err != nil || fresh.DestroyedAt != nil || fresh.Status != db.StatusFailed {
		t.Fatalf("sandbox state after cancelled build = %+v, err %v", fresh, err)
	}
	rows, err := store.ListDeployments(sb.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != string(db.DeployFailed) || !strings.Contains(rows[0].LogExcerpt, "canceled") {
		t.Fatalf("deployment rows = %+v, err %v; want cancellation failure", rows, err)
	}
}

func TestDeployLifecycleFailedBuildIsRecorded(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "build-fails", 880005)
	d, root := lifecycleDeployer(t, store, lifecycleNixpacks(t, "echo intentional-failure >&2\nexit 7"))
	workDir := lifecycleSeedWorkDir(t, root, sb, "package.json", `{"scripts":{"start":"node index.js"}}`)
	err := d.DeploySandbox(context.Background(), sb, DeployOptions{Trigger: db.TriggerInitial})
	if err == nil {
		t.Fatal("expected DeploySandbox to return build error, got nil")
	}
	fresh, err := store.GetSandbox(sb.ID)
	if err != nil || fresh.Status != db.StatusFailed {
		t.Fatalf("sandbox state = %+v, err %v; want failed", fresh, err)
	}
	rows, err := store.ListDeployments(sb.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != string(db.DeployFailed) {
		t.Fatalf("deployment rows = %+v, err %v; want one failed row", rows, err)
	}
	if !strings.Contains(rows[0].LogExcerpt, "exit status 7") || !strings.Contains(rows[0].LogExcerpt, "intentional-failure") {
		t.Errorf("failure log = %q", rows[0].LogExcerpt)
	}
	if _, err := os.Stat(filepath.Join(workDir, "package.json")); err != nil {
		t.Errorf("failed build discarded source workdir: %v", err)
	}
}

func TestDeployerRecoverStuckBuildsGuards(t *testing.T) {
	ctx := context.Background()

	// 1. Nil deployer
	var nilDeployer *Deployer
	n, err := nilDeployer.RecoverStuckBuilds(ctx)
	if err != nil || n != 0 {
		t.Errorf("expected (0, nil), got (%d, %v)", n, err)
	}

	// 2. Deployer with nil store
	d := &Deployer{}
	n, err = d.RecoverStuckBuilds(ctx)
	if err != nil || n != 0 {
		t.Errorf("expected (0, nil), got (%d, %v)", n, err)
	}
	n, err = d.SweepTimedOutBuilds(ctx)
	if err != nil || n != 0 {
		t.Errorf("expected (0, nil), got (%d, %v)", n, err)
	}
}

func TestDeployerRecoverStuckBuildsIntegration(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "stuck-build", 880006)
	d, _ := lifecycleDeployer(t, store, lifecycleNixpacks(t, "exit 0"))

	// Create an in-flight deployment
	dep, err := store.CreateDeployment(&db.Deployment{
		SandboxID: sb.ID,
		Trigger:   string(db.TriggerInitial),
		Status:    string(db.DeployBuilding),
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	recovered, err := d.RecoverStuckBuilds(context.Background())
	if err != nil {
		t.Fatalf("RecoverStuckBuilds: %v", err)
	}
	if recovered < 1 {
		t.Fatalf("expected at least 1 recovered deployment, got %d", recovered)
	}

	// Verify deployment was marked failed
	deps, err := store.ListDeployments(sb.ID)
	if err != nil || len(deps) == 0 {
		t.Fatalf("failed to list deployments: %v", err)
	}
	found := false
	for _, it := range deps {
		if it.ID == dep.ID {
			found = true
			if it.Status != string(db.DeployFailed) {
				t.Errorf("expected deployment status failed, got %s", it.Status)
			}
			if !strings.Contains(it.LogExcerpt, "worker process restarted") {
				t.Errorf("expected restart message in log, got %s", it.LogExcerpt)
			}
		}
	}
	if !found {
		t.Fatal("created deployment not found")
	}

	// Verify sandbox was marked failed
	fresh, err := store.GetSandbox(sb.ID)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}
	if fresh.Status != db.StatusFailed {
		t.Errorf("expected sandbox status failed, got %s", fresh.Status)
	}
}

// TestDeployLifecycleRaceWithDeletionUsingBarriers tests the deterministic sequence:
// 1. Deployment passes its destroyed-state check after cloning.
// 2. Deployment pauses before runtime activation (during build phase via barrier file).
// 3. Deletion and cleanup occur while deployment is paused.
// 4. Deployment is resumed.
// 5. The conditional database update rejects activation (ErrSandboxDestroyed).
// 6. Deployer tears down container and removes workspace directory so no resources remain.
func TestDeployLifecycleRaceWithDeletionUsingBarriers(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "race-barrier", 880005)

	barrierDir := t.TempDir()
	pausedFile := filepath.Join(barrierDir, "deploy_paused")
	resumeFile := filepath.Join(barrierDir, "deploy_resume")

	// Nixpacks fake script that signals pausedFile and waits for resumeFile
	buildBody := fmt.Sprintf(": > '%s'\nwhile [ ! -f '%s' ]; do sleep 0.05; done\nexit 0\n", pausedFile, resumeFile)
	d, workRoot := lifecycleDeployer(t, store, lifecycleNixpacks(t, buildBody))

	// Pre-seed workdir so clone is skipped and passes destroyed-state check
	workDir := lifecycleSeedWorkDir(t, workRoot, sb, "index.js", "console.log('hi')")

	deployDone := make(chan error, 1)
	go func() {
		deployDone <- d.DeploySandbox(context.Background(), sb, DeployOptions{Trigger: db.TriggerSaveRedeploy})
	}()

	// 1 & 2. Wait until deployment reaches the build pause barrier (after initial destroyed check)
	waitForFileDeploy(t, pausedFile, 10*time.Second)

	// 3. Begin sandbox deletion and cleanup while deployment is paused
	now := time.Now().UTC()
	if err := store.UpdateSandbox(sb.ID, map[string]any{
		"status":       string(db.StatusDeleted),
		"destroyed_at": now,
	}); err != nil {
		t.Fatalf("failed to update sandbox to destroyed: %v", err)
	}
	if err := RemoveSandboxWorkDir(workRoot, sb.Subdomain); err != nil {
		t.Fatalf("failed to remove workdir: %v", err)
	}

	// Verify workspace directory is deleted
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("expected workdir to be deleted during teardown")
	}

	// 4. Resume deployment
	if err := os.WriteFile(resumeFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("failed to write resumeFile: %v", err)
	}

	// Wait for DeploySandbox to finish
	select {
	case err := <-deployDone:
		_ = err
	case <-time.After(15 * time.Second):
		t.Fatal("DeploySandbox timed out waiting for completion")
	}

	// 5. Verify deployment is marked failed in DB with explanation
	deps, err := store.ListDeployments(sb.ID)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("expected at least one deployment record")
	}
	latest := deps[0]
	if latest.Status != string(db.DeployFailed) {
		t.Errorf("expected deployment status %s, got %s", db.DeployFailed, latest.Status)
	}
	if !strings.Contains(latest.LogExcerpt, "sandbox was destroyed while this deployment was still building") {
		t.Errorf("expected destroyed excerpt in deployment log, got %s", latest.LogExcerpt)
	}

	// 6. Verify sandbox status in DB remains deleted (NOT resurrected to running!)
	fresh, err := store.GetSandbox(sb.ID)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}
	if fresh.Status != db.StatusDeleted {
		t.Errorf("expected sandbox status to remain %s, got %s", db.StatusDeleted, fresh.Status)
	}
	if fresh.DestroyedAt == nil {
		t.Errorf("expected DestroyedAt to be preserved")
	}

	// Verify workdir does NOT exist on disk
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("work directory remains on disk after destroyed deploy: %v", err)
	}
}
