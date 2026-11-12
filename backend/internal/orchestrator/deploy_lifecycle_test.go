package orchestrator

// DB-gated lifecycle tests for workdir cleanup, cancellation, and deployment
// status transitions. The only build execution in this file uses a fake
// Nixpacks shell script with the explicitly local development override.

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
	sb := &db.Sandbox{RepositoryID: repo.ID, BranchName: "main", Subdomain: "lc-" + name,
		Status: db.StatusBuilding, LifetimeSeconds: 3600, CreatedAt: now, UpdatedAt: now}
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
	cfg := &config.Config{SandboxWorkDir: root, NixpacksBin: bin}
	o := &Orchestrator{cfg: cfg, store: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
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

func TestDeployLifecycleDestroyDuringBuildConverges(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "destroy-race", 880002)
	tmp := t.TempDir()
	started, signal := filepath.Join(tmp, "build-started"), filepath.Join(tmp, "destroy-done")
	body := fmt.Sprintf(": > '%s'\ni=0\nwhile [ ! -f '%s' ] && [ $i -lt 600 ]; do i=$((i+1)); sleep 0.05; done\nexit 1", started, signal)
	d, root := lifecycleDeployer(t, store, lifecycleNixpacks(t, body))
	workDir := lifecycleSeedWorkDir(t, root, sb, "README.md", "# fixture\n")
	done := make(chan error, 1)
	go func() {
		done <- d.DeploySandbox(context.Background(), sb, DeployOptions{Trigger: db.TriggerSaveRedeploy})
	}()
	waitForFileDeploy(t, started, 10*time.Second)
	if err := store.UpdateSandbox(sb.ID, map[string]any{"status": string(db.StatusDeleted), "destroyed_at": time.Now().UTC(), "container_id": ""}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSandboxWorkDir(root, sb.Subdomain); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signal, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeploySandbox: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("deploy did not finish")
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("workdir remains after destroy/build race: %v", err)
	}
	rows, err := store.ListDeployments(sb.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != string(db.DeployFailed) {
		t.Fatalf("deployments = %+v, err %v; want one failed row", rows, err)
	}
	fresh, err := store.GetSandbox(sb.ID)
	if err != nil || fresh.DestroyedAt == nil {
		t.Fatalf("destroyed tombstone lost: sandbox %+v, err %v", fresh, err)
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
	if err := d.DeploySandbox(context.Background(), sb, DeployOptions{Trigger: db.TriggerInitial}); err != nil {
		t.Fatalf("DeploySandbox: %v", err)
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
