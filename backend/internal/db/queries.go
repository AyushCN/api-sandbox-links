package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store bundles the gorm handle plus every query. Keeping all SQL-shaped
// operations here (rather than scattered in handlers) makes the access layer
// reviewable and testable in one place.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store wrapping the given gorm handle.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("db: store not initialized")
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("db: accessing underlying sql.DB: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

func newID() string {
	return uuid.NewString()
}

// ---- users --------------------------------------------------------------

// UpsertUser inserts the user if new, or updates profile fields and the
// (rotated) GitHub token if the user already exists. GitHub identity is the
// unique key, per the schema.
func (s *Store) UpsertUser(u *User) error {
	if u.ID == "" {
		u.ID = newID()
	}
	existing := &User{}
	err := s.db.Where("github_id = ?", u.GithubID).First(existing).Error
	if err == gorm.ErrRecordNotFound {
		return s.db.Create(u).Error
	}
	if err != nil {
		return fmt.Errorf("db: looking up user: %w", err)
	}
	// Preserve the stable UUID; refresh everything else.
	u.ID = existing.ID
	u.CreatedAt = existing.CreatedAt
	return s.db.Save(u).Error
}

// GetUserByGithubID fetches a user by their GitHub numeric identity.
func (s *Store) GetUserByGithubID(id int64) (*User, error) {
	u := &User{}
	if err := s.db.Where("github_id = ?", id).First(u).Error; err != nil {
		return nil, fmt.Errorf("db: fetching user %d: %w", id, err)
	}
	return u, nil
}

// GetUser fetches a user by internal UUID.
func (s *Store) GetUser(id string) (*User, error) {
	u := &User{}
	if err := s.db.First(u, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("db: fetching user %s: %w", id, err)
	}
	return u, nil
}

// ---- repositories -------------------------------------------------------

// FindOrCreateRepository returns the user's repository row for githubURL,
// creating it on first use.
func (s *Store) FindOrCreateRepository(userID, githubURL, defaultBranch string) (*Repository, error) {
	repo := &Repository{}
	err := s.db.Where("user_id = ? AND github_url = ?", userID, githubURL).First(repo).Error
	if err == nil {
		return repo, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("db: looking up repository: %w", err)
	}
	repo = &Repository{
		ID:            newID(),
		UserID:        userID,
		GithubURL:     githubURL,
		DefaultBranch: defaultBranch,
	}
	if err := s.db.Create(repo).Error; err != nil {
		return nil, fmt.Errorf("db: creating repository: %w", err)
	}
	return repo, nil
}

// GetRepository fetches a repository by ID.
func (s *Store) GetRepository(id string) (*Repository, error) {
	repo := &Repository{}
	if err := s.db.First(repo, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("db: fetching repository %s: %w", id, err)
	}
	return repo, nil
}

// ---- sandboxes ----------------------------------------------------------

// CreateSandbox persists a new sandbox row sized from the deploy request.
//
// ExpiresAt is anchored at CreatedAt here as a provisional deadline: it is what
// cleans up a sandbox whose build never finishes. The real clock starts when the
// sandbox first goes live, via MarkSandboxActive.
func (s *Store) CreateSandbox(sb *Sandbox) error {
	if sb.ID == "" {
		sb.ID = newID()
	}
	sb.ExpiresAt = sb.CreatedAt.Add(time.Duration(sb.LifetimeSeconds) * time.Second)
	return s.db.Create(sb).Error
}

// ErrSandboxQuotaExceeded is returned by CreateSandboxWithinQuota when the
// owning user already has the maximum number of live sandboxes.
var ErrSandboxQuotaExceeded = errors.New("db: user's active sandbox quota is exhausted")

// ErrGlobalCapacityReached is returned when the platform-wide live-sandbox
// cap is already full. It is distinct from a user's quota so the API can tell
// a user to wait for somebody else's sandbox to expire instead of suggesting
// they destroy one of their own.
var ErrGlobalCapacityReached = errors.New("db: platform sandbox capacity is exhausted")

// CreateSandboxWithinQuota creates a sandbox row only while the owning user
// is below maxActive live sandboxes. It is CreateSandboxWithinLimits with no
// platform-wide cap.
func (s *Store) CreateSandboxWithinQuota(sb *Sandbox, ownerUserID string, maxActive int) error {
	return s.CreateSandboxWithinLimits(sb, ownerUserID, maxActive, 0)
}

// CreateSandboxWithinLimits creates a sandbox row only while the owning user
// is below maxActive live sandboxes and, when maxRunning > 0, the platform is
// below maxRunning live sandboxes in total.
//
// The check-and-insert is atomic: per-user and global Postgres advisory locks
// (transaction-scoped, released automatically on commit or rollback) serialize
// the checks across concurrent requests and backend instances, so racing
// creations can never overshoot either cap. The count uses the same tombstone
// definition as the rest of the platform: sandboxes with destroyed_at set
// (manual destroy or lifetime expiry) drop out immediately.
func (s *Store) CreateSandboxWithinLimits(sb *Sandbox, ownerUserID string, maxActive, maxRunning int) error {
	if sb.ID == "" {
		sb.ID = newID()
	}
	sb.ExpiresAt = sb.CreatedAt.Add(time.Duration(sb.LifetimeSeconds) * time.Second)
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Advisory locks take an arbitrary bigint key; hashing the user ID
		// yields a stable per-user key with a 64-bit keyspace.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
			"sandbox-quota:"+ownerUserID).Error; err != nil {
			return fmt.Errorf("db: acquiring sandbox quota lock: %w", err)
		}
		var active int64
		if err := tx.Raw(`SELECT count(*) FROM sandboxes s
			JOIN repositories r ON r.id = s.repository_id
			WHERE r.user_id = ? AND s.destroyed_at IS NULL`, ownerUserID).Scan(&active).Error; err != nil {
			return fmt.Errorf("db: counting active sandboxes: %w", err)
		}
		if active >= int64(maxActive) {
			return ErrSandboxQuotaExceeded
		}
		if maxRunning > 0 {
			// A single global lock serializes every creator, so the global
			// count cannot race. It is taken after the per-user lock, and all
			// creators use the same order, so there is no deadlock cycle.
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
				"sandbox-global-capacity").Error; err != nil {
				return fmt.Errorf("db: acquiring global sandbox capacity lock: %w", err)
			}
			var running int64
			if err := tx.Raw(`SELECT count(*) FROM sandboxes WHERE destroyed_at IS NULL`).Scan(&running).Error; err != nil {
				return fmt.Errorf("db: counting live sandboxes: %w", err)
			}
			if running >= int64(maxRunning) {
				return ErrGlobalCapacityReached
			}
		}
		if err := tx.Create(sb).Error; err != nil {
			return fmt.Errorf("db: creating sandbox: %w", err)
		}
		return nil
	})
}

