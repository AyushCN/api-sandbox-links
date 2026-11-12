package db

import (
	"fmt"
	"hash/fnv"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These exercise the SQL that unit tests cannot reach: the migration, the
// conflict clause, and the newest-N window query. They skip unless
// ASL_TEST_DATABASE_URL points at a real Postgres, so the suite still runs on a
// machine without one.
//
//	go test -run 'TestMigration|TestSnapshot' ./internal/db/
//
// with e.g. a throwaway container:
//
//	docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=pw postgres:16-alpine
//	ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' go test ./internal/db/
func testDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping: set ASL_TEST_DATABASE_URL to run the database tests")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping: cannot reach the test database (%v)", err)
	}
	if err := Migrate(gdb); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewStore(gdb)
}

// newSandbox inserts the user, repository and sandbox rows the foreign keys
// require, and removes their samples when the test ends.
//
// `label` makes the uuid columns unique; `tag` becomes users.github_id, which
// carries a unique constraint, so it must differ for every sandbox created
// anywhere in this package. Distinct labels alone are not enough.
func newSandbox(t *testing.T, s *Store, label string, tag int) string {
	t.Helper()
	db := s.db
	// The id columns are uuid, so these have to be valid uuids. Hashing the
	// caller's label keeps them distinct and stable across runs, which matters
	// because the migration ledger lives in the same database.
	uid := func(tag uint32) string {
		h := fnv.New32a()
		_, _ = h.Write([]byte(label))
		hi := h.Sum32()
		h2 := fnv.New32a()
		_, _ = h2.Write([]byte(label + "/salt"))
		lo := h2.Sum32()
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%04x%08x",
			0x10000000+tag, tag&0xffff, hi&0xffff, lo&0x0fff, hi&0xffff, lo)
	}

	user := &User{
		ID:       uid(1),
		GithubID: int64(tag), Username: "u-" + label, Email: label + "@example.test", GithubToken: []byte("x"),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("creating user: %v", err)
	}
	repo := &Repository{
		ID:     uid(2),
		UserID: user.ID, GithubURL: "https://github.com/example/" + label,
	}
	if err := db.Create(repo).Error; err != nil {
		t.Fatalf("creating repository: %v", err)
	}
	now := time.Now().UTC()
	sb := &Sandbox{
		ID:           uid(3),
		RepositoryID: repo.ID, BranchName: "main", Subdomain: label,
		Status: StatusRunning, ImageTag: "latest",
		LifetimeSeconds: 86400, ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(sb).Error; err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DeleteResourceSnapshotsForSandbox(sb.ID)
		db.Delete(&Sandbox{ID: sb.ID})
		db.Delete(&Repository{ID: repo.ID})
		db.Delete(&User{ID: user.ID})
	})
	return sb.ID
}

func TestMigrateAppliesSnapshotDedupIndex(t *testing.T) {
	s := testDB(t)

	// Checked by the columns it covers rather than by name: the migration names
	// the index explicitly, but what the ON CONFLICT clause depends on is that
	// some unique index spans exactly (sandbox_id, recorded_at).
	const q = `SELECT count(*) FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		WHERE t.relname = 'resource_snapshots' AND i.indisunique
		  AND (SELECT array_agg(a.attname::text ORDER BY a.attname::text)
		       FROM unnest(i.indkey) k
		       JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k
		      ) = ARRAY['recorded_at','sandbox_id']`
	var count int64
	if err := s.db.Raw(q).Scan(&count).Error; err != nil {
		t.Fatalf("reading pg_index: %v", err)
	}
	if count != 1 {
		t.Errorf("found %d unique indexes on (sandbox_id, recorded_at), want exactly 1", count)
	}
}

