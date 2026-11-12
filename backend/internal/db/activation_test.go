package db

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The lifetime is a promise about how long a usable link lives, so the clock has
// to start when the sandbox goes live rather than when the row is created. These
// cover the store method that does the rebasing, including the case that makes it
// subtle: a redeploy also passes through 'running', and must not restart the
// clock or the sandbox would outlive every cap and become immortal.
//
// They skip unless ASL_TEST_DATABASE_URL points at a real Postgres, because the
// rebasing is an atomic conditional UPDATE that only the database can evaluate.
//
//	docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=pw postgres:16-alpine
//	ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' go test -run TestActivation ./internal/db/

// clockSandbox creates a sandbox that has been building for buildFor, so the
// test can assert the deadline moved by the lifetime rather than by the build.
func clockSandbox(t *testing.T, s *Store, label string, tag int, lifetime int64, buildFor time.Duration) *Sandbox {
	t.Helper()
	id := newSandbox(t, s, label, tag)
	created := time.Now().UTC().Add(-buildFor)
	if err := s.db.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"lifetime_seconds": lifetime,
		"status":           string(StatusBuilding),
		"created_at":       created,
		// The provisional deadline the creation path writes.
		"expires_at":   created.Add(time.Duration(lifetime) * time.Second),
		"activated_at": nil,
	}).Error; err != nil {
		t.Fatalf("seeding building sandbox: %v", err)
	}
	got, err := s.GetSandbox(id)
	if err != nil {
		t.Fatalf("re-reading sandbox: %v", err)
	}
	return got
}

// The headline behaviour: build time is not billed against the lifetime.
func TestActivationRebasesExpiryToTheLiveInstant(t *testing.T) {
	s := testDB(t)
	const lifetime = 300 // 5 minutes, the floor
	sb := clockSandbox(t, s, "act-rebase", 101, lifetime, 4*time.Minute)

	before := time.Now()
	activated, err := s.MarkSandboxActive(sb.ID, time.Now().UTC(), map[string]any{
		"status":       string(StatusRunning),
		"container_id": "deadbeef",
	})
	if err != nil {
		t.Fatalf("MarkSandboxActive: %v", err)
	}
	if !activated {
		t.Error("first activation reported false; the clock was not claimed")
	}

	got, err := s.GetSandbox(sb.ID)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}
	if got.ActivatedAt == nil {
		t.Fatal("activated_at is still null after the first activation")
	}
	// A 4-minute build with a 5-minute lifetime: under the old behaviour the
	// sandbox would already be overdue. The whole lifetime must be ahead now.
	remaining := time.Until(got.ExpiresAt)
	if remaining < time.Duration(lifetime)*time.Second-time.Minute {
		t.Errorf("only %s left of a %ds lifetime; build time was billed to it", remaining, lifetime)
	}
	if got.ExpiresAt.Before(before.Add(time.Duration(lifetime) * time.Second)) {
		t.Errorf("expires_at %s is earlier than activation + lifetime", got.ExpiresAt)
	}
	if got.ContainerID != "deadbeef" {
		t.Errorf("container_id = %q, want the running state to be applied", got.ContainerID)
	}
}

// The subtle one: a redeploy walks building -> running again. If this rebased,
// every save-and-redeploy in the editor would hand out another full lifetime.
func TestRedeployDoesNotRestartTheClock(t *testing.T) {
	s := testDB(t)
	const lifetime = 3600
	sb := clockSandbox(t, s, "act-redeploy", 102, lifetime, time.Minute)

	if _, err := s.MarkSandboxActive(sb.ID, time.Now().UTC(), map[string]any{
		"status": string(StatusRunning),
	}); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	first, err := s.GetSandbox(sb.ID)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}

	// Redeploy: back to building, then live again much later.
	if err := s.db.Model(&Sandbox{}).Where("id = ?", sb.ID).
		Update("status", string(StatusBuilding)).Error; err != nil {
		t.Fatalf("marking building: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	activated, err := s.MarkSandboxActive(sb.ID, time.Now().UTC(), map[string]any{
		"status":       string(StatusRunning),
		"container_id": "rebuilt00",
	})
	if err != nil {
		t.Fatalf("redeploy activation: %v", err)
	}
	if activated {
		t.Error("redeploy reported a first activation and would have reset the clock")
	}

	second, err := s.GetSandbox(sb.ID)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}
	if !second.ExpiresAt.Equal(first.ExpiresAt) {
		t.Errorf("expires_at moved from %s to %s on redeploy", first.ExpiresAt, second.ExpiresAt)
	}
	if !second.ActivatedAt.Equal(*first.ActivatedAt) {
		t.Errorf("activated_at moved from %s to %s on redeploy", first.ActivatedAt, second.ActivatedAt)
	}
	// The running state must still be applied, or the redeploy would be a no-op.
	if second.ContainerID != "rebuilt00" {
		t.Errorf("container_id = %q, want the redeployed container", second.ContainerID)
	}
}