// ErrSandboxDestroyed is returned by MarkSandboxActive when the row is already a
// tombstone. A deploy that finishes after the user destroyed (or the reaper
// expired) the sandbox must not resurrect it.
var ErrSandboxDestroyed = errors.New("db: sandbox is already destroyed")

// MarkSandboxActive records a transition into 'running' and, on the first one
// only, starts the lifetime clock.
//
// The distinction matters: a redeploy also walks building -> running, so keying
// off status would hand out a fresh full lifetime on every save-and-redeploy and
// make the sandbox immortal. The `activated_at IS NULL` predicate makes the
// rebasing a single atomic claim on the row, so two concurrent deploys cannot
// both win it.
//
// It returns true when this call performed the first activation, and
// ErrSandboxDestroyed if the row is already torn down. `fields` carries the
// running-state columns (container_id, image_tag, ...); expires_at and
// activated_at are owned by this method and are ignored in `fields`.
func (s *Store) MarkSandboxActive(id string, at time.Time, fields map[string]any) (bool, error) {
	// First activation: claim the clock and rebase the deadline in one statement.
	// lifetime_seconds is read from the row rather than passed in, so the new
	// deadline is derived from the same value the reaper and warnings use.
	first := map[string]any{
		"activated_at": at,
		"expires_at":   gorm.Expr("?::timestamptz + (lifetime_seconds * interval '1 second')", at),
	}
	for k, v := range fields {
		first[k] = v
	}
	res := s.db.Model(&Sandbox{}).
		Where("id = ? AND activated_at IS NULL AND destroyed_at IS NULL", id).
		Updates(first)
	if res.Error != nil {
		return false, fmt.Errorf("db: activating sandbox %s: %w", id, res.Error)
	}
	if res.RowsAffected > 0 {
		return true, nil
	}

	// The claim was lost, which is one of two legitimate reasons.
	var sb Sandbox
	if err := s.db.First(&sb, "id = ?", id).Error; err != nil {
		return false, fmt.Errorf("db: reading sandbox %s after failed activation: %w", id, err)
	}
	if sb.DestroyedAt != nil {
		return false, ErrSandboxDestroyed
	}
	// Already activated: a redeploy. Apply the running state but leave the
	// deadline exactly where it was.
	if err := s.db.Model(&Sandbox{}).
		Where("id = ? AND destroyed_at IS NULL", id).
		Updates(fields).Error; err != nil {
		return false, fmt.Errorf("db: updating active sandbox %s: %w", id, err)
	}
	return false, nil
}

