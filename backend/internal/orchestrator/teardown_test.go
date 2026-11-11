package orchestrator

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// github_id values here are in the 200s to stay clear of the other packages'
// fixtures: `go test ./...` runs packages in parallel against one database, and
// users.github_id carries a unique constraint.
//
// Teardown is the only thing that clears a destroyed sandbox's metric history,
// because the cascade on resource_snapshots.sandbox_id never fires while the
// sandbox row survives as a tombstone. So the delete itself being correct — which
// the db package tests cover — is not enough; what matters is that teardown
// actually calls it, and that is only observable by running teardown.
//
// This therefore needs the real thing: a real Postgres for the rows and a real
// Docker daemon, because teardown lists the sandbox's containers and removes its
// network. A sandbox with no containers and no sidecar databases is enough — the
// container and network steps are best-effort and only warn — and it is the
// realistic shape for a sandbox whose runtime already died.
//
// Skips unless both are available:
//
//	docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=pw postgres:16-alpine
//	ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' \
//	  go test -run TestTeardown ./internal/orchestrator/
func teardownEnv(t *testing.T) (*Orchestrator, *db.Store, *gorm.DB) {
	t.Helper()

	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping: set ASL_TEST_DATABASE_URL to run the teardown test")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping: cannot reach the test database (%v)", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store := db.NewStore(gdb)

	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	o, err := New(context.Background(), &config.Config{DockerHost: host},
		store, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Skipf("skipping: no usable Docker daemon (%v)", err)
	}
	t.Cleanup(func() { _ = o.Close() })
	return o, store, gdb
}

// teardownSandbox inserts the user, repository, sandbox and its metric history,
// returning the sandbox and a count of live snapshot rows.
func teardownSandbox(t *testing.T, store *db.Store, gdb *gorm.DB, label string, tag int) (*db.Sandbox, func() int64) {
	t.Helper()
	uid := func(tag uint32) string {
		h := fnv.New32a()
		_, _ = h.Write([]byte(label))
		hi := h.Sum32()
		h2 := fnv.New32a()
		_, _ = h2.Write([]byte(label + "/salt"))
		lo := h2.Sum32()
		return sprintfUUID(0x10000000+tag, hi, lo)
	}
	// Cleanup is registered before the first insert: a t.Fatalf between two
	// inserts would otherwise strand the earlier row and the next run of the same
	// test would fail on users_pkey. Deletes are idempotent, so removing rows
	// that were never created is a no-op.
	userID, repoID, sandboxID := uid(1), uid(2), uid(3)
	t.Cleanup(func() {
		gdb.Exec("DELETE FROM resource_snapshots WHERE sandbox_id = ?", sandboxID)
		gdb.Exec("DELETE FROM sandboxes WHERE id = ?", sandboxID)
		gdb.Exec("DELETE FROM repositories WHERE id = ?", repoID)
		gdb.Exec("DELETE FROM users WHERE id = ?", userID)
	})

	user := &db.User{ID: userID, GithubID: int64(tag), Username: "u-" + label,
		Email: label + "@example.test", GithubToken: []byte("x")}
	if err := gdb.Create(user).Error; err != nil {
		t.Fatalf("creating user: %v", err)
	}
	repo := &db.Repository{ID: repoID, UserID: user.ID, GithubURL: "https://github.com/example/" + label}
	if err := gdb.Create(repo).Error; err != nil {
		t.Fatalf("creating repository: %v", err)
	}
	now := time.Now().UTC()
	sb := &db.Sandbox{
		ID: sandboxID, RepositoryID: repo.ID, BranchName: "main", Subdomain: label,
		Status: db.StatusRunning, ImageTag: "latest",
		LifetimeSeconds: 86400, ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(sb).Error; err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}

	base := now.Truncate(time.Second)
	rows := make([]*db.ResourceSnapshot, 0, 5)
	for i := range 5 {
		rows = append(rows, &db.ResourceSnapshot{
			SandboxID: sb.ID, CPUPercent: float32(i), MemoryLimitBytes: 1,
			RecordedAt: base.Add(time.Duration(i) * 10 * time.Second),
		})
	}
	if err := store.InsertResourceSnapshots(rows); err != nil {
		t.Fatalf("seeding snapshots: %v", err)
	}

	count := func() int64 {
		var n int64
		if err := gdb.Model(&db.ResourceSnapshot{}).
			Where("sandbox_id = ?", sb.ID).Count(&n).Error; err != nil {
			t.Fatalf("counting snapshots: %v", err)
		}
		return n
	}

	return sb, count
}