// The flush worker runs every 10s, so a retried or overlapping flush must not
// inflate the history the chart draws.
func TestInsertResourceSnapshotsIsIdempotent(t *testing.T) {
	s := testDB(t)
	id := newSandbox(t, s, "idem", 1)
	at := time.Now().UTC().Truncate(time.Second)

	rows := []*ResourceSnapshot{
		{SandboxID: id, CPUPercent: 1, MemoryUsedBytes: 2, MemoryLimitBytes: 3, NetworkRxBytes: 4, NetworkTxBytes: 5, RecordedAt: at},
		{SandboxID: id, CPUPercent: 6, MemoryLimitBytes: 3, RecordedAt: at.Add(10 * time.Second)},
	}
	if err := s.InsertResourceSnapshots(rows); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Same keys again, with different values, as a replayed flush would send.
	replay := []*ResourceSnapshot{
		{SandboxID: id, CPUPercent: 99, MemoryLimitBytes: 3, RecordedAt: at},
		{SandboxID: id, CPUPercent: 98, MemoryLimitBytes: 3, RecordedAt: at.Add(10 * time.Second)},
	}
	if err := s.InsertResourceSnapshots(replay); err != nil {
		t.Fatalf("replayed insert should be a no-op, got: %v", err)
	}

	got, err := s.QueryResourceSnapshots(id, at.Add(-time.Hour), at.Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("QueryResourceSnapshots: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 — the replay duplicated samples", len(got))
	}
	// The first write wins, so a replay cannot rewrite history.
	if got[0].CPUPercent != 1 {
		t.Errorf("cpu_percent = %v, want the originally stored 1", got[0].CPUPercent)
	}
}

// The bug this guards: a 24h window holds ~8,600 rows, so the newest-N cap has
// to select the *newest* n, or the chart shows a stale two-hour slice from the
// start of the window.
func TestQueryResourceSnapshotsReturnsNewestWithinLimit(t *testing.T) {
	s := testDB(t)
	id := newSandbox(t, s, "newest", 2)

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	const total = 50
	rows := make([]*ResourceSnapshot, 0, total)
	for i := range total {
		rows = append(rows, &ResourceSnapshot{
			SandboxID: id, CPUPercent: float32(i), MemoryLimitBytes: 1,
			RecordedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	if err := s.InsertResourceSnapshots(rows); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	got, err := s.QueryResourceSnapshots(id, base.Add(-time.Minute), base.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("QueryResourceSnapshots: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("got %d rows, want 10", len(got))
	}
	// Oldest first for the chart...
	if got[0].CPUPercent != float32(total-10) {
		t.Errorf("first row = %v, want the 10-newest window to start at %d", got[0].CPUPercent, total-10)
	}
	if got[len(got)-1].CPUPercent != float32(total-1) {
		t.Errorf("last row = %v, want the newest sample %d", got[len(got)-1].CPUPercent, total-1)
	}
	// ...and strictly ascending, or the line chart zigzags.
	for i := 1; i < len(got); i++ {
		if !got[i].RecordedAt.After(got[i-1].RecordedAt) {
			t.Fatalf("rows are not ascending by recorded_at at %d", i)
		}
	}
}

// Teardown relies on this to clear a destroyed sandbox's history, since the
// cascade on sandbox_id never fires while the sandbox row survives.
func TestDeleteResourceSnapshotsForSandbox(t *testing.T) {
	s := testDB(t)
	id := newSandbox(t, s, "delme", 3)
	keep := newSandbox(t, s, "keeper", 4)

	at := time.Now().UTC().Truncate(time.Second)
	mk := func(sb string) []*ResourceSnapshot {
		return []*ResourceSnapshot{
			{SandboxID: sb, MemoryLimitBytes: 1, RecordedAt: at},
			{SandboxID: sb, MemoryLimitBytes: 1, RecordedAt: at.Add(time.Second)},
		}
	}
	if err := s.InsertResourceSnapshots(mk(id)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := s.InsertResourceSnapshots(mk(keep)); err != nil {
		t.Fatalf("insert: %v", err)
	}

	n, err := s.DeleteResourceSnapshotsForSandbox(id)
	if err != nil {
		t.Fatalf("DeleteResourceSnapshotsForSandbox: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted %d rows, want 2", n)
	}

	if got, _ := s.QueryResourceSnapshots(id, at.Add(-time.Hour), at.Add(time.Hour), 10); len(got) != 0 {
		t.Errorf("%d rows survived the delete", len(got))
	}
	// Scoped to one sandbox, so a teardown cannot take out a neighbour.
	if got, _ := s.QueryResourceSnapshots(keep, at.Add(-time.Hour), at.Add(time.Hour), 10); len(got) != 2 {
		t.Errorf("other sandbox has %d rows, want its 2 to survive", len(got))
	}
}