// GetSandbox fetches a sandbox by ID.
func (s *Store) GetSandbox(id string) (*Sandbox, error) {
	sb := &Sandbox{}
	if err := s.db.First(sb, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("db: fetching sandbox %s: %w", id, err)
	}
	return sb, nil
}

// GetSandboxBySubdomain fetches a sandbox by its subdomain (used by the
// wake/reverse-proxy gateway to resolve incoming requests).
func (s *Store) GetSandboxBySubdomain(sub string) (*Sandbox, error) {
	sb := &Sandbox{}
	if err := s.db.First(sb, "subdomain = ?", sub).Error; err != nil {
		return nil, fmt.Errorf("db: fetching sandbox by subdomain %s: %w", sub, err)
	}
	return sb, nil
}

// ListSandboxes returns the user's sandboxes, newest first, together with the
// repo URL so the UI can show "this sandbox was forked from X" and recreate.
func (s *Store) ListSandboxes(userID string) ([]*Sandbox, error) {
	var out []*Sandbox
	// Only sandboxes reachable through the user's own repositories are shown.
	sub := s.db.Where("repository_id IN (SELECT id FROM repositories WHERE user_id = ?)", userID)
	if err := sub.Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("db: listing sandboxes: %w", err)
	}
	return out, nil
}

// ListWangableSandboxes lists sandboxes that are still alive (not yet
// destroyed) and not in a terminal failed state for the warnings worker.
func (s *Store) ListActiveSandboxes() ([]*Sandbox, error) {
	var out []*Sandbox
	if err := s.db.Where("destroyed_at IS NULL AND status NOT IN ?",
		[]SandboxStatus{StatusFailed, StatusExpired}).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("db: listing active sandboxes: %w", err)
	}
	return out, nil
}

// ListDuedExpiredSandboxes finds non-destroyed sandboxes whose deadline has
// passed. This is the only query the destroyer worker makes, so it stays on
// the partial index from the schema.
func (s *Store) ListDuedExpiredSandboxes(now time.Time) ([]*Sandbox, error) {
	var out []*Sandbox
	if err := s.db.Where("expires_at < ? AND destroyed_at IS NULL", now).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("db: listing expired sandboxes: %w", err)
	}
	return out, nil
}

// UpdateSandbox applies the non-zero fields of patch to the sandbox row.
func (s *Store) UpdateSandbox(id string, patch map[string]any) error {
	patch["updated_at"] = time.Now()
	if err := s.db.Model(&Sandbox{}).Where("id = ?", id).Updates(patch).Error; err != nil {
		return fmt.Errorf("db: updating sandbox %s: %w", id, err)
	}
	return nil
}

// OwnsSandbox reports whether userID owns the sandbox with the given ID.
func (s *Store) OwnsSandbox(sandboxID, userID string) (bool, error) {
	var count int64
	err := s.db.Model(&Sandbox{}).
		Joins("JOIN repositories ON repositories.id = sandboxes.repository_id").
		Where("sandboxes.id = ? AND repositories.user_id = ?", sandboxID, userID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("db: ownership check: %w", err)
	}
	return count > 0, nil
}

// ---- deployments --------------------------------------------------------

// CreateDeployment records a build attempt in the history table and returns
// the row with its generated ID.
func (s *Store) CreateDeployment(d *Deployment) (*Deployment, error) {
	if d.ID == "" {
		d.ID = newID()
	}
	if err := s.db.Create(d).Error; err != nil {
		return nil, fmt.Errorf("db: creating deployment: %w", err)
	}
	return d, nil
}

// UpdateDeployment patches a deployment row (commonly status and log excerpt).
func (s *Store) UpdateDeployment(id string, patch map[string]any) error {
	if err := s.db.Model(&Deployment{}).Where("id = ?", id).Updates(patch).Error; err != nil {
		return fmt.Errorf("db: updating deployment %s: %w", id, err)
	}
	return nil
}

// ListDeployments returns deploy history for a sandbox, newest first.
func (s *Store) ListDeployments(sandboxID string) ([]*Deployment, error) {
	var out []*Deployment
	if err := s.db.Where("sandbox_id = ?", sandboxID).Order("started_at DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("db: listing deployments: %w", err)
	}
	return out, nil
}

// ---- lifetime warnings --------------------------------------------------

// HasWarningSent reports whether the given threshold warning was already sent.
func (s *Store) HasWarningSent(sandboxID, threshold string) (bool, error) {
	var count int64
	err := s.db.Model(&LifetimeWarning{}).
		Where("sandbox_id = ? AND threshold = ?", sandboxID, threshold).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("db: checking warning: %w", err)
	}
	return count > 0, nil
}

