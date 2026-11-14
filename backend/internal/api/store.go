package api

import (
	"context"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
)

// Store defines the database contract required by the API server.
// The production *db.Store satisfies this interface.
type Store interface {
	Ping(ctx context.Context) error
	UpsertUser(u *db.User) error
	GetUserByGithubID(githubID int64) (*db.User, error)
	GetUser(id string) (*db.User, error)
	OwnsSandbox(sandboxID, userID string) (bool, error)
	GetSandbox(id string) (*db.Sandbox, error)
	GetSandboxBySubdomain(subdomain string) (*db.Sandbox, error)
	ListSandboxes(userID string) ([]*db.Sandbox, error)
	FindOrCreateRepository(userID, cloneURL, defaultBranch string) (*db.Repository, error)
	GetRepository(id string) (*db.Repository, error)
	CreateSandboxWithinLimits(sb *db.Sandbox, userID string, maxActive, maxRunning int) error
	UpsertEnvVar(sandboxID, key string, value []byte, aiSuggested bool) error
	RecordFileEdit(sandboxID, relPath, userID string) error
	MarkFileEditsPushed(sandboxID string) error
	ListSandboxDatabases(sandboxID string) ([]*db.SandboxDatabase, error)
	UpdateSandbox(id string, fields map[string]any) error
	ListDeployments(sandboxID string) ([]*db.Deployment, error)
	ListWarningThresholds(sandboxID string) ([]string, error)
	QueryResourceSnapshots(sandboxID string, since, until time.Time, limit int) ([]*db.ResourceSnapshot, error)
}
