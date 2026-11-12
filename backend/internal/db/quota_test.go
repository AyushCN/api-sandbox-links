package db

// Atomic-quota tests for CreateSandboxWithinQuota. Gated on the dedicated
// disposable test database like the rest of this package's tests (testDB in
// queries_test.go):
//
//	ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' \
//	  go test -run TestQuota ./internal/db/

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// quotaOwner creates (or reuses) a user with one repository, and arranges
// that every live sandbox this owner accumulates — in this run or a previous
// one against the same persistent test database — is destroyed at test end
// and at test start, so quota state never leaks between runs. Tags are
// github_id values and carry a unique constraint, so they must differ from
// every other fixture in this package.
func quotaOwner(t *testing.T, s *Store, tag int) (userID, repoID string) {
	t.Helper()
	u := &User{GithubID: int64(tag), Username: fmt.Sprintf("quota%d", tag),
		GithubToken: []byte("dev")} // satisfies the NOT NULL column; never used
	if err := s.UpsertUser(u); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	destroyLive := func() {
		s.db.Exec(`UPDATE sandboxes SET destroyed_at = now()
			WHERE repository_id IN (SELECT id FROM repositories WHERE user_id = ?)
			AND destroyed_at IS NULL`, u.ID)
	}
	destroyLive() // start clean: earlier runs' rows must not count
	t.Cleanup(destroyLive)

	repo, err := s.FindOrCreateRepository(u.ID, fmt.Sprintf("https://github.com/quota%d/app", tag), "main")
	if err != nil {
		t.Fatalf("FindOrCreateRepository: %v", err)
	}
	return u.ID, repo.ID
}

func quotaSandbox(repoID, label string) *Sandbox {
	return &Sandbox{
		RepositoryID:    repoID,
		BranchName:      "main",
		Subdomain:       "q-" + label + "-" + newID()[:8],
		Status:          StatusQueued,
		LifetimeSeconds: 600,
		CreatedAt:       time.Now().UTC(),
	}
}

// Below the cap creates succeed; the next one is refused with the sentinel
// error and — critically — creates no row.
func TestQuotaAllowsUpToCapThenRefuses(t *testing.T) {
	s := testDB(t)
	userID, repoID := quotaOwner(t, s, 9501)
	const cap = 3

	for i := 0; i < cap; i++ {
		if err := s.CreateSandboxWithinQuota(quotaSandbox(repoID, "cap"), userID, cap); err != nil {
			t.Fatalf("create %d of %d: %v", i+1, cap, err)
		}
	}
	err := s.CreateSandboxWithinQuota(quotaSandbox(repoID, "over"), userID, cap)
	if !errors.Is(err, ErrSandboxQuotaExceeded) {
		t.Fatalf("over-cap create: err = %v, want ErrSandboxQuotaExceeded", err)
	}

	var n int64
	if err := s.db.Raw(`SELECT count(*) FROM sandboxes s
		JOIN repositories r ON r.id = s.repository_id
		WHERE r.user_id = ? AND s.destroyed_at IS NULL`, userID).Scan(&n).Error; err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != cap {
		t.Errorf("live rows = %d, want %d (a refused create must insert nothing)", n, cap)
	}
}