// MarkWarningSent records a threshold warning as delivered.
func (s *Store) MarkWarningSent(sandboxID, threshold string) error {
	w := &LifetimeWarning{ID: newID(), SandboxID: sandboxID, Threshold: threshold}
	return s.db.Create(w).Error
}

// ListWarningThresholds returns the thresholds already sent for a sandbox.
func (s *Store) ListWarningThresholds(sandboxID string) ([]string, error) {
	var out []string
	if err := s.db.Model(&LifetimeWarning{}).
		Where("sandbox_id = ?", sandboxID).
		Pluck("threshold", &out).Error; err != nil {
		return nil, fmt.Errorf("db: listing warnings: %w", err)
	}
	return out, nil
}

// ---- env vars -----------------------------------------------------------

// UpsertEnvVar inserts or replaces one secret env var for a sandbox (values
// stored encrypted; encrypt before calling).
func (s *Store) UpsertEnvVar(sandboxID, key string, value []byte, aiSuggested bool) error {
	v := &EnvVar{}
	err := s.db.Where("sandbox_id = ? AND key = ?", sandboxID, key).First(v).Error
	if err == gorm.ErrRecordNotFound {
		v = &EnvVar{ID: newID(), SandboxID: sandboxID, Key: key, Value: value, IsAISuggested: aiSuggested}
		if err := s.db.Create(v).Error; err != nil {
			return fmt.Errorf("db: creating env var: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("db: looking up env var: %w", err)
	}
	return s.db.Model(v).Where("id = ?", v.ID).
		Updates(map[string]any{"value": value, "is_ai_suggested": aiSuggested}).Error
}

// ListEnvVars returns all env vars for a sandbox (encrypted values).
func (s *Store) ListEnvVars(sandboxID string) ([]*EnvVar, error) {
	var out []*EnvVar
	if err := s.db.Where("sandbox_id = ?", sandboxID).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("db: listing env vars: %w", err)
	}
	return out, nil
}

// ---- sidecar databases --------------------------------------------------

// CreateDatabase records a provisioned sidecar database.
func (s *Store) CreateDatabase(d *SandboxDatabase) error {
	if d.ID == "" {
		d.ID = newID()
	}
	return s.db.Create(d).Error
}

// ListSandboxDatabases returns the sidecar DBs attached to a sandbox.
func (s *Store) ListSandboxDatabases(sandboxID string) ([]*SandboxDatabase, error) {
	var out []*SandboxDatabase
	if err := s.db.Where("sandbox_id = ?", sandboxID).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("db: listing sandbox databases: %w", err)
	}
	return out, nil
}

// DeleteSandboxDatabases drops the sidecar metadata for a sandbox. Teardown
// calls it after the containers and volumes are gone: without this, the row
// outlives the data it describes and a later deploy can believe a sidecar
// exists when it does not.
func (s *Store) DeleteSandboxDatabases(sandboxID string) (int64, error) {
	res := s.db.Where("sandbox_id = ?", sandboxID).Delete(&SandboxDatabase{})
	if res.Error != nil {
		return 0, fmt.Errorf("db: deleting sandbox databases for %s: %w", sandboxID, res.Error)
	}
	return res.RowsAffected, nil
}

// GetSandboxDatabase fetches one sidecar DB by id.
func (s *Store) GetSandboxDatabase(id string) (*SandboxDatabase, error) {
	d := &SandboxDatabase{}
	if err := s.db.First(d, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("db: fetching sandbox database %s: %w", id, err)
	}
	return d, nil
}

// ---- resource snapshots -------------------------------------------------

// InsertResourceSnapshots bulk-inserts buffered resource samples.
//
// A conflict on (sandbox_id, recorded_at) is expected rather than exceptional:
// the flush worker reads one Redis key per sandbox, so a collector that restarts
// between two passes can leave the key holding a sample the previous pass
// already stored. ON CONFLICT DO NOTHING makes that a no-op instead of aborting
// the whole batch, which would otherwise throw away every other sandbox's
// reading along with it.
func (s *Store) InsertResourceSnapshots(rows []*ResourceSnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "sandbox_id"}, {Name: "recorded_at"}},
		DoNothing: true,
	}).CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("db: inserting resource snapshots: %w", err)
	}
	return nil
}

