package reaper

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// expiryLifecycleOrchestrator exercises the DB/workdir portion of expiry for
// queued sandboxes (which have no runtime resources to destroy).
type expiryLifecycleOrchestrator struct{}

func TestExpiryReaperCleansOnlyExpiredSandboxWorkDir(t *testing.T) {
	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ASL_TEST_DATABASE_URL to run expiry lifecycle test")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("disposable test database unavailable: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	store := db.NewStore(gdb)

	userID, repoID, sandboxID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	name := "expiry-test-" + sandboxID[:8]
	t.Cleanup(func() { _ = gdb.Exec("DELETE FROM users WHERE id = ?", userID).Error })
	user := &db.User{ID: userID, GithubID: 990_001_000 + time.Now().UnixNano()%1_000_000,
		Username: name, Email: name + "@example.test", GithubToken: []byte("test")}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("create disposable test user: %v", err)
	}
	repo := &db.Repository{ID: repoID, UserID: userID, GithubURL: "https://github.com/test/" + name, DefaultBranch: "main"}
	if err := gdb.Create(repo).Error; err != nil {
		t.Fatalf("create disposable test repo: %v", err)
	}
	subdomain := "expiry-" + sandboxID[:8]
	sb := &db.Sandbox{ID: sandboxID, RepositoryID: repoID, BranchName: "main", Subdomain: subdomain,
		Status: db.StatusQueued, LifetimeSeconds: 60, ExpiresAt: time.Now().Add(-time.Minute), CreatedAt: time.Now().Add(-2 * time.Minute)}
	if err := gdb.Create(sb).Error; err != nil {
		t.Fatalf("create disposable sandbox: %v", err)
	}
	workRoot := t.TempDir()
	workDir := filepath.Join(workRoot, subdomain)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "keep-until-expiry"), []byte("sandbox-owned"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A nil Docker orchestrator is safe here: the queued fixture has no
	// ContainerID, so tick skips runtime teardown and post-tombstone sweep.
	reaper := NewExpiryReaper(store, nil, &config.Config{SandboxWorkDir: workRoot},
		slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
	reaper.tick(context.Background())

	fresh, err := store.GetSandbox(sandboxID)
	if err != nil {
		t.Fatalf("reload expired sandbox: %v", err)
	}
	if fresh.DestroyedAt == nil || fresh.Status != db.StatusExpired || fresh.DestructionReason != string(db.DestroyLifetimeExpired) {
		t.Errorf("sandbox state after expiry = status %q destroyed %v reason %q", fresh.Status, fresh.DestroyedAt, fresh.DestructionReason)
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("expired work directory remains (stat error %v)", err)
	}
	if _, err := os.Stat(workRoot); err != nil {
		t.Errorf("expiry cleanup removed work root: %v", err)
	}
}