// Destroying a sandbox releases its slot immediately — the count excludes
// tombstones, which is what makes the quota about *concurrent* sandboxes.
func TestQuotaReleasesSlotWhenSandboxDestroyed(t *testing.T) {
	s := testDB(t)
	userID, repoID := quotaOwner(t, s, 9502)

	first := quotaSandbox(repoID, "release")
	if err := s.CreateSandboxWithinQuota(first, userID, 1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := s.CreateSandboxWithinQuota(quotaSandbox(repoID, "blocked"), userID, 1); !errors.Is(err, ErrSandboxQuotaExceeded) {
		t.Fatalf("second create: err = %v, want ErrSandboxQuotaExceeded", err)
	}

	// Simulate destroy/expiry the way the handlers and reapers do it.
	if err := s.db.Model(&Sandbox{}).Where("id = ?", first.ID).
		Update("destroyed_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("marking destroyed: %v", err)
	}

	if err := s.CreateSandboxWithinQuota(quotaSandbox(repoID, "after-release"), userID, 1); err != nil {
		t.Fatalf("create after destroy: %v (a destroyed sandbox must release its slot)", err)
	}
}

// Quotas are per owner: one user sitting at the cap must not block another.
func TestQuotaIsIndependentPerUser(t *testing.T) {
	s := testDB(t)
	userA, repoA := quotaOwner(t, s, 9503)
	userB, repoB := quotaOwner(t, s, 9504)

	if err := s.CreateSandboxWithinQuota(quotaSandbox(repoA, "a1"), userA, 1); err != nil {
		t.Fatalf("A's first create: %v", err)
	}
	if err := s.CreateSandboxWithinQuota(quotaSandbox(repoA, "a2"), userA, 1); !errors.Is(err, ErrSandboxQuotaExceeded) {
		t.Fatalf("A over cap: err = %v, want ErrSandboxQuotaExceeded", err)
	}
	if err := s.CreateSandboxWithinQuota(quotaSandbox(repoB, "b1"), userB, 1); err != nil {
		t.Fatalf("B blocked by A's quota: %v", err)
	}
}

// The whole point of the advisory lock: N racing creations against cap C
// must land exactly C rows — no overshoot, no lost update.
func TestQuotaConcurrentCreatesCannotOvershoot(t *testing.T) {
	s := testDB(t)
	userID, repoID := quotaOwner(t, s, 9505)
	const (
		cap      = 3
		attempts = 10
	)

	var (
		wg       sync.WaitGroup
		ok, full atomic.Int32
		other    atomic.Int32
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.CreateSandboxWithinQuota(quotaSandbox(repoID, fmt.Sprintf("conc%d", i)), userID, cap)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrSandboxQuotaExceeded):
				full.Add(1)
			default:
				other.Add(1)
				t.Errorf("attempt %d: unexpected error %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if other.Load() != 0 {
		t.Fatalf("%d attempts failed for reasons other than the quota", other.Load())
	}
	if got := ok.Load(); got != cap {
		t.Errorf("successful creates = %d, want exactly the cap of %d", got, cap)
	}
	if got := full.Load(); got != attempts-cap {
		t.Errorf("refused creates = %d, want %d", got, attempts-cap)
	}

	var n int64
	if err := s.db.Raw(`SELECT count(*) FROM sandboxes s
		JOIN repositories r ON r.id = s.repository_id
		WHERE r.user_id = ? AND s.destroyed_at IS NULL`, userID).Scan(&n).Error; err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != cap {
		t.Errorf("live rows = %d, want %d — concurrent inserts overshot the quota", n, cap)
	}
}

// The global cap applies across users, and a create refused by it inserts no
// row. The test computes the database's existing live count first because the
// cap is platform-wide and the test database is shared.
func TestGlobalCapacityRefusesAcrossUsers(t *testing.T) {
	s := testDB(t)
	userA, repoA := quotaOwner(t, s, 9506)
	userB, repoB := quotaOwner(t, s, 9507)

	var base int64
	if err := s.db.Raw(`SELECT count(*) FROM sandboxes WHERE destroyed_at IS NULL`).Scan(&base).Error; err != nil {
		t.Fatalf("counting live sandboxes: %v", err)
	}
	cap := int(base) + 1

	if err := s.CreateSandboxWithinLimits(quotaSandbox(repoA, "global-a"), userA, 100, cap); err != nil {
		t.Fatalf("A's create below the global cap: %v", err)
	}
	err := s.CreateSandboxWithinLimits(quotaSandbox(repoB, "global-b"), userB, 100, cap)
	if !errors.Is(err, ErrGlobalCapacityReached) {
		t.Fatalf("B's create at the global cap: err = %v, want ErrGlobalCapacityReached", err)
	}

	var n int64
	if err := s.db.Raw(`SELECT count(*) FROM sandboxes WHERE destroyed_at IS NULL`).Scan(&n).Error; err != nil {
		t.Fatalf("counting live sandboxes: %v", err)
	}
	if n != base+1 {
		t.Errorf("live rows = %d, want %d (a refused create must insert nothing)", n, base+1)
	}
}