// DeleteResourceSnapshotsForSandbox drops every sample belonging to a sandbox.
// Teardown calls it so a destroyed sandbox does not leave behind rows that
// nothing will ever read; the cascade on sandbox_id cannot do this job, because
// the sandbox row survives as a tombstone.
func (s *Store) DeleteResourceSnapshotsForSandbox(sandboxID string) (int64, error) {
	res := s.db.Where("sandbox_id = ?", sandboxID).Delete(&ResourceSnapshot{})
	if res.Error != nil {
		return 0, fmt.Errorf("db: deleting resource snapshots for %s: %w", sandboxID, res.Error)
	}
	return res.RowsAffected, nil
}

// QueryResourceSnapshots returns samples within a window, oldest first.
//
// limit caps how many rows come back, and it is the newest ones that matter: a
// 24h window holds ~8,600 rows at the flush cadence, so `ORDER BY recorded_at
// ASC LIMIT n` would return the *oldest* n and the chart would show a stale
// two-hour slice from the start of the window. Selecting the newest n in an
// inner query and re-sorting ascending in the outer one keeps the cap while
// still handing back points in the order they were recorded.
func (s *Store) QueryResourceSnapshots(sandboxID string, since, until time.Time, limit int) ([]*ResourceSnapshot, error) {
	var out []*ResourceSnapshot
	const q = `SELECT * FROM (
		SELECT * FROM resource_snapshots
		WHERE sandbox_id = ? AND recorded_at BETWEEN ? AND ?
		ORDER BY recorded_at DESC
		LIMIT ?
	) AS recent
	ORDER BY recent.recorded_at ASC`
	if err := s.db.Raw(q, sandboxID, since, until, limit).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("db: querying resource snapshots: %w", err)
	}
	return out, nil
}

// DeleteResourceSnapshotsBefore drops samples older than the cutoff and returns
// how many went. resource_snapshots is append-only and would otherwise grow
// forever; the snapshot reaper calls this on a timer. The recorded_at index
// makes the range delete cheap.
func (s *Store) DeleteResourceSnapshotsBefore(cutoff time.Time) (int64, error) {
	res := s.db.Where("recorded_at < ?", cutoff).Delete(&ResourceSnapshot{})
	if res.Error != nil {
		return 0, fmt.Errorf("db: deleting resource snapshots older than %s: %w",
			cutoff.UTC().Format(time.RFC3339), res.Error)
	}
	return res.RowsAffected, nil
}

// ---- file edits ---------------------------------------------------------

// RecordFileEdit logs that a user edited a file in a sandbox.
func (s *Store) RecordFileEdit(sandboxID, path, userID string) error {
	e := &FileEdit{ID: newID(), SandboxID: sandboxID, FilePath: path, EditedBy: userID}
	return s.db.Create(e).Error
}

// MarkFileEditsPushed sets pushed_to_github for a sandbox's pending edits.
func (s *Store) MarkFileEditsPushed(sandboxID string) error {
	return s.db.Model(&FileEdit{}).
		Where("sandbox_id = ? AND pushed_to_github = ?", sandboxID, false).
		Update("pushed_to_github", true).Error
}

// ---- build queue recovery & reconciliation ------------------------------

// RecoverStuckDeployments reconciles in-flight deployments and building sandboxes
// after a worker or backend process restart, or after exceeding a heartbeat timeout.
// It marks any pending/building deployment as failed and transitions any
// queued/building sandbox to failed.
func (s *Store) RecoverStuckDeployments(olderThan time.Duration, reason string) (int64, error) {
	if reason == "" {
		reason = "build aborted: worker process restarted while build was in progress"
	}
	now := time.Now().UTC()

	dQuery := s.db.Model(&Deployment{}).Where("status IN ?", []string{string(DeployPending), string(DeployBuilding)})
	if olderThan > 0 {
		cutoff := now.Add(-olderThan)
		dQuery = dQuery.Where("started_at < ?", cutoff)
	}

	res := dQuery.Updates(map[string]any{
		"status":      string(DeployFailed),
		"log_excerpt": reason,
		"finished_at": now,
	})
	if res.Error != nil {
		return 0, fmt.Errorf("db: recovering stuck deployments: %w", res.Error)
	}
	affected := res.RowsAffected

	sbQuery := s.db.Model(&Sandbox{}).
		Where("status IN ? AND destroyed_at IS NULL", []SandboxStatus{StatusQueued, StatusBuilding})
	if olderThan > 0 {
		cutoff := now.Add(-olderThan)
		sbQuery = sbQuery.Where("updated_at < ?", cutoff)
	}
	if err := sbQuery.Updates(map[string]any{
		"status": string(StatusFailed),
	}).Error; err != nil {
		return affected, fmt.Errorf("db: recovering stuck sandboxes: %w", err)
	}

	return affected, nil
}