func TestTeardownDeletesResourceSnapshots(t *testing.T) {
	o, store, gdb := teardownEnv(t)
	sb, count := teardownSandbox(t, store, gdb, "teardown", 201)

	if got := count(); got != 5 {
		t.Fatalf("seeded %d snapshots, want 5", got)
	}

	if err := o.TeardownSandbox(context.Background(), sb); err != nil {
		t.Fatalf("TeardownSandbox: %v", err)
	}

	if got := count(); got != 0 {
		t.Errorf("%d snapshots survived teardown; the prune is not wired up", got)
	}
}

// The metadata row is the tombstone the sandbox detail page renders, so teardown
// must not take it with the history.
func TestTeardownKeepsTheSandboxRow(t *testing.T) {
	o, store, gdb := teardownEnv(t)
	sb, _ := teardownSandbox(t, store, gdb, "tombstone", 202)

	if err := o.TeardownSandbox(context.Background(), sb); err != nil {
		t.Fatalf("TeardownSandbox: %v", err)
	}

	var n int64
	if err := gdb.Model(&db.Sandbox{}).Where("id = ?", sb.ID).Count(&n).Error; err != nil {
		t.Fatalf("counting the sandbox: %v", err)
	}
	if n != 1 {
		t.Error("teardown deleted the sandbox row; it must survive as a tombstone")
	}
}

// A teardown of one sandbox must not reach into another's history, since the
// expiry reaper can be walking several sandboxes at once.
func TestTeardownLeavesOtherSandboxesAlone(t *testing.T) {
	o, store, gdb := teardownEnv(t)
	doomed, doomedCount := teardownSandbox(t, store, gdb, "doomed", 203)
	_, otherCount := teardownSandbox(t, store, gdb, "bystander", 204)

	if err := o.TeardownSandbox(context.Background(), doomed); err != nil {
		t.Fatalf("TeardownSandbox: %v", err)
	}

	if got := doomedCount(); got != 0 {
		t.Errorf("the torn-down sandbox kept %d snapshots", got)
	}
	if got := otherCount(); got != 5 {
		t.Errorf("a bystander sandbox has %d snapshots, want its 5 to survive", got)
	}
}

// sprintfUUID builds a valid v4-shaped uuid from two hash words, so the id
// columns (which are uuid in the schema) accept them.
func sprintfUUID(seed, hi, lo uint32) string {
	return fmt.Sprintf("%08x-%04x-4%03x-8%03x-%04x%08x",
		seed, hi&0xffff, lo&0x0fff, hi&0x0fff, lo&0xffff, lo)
}

// Volume labels are what let teardown find a sidecar volume after a deploy
// failed before the sandbox_databases row was written.
func TestVolumeCreateCarriesSandboxLabel(t *testing.T) {
	opts := volumeCreate("sandboxdata_abc123", "sb-123")
	if opts.Name != "sandboxdata_abc123" {
		t.Errorf("Name = %q", opts.Name)
	}
	if opts.Labels[SandboxIDLabel] != "sb-123" {
		t.Errorf("labels = %v, want %s=sb-123", opts.Labels, SandboxIDLabel)
	}
}

// Teardown may only remove images this platform built. A legacy/test row with
// an unrelated tag must never make it delete somebody else's image.
func TestSandboxImageRefOnlyTrustsPlatformTagOrSubdomain(t *testing.T) {
	cases := []struct {
		name string
		sb   *db.Sandbox
		want string
	}{
		{"platform tag", &db.Sandbox{ImageTag: "sandbox-demo:latest"}, "sandbox-demo:latest"},
		{"derived from subdomain", &db.Sandbox{Subdomain: "demo"}, "sandbox-demo:latest"},
		{"unrelated tag", &db.Sandbox{ImageTag: "latest", Subdomain: ""}, ""},
	}
	for _, tc := range cases {
		if got := sandboxImageRef(tc.sb); got != tc.want {
			t.Errorf("%s: sandboxImageRef() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