// A deploy that finishes after the user destroyed the sandbox must not resurrect
// it, and must say so rather than reporting success.
func TestActivationRefusesADestroyedSandbox(t *testing.T) {
	s := testDB(t)
	sb := clockSandbox(t, s, "act-destroyed", 103, 3600, time.Minute)

	now := time.Now().UTC()
	if err := s.db.Model(&Sandbox{}).Where("id = ?", sb.ID).Updates(map[string]any{
		"destroyed_at":       now,
		"destruction_reason": string(DestroyUserRequested),
		"status":             string(StatusDeleted),
	}).Error; err != nil {
		t.Fatalf("destroying: %v", err)
	}

	activated, err := s.MarkSandboxActive(sb.ID, now.Add(time.Minute), map[string]any{
		"status":       string(StatusRunning),
		"container_id": "zombie001",
	})
	if !errors.Is(err, ErrSandboxDestroyed) {
		t.Fatalf("err = %v, want ErrSandboxDestroyed", err)
	}
	if activated {
		t.Error("a destroyed sandbox reported a first activation")
	}

	got, err := s.GetSandbox(sb.ID)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}
	if got.Status != StatusDeleted {
		t.Errorf("status = %q, want the tombstone left alone", got.Status)
	}
	if got.ContainerID == "zombie001" {
		t.Error("a destroyed sandbox was given a container")
	}
	if got.ActivatedAt != nil {
		t.Error("a destroyed sandbox was marked activated")
	}
}

// The clock must not be claimable twice under concurrency: two deploys racing
// would otherwise both see RowsAffected==0 handling wrong, or worse, both rebase.
func TestActivationIsClaimedExactlyOnceUnderConcurrency(t *testing.T) {
	s := testDB(t)
	sb := clockSandbox(t, s, "act-race", 104, 3600, time.Minute)

	const racers = 6
	results := make(chan bool, racers)
	errs := make(chan error, racers)
	for range racers {
		go func() {
			ok, err := s.MarkSandboxActive(sb.ID, time.Now().UTC(), map[string]any{
				"status": string(StatusRunning),
			})
			results <- ok
			errs <- err
		}()
	}
	wins := 0
	for range racers {
		if err := <-errs; err != nil {
			t.Errorf("concurrent MarkSandboxActive: %v", err)
		}
		if <-results {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d concurrent calls claimed the first activation, want exactly 1", wins)
	}
}

// A sandbox that never activates keeps its creation-time deadline, so a build
// that hangs forever still gets reaped instead of leaking the row.
func TestUnactivatedSandboxKeepsItsProvisionalDeadline(t *testing.T) {
	s := testDB(t)
	const lifetime = 300
	sb := clockSandbox(t, s, "act-hang", 105, lifetime, 0)

	if sb.ActivatedAt != nil {
		t.Fatal("a freshly seeded building sandbox is already marked activated")
	}
	due, err := s.ListDuedExpiredSandboxes(sb.ExpiresAt.Add(time.Second))
	if err != nil {
		t.Fatalf("ListDuedExpiredSandboxes: %v", err)
	}
	var found bool
	for _, d := range due {
		if d.ID == sb.ID {
			found = true
		}
	}
	if !found {
		t.Error("an unactivated sandbox past its provisional deadline is not reaped; the row would leak")
	}
}

// A down migration that has never been executed is worse than none, because a
// rollback in production would fail at the worst possible moment.
//
// This runs on a throwaway database of its own rather than the shared one:
// stepping the ledger down drops a column, and `go test ./...` runs the packages
// in parallel, so doing it on the shared database would break whatever else
// happened to be inserting a sandbox at that moment.
func TestActivationMigrationRollsBackAndForward(t *testing.T) {
	store := scratchDB(t)

	hasColumn := func() bool {
		var n int64
		store.db.Raw("SELECT count(*) FROM information_schema.columns " +
			"WHERE table_name = 'sandboxes' AND column_name = 'activated_at'").Scan(&n)
		return n > 0
	}

	if !hasColumn() {
		t.Fatal("activated_at is missing after migrating; the test would prove nothing")
	}

	sqlDB, err := store.db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	m, err := migrator(sqlDB)
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling back: %v", err)
	}
	if hasColumn() {
		t.Error("activated_at survived the down migration")
	}
	// The ledger must have stepped back exactly one version.
	if got := store.migratorVersion(t); got != 2 {
		t.Errorf("version = %d after the rollback, want 2", got)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-applying: %v", err)
	}
	if !hasColumn() {
		t.Error("activated_at did not come back on re-apply")
	}
	if got := store.migratorVersion(t); got != 3 {
		t.Errorf("version = %d after re-apply, want 3", got)
	}
}

// scratchDB creates a uniquely named database on the same server, migrates it,
// and drops it when the test ends.
func scratchDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping: set ASL_TEST_DATABASE_URL to run the migration test")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping: cannot reach the test database (%v)", err)
	}

	// Unique per process and per run, so two packages testing at once cannot
	// land on the same name.
	name := fmt.Sprintf("asl_migtest_%d_%d", os.Getpid(), time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Skipf("skipping: cannot create a scratch database (%v)", err)
	}

	var gdb *gorm.DB
	t.Cleanup(func() {
		// Close the scratch pool before dropping it: Postgres refuses to drop a
		// database that still has connections, and the pool can lazily reopen one.
		if gdb != nil {
			if sqlDB, err := gdb.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		dropScratch(admin, name)
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parsing ASL_TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	gdb, err = gorm.Open(postgres.Open(u.String()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping: cannot open the scratch database (%v)", err)
	}
	if err := Migrate(gdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewStore(gdb)
}

// dropScratch removes the throwaway database, retrying and then forcing the
// issue if a connection is still holding it open. Leaving these behind would
// fill the server with databases over repeated runs.
func dropScratch(admin *gorm.DB, name string) {
	for range 2 {
		if err := admin.Exec("DROP DATABASE IF EXISTS " + name).Error; err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// PostgreSQL 13+ can terminate the remaining backends for us.
	_ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)").Error
}

// migratorVersion reports the highest applied version on the ledger.
func (s *Store) migratorVersion(t *testing.T) uint {
	t.Helper()
	var v uint
	if err := s.db.Raw("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").
		Scan(&v).Error; err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	return v
}
