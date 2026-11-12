// Package db is the Postgres access layer. It defines GORM models that mirror
// the SQL schema applied by the numbered migrations in backend/migrations/,
// plus every query the rest of the backend needs. Schema changes always go
// through new migration files first; the models here must be kept in sync.
package db

import (
	"time"

	"gorm.io/datatypes"
)

// ---- Typed status constants ---------------------------------------------
// No magic strings: every status value in the codebase flows through one of
// these typed constants so the compiler catches typos.

// SandboxStatus describes the lifecycle of a sandbox.
type SandboxStatus string

const (
	StatusQueued     SandboxStatus = "queued"     // accepted, deployment pending
	StatusBuilding   SandboxStatus = "building"   // cloning / building image
	StatusRunning    SandboxStatus = "running"    // container up and reachable
	StatusHibernated SandboxStatus = "hibernated" // paused by idle timeout (reversible)
	StatusFailed     SandboxStatus = "failed"     // build/deploy failed
	StatusExpired    SandboxStatus = "expired"    // destroyed by lifetime worker (tombstone)
	StatusDeleted    SandboxStatus = "deleted"    // manually torn down before expiry
)

// DeploymentStatus tracks an individual build/deploy attempt.
type DeploymentStatus string

const (
	DeployPending  DeploymentStatus = "pending"
	DeployBuilding DeploymentStatus = "building"
	DeploySuccess  DeploymentStatus = "success"
	DeployFailed   DeploymentStatus = "failed"
)

// DeploymentTrigger records what started a deploy.
type DeploymentTrigger string

const (
	TriggerInitial      DeploymentTrigger = "initial"
	TriggerSaveRedeploy DeploymentTrigger = "save_redeploy"
	TriggerGithubPush   DeploymentTrigger = "github_push"
	TriggerManual       DeploymentTrigger = "manual"
	TriggerFork         DeploymentTrigger = "fork"
)

// DestructionReason explains why a sandbox was permanently destroyed.
type DestructionReason string

const (
	DestroyLifetimeExpired DestructionReason = "lifetime_expired"
	DestroyUserRequested   DestructionReason = "user_requested"
)

// ---- Models -------------------------------------------------------------

// User is a GitHub-authenticated account.
type User struct {
	ID          string    `gorm:"type:uuid;primaryKey"`
	GithubID    int64     `gorm:"uniqueIndex;not null"`
	Username    string    `gorm:"not null"`
	Email       string
	AvatarURL   string
	GithubToken []byte    `gorm:"not null"` // AES-GCM encrypted at rest
	CreatedAt   time.Time `gorm:"default:now()"`
}

// Repository is a GitHub repo a user has connected at least once.
type Repository struct {
	ID            string    `gorm:"type:uuid;primaryKey"`
	UserID        string    `gorm:"type:uuid;not null"`
	GithubURL     string    `gorm:"not null"`
	DefaultBranch string    `gorm:"not null;default:main"`
	CreatedAt     time.Time `gorm:"default:now()"`
}

// TableName keeps GORM from pluralizing to "repositorie".
func (Repository) TableName() string { return "repositories" }

// Sandbox is the core entity: one running backend instance with its own
// lifetime and idle-timeout controls.
type Sandbox struct {
	ID                 string         `gorm:"type:uuid;primaryKey"`
	RepositoryID       string         `gorm:"type:uuid;not null"`
	ParentSandboxID    *string        `gorm:"type:uuid"`
	BranchName         string         `gorm:"not null"`
	Subdomain          string         `gorm:"uniqueIndex;not null"`
	Status             SandboxStatus  `gorm:"not null;default:queued"`
	ContainerID        string
	ImageTag           string
	InternalPort       int
	IdleTimeoutSeconds int            `gorm:"not null;default:900"`
	LastRequestAt      *time.Time

	LifetimeSeconds   int64     `gorm:"not null;default:86400"`
	ExpiresAt         time.Time `gorm:"not null"`
	// ActivatedAt is when the sandbox first reached running, and is the instant
	// the lifetime clock starts from. It stays nil until then, which is what
	// tells a first activation apart from a redeploy.
	ActivatedAt       *time.Time
	DestroyedAt       *time.Time
	DestructionReason string

	DetectedLanguage string         `gorm:"type:text"`
	BuildConfig      datatypes.JSON `gorm:"type:jsonb"`
	AISuggestedEnv   datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt        time.Time      `gorm:"default:now()"`
	UpdatedAt        time.Time      `gorm:"default:now()"`
}

// LifetimeWarning records that a particular expiry threshold ("24h", "1h",
// "5m", or a proportional equivalent) has already been sent, so the warnings
// worker never duplicates notifications.
type LifetimeWarning struct {
	ID        string    `gorm:"type:uuid;primaryKey"`
	SandboxID string    `gorm:"type:uuid;uniqueIndex:uniq_sandbox_threshold,priority:1;not null"`
	Threshold string    `gorm:"uniqueIndex:uniq_sandbox_threshold,priority:2;not null"`
	SentAt    time.Time `gorm:"default:now()"`
}

// TableName matches the migration's 0001_init table name.
func (LifetimeWarning) TableName() string { return "sandbox_lifetime_warnings" }

// EnvVar is one decrypted-on-read environment variable per sandbox.
// The DB stores the AES-encrypted value; the API layer encrypts on write and
// decrypts only when injecting into a container.
type EnvVar struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	SandboxID     string `gorm:"type:uuid;uniqueIndex:uniq_sandbox_key,priority:1;not null"`
	Key           string `gorm:"uniqueIndex:uniq_sandbox_key,priority:2;not null"`
	Value         []byte `gorm:"not null"` // encrypted
	IsAISuggested bool   `gorm:"not null;default:false"`
}

// TableName matches the migration's 0001_init table name.
func (EnvVar) TableName() string { return "sandbox_env_vars" }

// SandboxDatabase is a sidecar DB container (per-sandbox, on the sandbox's
// private Docker network).
type SandboxDatabase struct {
	ID            string    `gorm:"type:uuid;primaryKey"`
	SandboxID     string    `gorm:"type:uuid;not null"`
	Engine        string    `gorm:"not null"` // postgres | mysql | sqlite
	ContainerID   string
	ConnectionURL []byte    `gorm:"not null"` // encrypted
	ForkedFrom    *string   `gorm:"type:uuid"`
	CreatedAt     time.Time `gorm:"default:now()"`
}

// Deployment is one entry in a sandbox's deploy/build history.
type Deployment struct {
	ID              string     `gorm:"type:uuid;primaryKey" json:"id"`
	SandboxID       string     `gorm:"type:uuid;not null" json:"sandbox_id"`
	Trigger         string     `gorm:"not null" json:"trigger"`
	Status          string     `gorm:"not null" json:"status"`
	LogExcerpt      string     `gorm:"type:text" json:"log_excerpt"`
	Builder         string     `gorm:"type:text" json:"builder,omitempty"`
	BuildDurationMs *int64     `json:"build_duration_ms,omitempty"`
	CacheHit        *bool      `json:"cache_hit,omitempty"`
	ImageSizeBytes  *int64     `json:"image_size_bytes,omitempty"`
	StartedAt       time.Time  `gorm:"not null;default:now()" json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

// ResourceSnapshot is a persisted CPU/memory/network sample.
type ResourceSnapshot struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SandboxID        string    `gorm:"type:uuid;not null" json:"sandbox_id"`
	CPUPercent       float32   `gorm:"not null" json:"cpu_percent"`
	MemoryUsedBytes  int64     `gorm:"not null" json:"memory_used_bytes"`
	MemoryLimitBytes int64     `gorm:"not null" json:"memory_limit_bytes"`
	NetworkRxBytes   int64     `gorm:"not null" json:"network_rx_bytes"`
	NetworkTxBytes   int64     `gorm:"not null" json:"network_tx_bytes"`
	RecordedAt       time.Time `gorm:"not null;default:now()" json:"recorded_at"`
}

// FileEdit records that someone edited a file in a sandbox (optional audit trail).
type FileEdit struct {
	ID             string    `gorm:"type:uuid;primaryKey"`
	SandboxID      string    `gorm:"type:uuid;not null"`
	FilePath       string    `gorm:"not null"`
	EditedBy       string    `gorm:"type:uuid;not null"`
	EditedAt       time.Time `gorm:"not null;default:now()"`
	PushedToGithub bool      `gorm:"not null;default:false"`
}