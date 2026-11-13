package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/git"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/google/uuid"
)

// forkLocks prevents concurrent file mutations while a sandbox is being forked.
var forkLocks sync.Map

// handleForkSandbox creates a child sandbox that is an independent copy of the
// parent: its own sandbox row (parent_sandbox_id set), a deep copy of the
// working directory (so uncommitted in-browser edits travel too), and a
// byte-for-byte copy of each sidecar database volume. Lineage is recorded in
// the DB for the ForkTree UI. The child gets the parent's *remaining* lifetime
// (capped), because a fork that outlives its source material would be
// surprising. // DECISION: remaining-lifetime inheritance.
func (s *Server) handleForkSandbox(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	if parent.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "cannot fork a destroyed sandbox")
		return
	}
	if _, already := forkLocks.LoadOrStore(parent.ID, struct{}{}); already {
		writeErr(w, http.StatusConflict, "fork already in progress for this sandbox")
		return
	}
	defer forkLocks.Delete(parent.ID)

	// Child lifetime = parent's remaining lifetime, clamped to [min, max].
	//
	// The parent is already live, so its remaining time is the meaningful
	// figure. The child is not: like any new sandbox it is created 'queued' and
	// its own clock starts when it first goes live, so the child ends up outliving
	// the parent by however long the copy takes. That is the intended reading of
	// a fork — a fresh link, not an extension of the old one.
	remaining := time.Until(parent.ExpiresAt)
	childLifetime := int64(remaining / time.Second)
	if childLifetime < MinLifetimeSeconds {
		childLifetime = MinLifetimeSeconds
	}
	if childLifetime > s.maxLifetime() {
		childLifetime = s.maxLifetime()
	}

	subdomain, err := s.uniqueSubdomain("fork-" + parent.Subdomain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not allocate subdomain")
		return
	}

	now := time.Now()
	child := &db.Sandbox{
		ID:                 uuid.NewString(),
		RepositoryID:       parent.RepositoryID,
		ParentSandboxID:    &parent.ID,
		BranchName:         parent.BranchName,
		Subdomain:          subdomain,
		Status:             db.StatusQueued,
		IdleTimeoutSeconds: parent.IdleTimeoutSeconds,
		LifetimeSeconds:    childLifetime,
		ExpiresAt:          now.Add(time.Duration(childLifetime) * time.Second),
		CreatedAt:          now,
	}
	if err := s.store.CreateSandboxWithinLimits(child, user.ID, s.maxActiveSandboxes(), s.maxRunningSandboxes()); err != nil {
		if errors.Is(err, db.ErrSandboxQuotaExceeded) {
			s.writeSandboxQuotaExceeded(w)
			return
		}
		if errors.Is(err, db.ErrGlobalCapacityReached) {
			s.writeGlobalCapacityExceeded(w)
			return
		}
		s.logger.Warn("fork: creating child sandbox", slog.String("parent", parent.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "could not create fork")
		return
	}

	go s.forkDeploy(r.Context(), parent, child)

	writeJSON(w, http.StatusAccepted, s.sandboxJSON(child))
}

// forkDeploy prepares the child's working copy and sidecar DBs, then deploys.
func (s *Server) forkDeploy(parentCtx context.Context, parent, child *db.Sandbox) {
	ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
	defer cancel()

	// 1. Deep-copy the parent's working directory so current edits carry over.
	parentDir := filepath.Join(s.cfg.SandboxWorkDir, parent.Subdomain)
	childDir := filepath.Join(s.cfg.SandboxWorkDir, child.Subdomain)
	if info, err := os.Stat(parentDir); err == nil && info.IsDir() {
		if err := copyDir(parentDir, childDir); err != nil {
			s.forkFailed(parentCtx, child, err)
			return
		}
	} else {
		repo, err := s.store.GetRepository(parent.RepositoryID)
		if err == nil {
			if _, err := git.Clone(ctx, repo.GithubURL, child.BranchName, childDir); err != nil {
				s.forkFailed(parentCtx, child, err)
				return
			}
		}
	}

	// 2. Deep-copy each parent sidecar database volume into a fresh child DB.
	dbs, err := s.store.ListSandboxDatabases(parent.ID)
	if err != nil {
		s.forkFailed(parentCtx, child, err)
		return
	}
	for _, pdb := range dbs {
		if _, _, err := s.orch.ForkDatabase(ctx, pdb, child.ID); err != nil {
			s.forkFailed(parentCtx, child, fmt.Errorf("forking database %s: %w", pdb.ID, err))
			return
		}
	}

	// 3. Deploy the child like any other sandbox; the DB container's IP waits
	// are handled inside DeploySandbox (it only needs the DATABASE_URL env).
	if err := s.deployer.DeploySandbox(ctx, child, orchestrator.DeployOptions{Trigger: db.TriggerFork}); err != nil {
		s.forkFailed(parentCtx, child, err)
		return
	}
}

// forkFailed marks the child sandbox failed so the UI shows a clear error, and
// reclaims anything the fork created before the failure (a sidecar copied from
// the parent, its volume, the private network, or a partially built image).
// Only the child is touched: every lookup is keyed by the child's sandbox-id
// label.
func (s *Server) forkFailed(ctx context.Context, child *db.Sandbox, cause error) {
	s.logger.Warn("fork deploy failed", slog.String("sandbox", child.ID), slog.String("err", cause.Error()))
	if s.orch != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := s.orch.TeardownSandbox(cleanupCtx, child); err != nil {
			s.logger.Warn("fork: tearing down failed fork",
				slog.String("sandbox", child.ID), slog.String("err", err.Error()))
		}
		cancel()
	}
	_ = s.store.UpdateSandbox(child.ID, map[string]any{
		"status": string(db.StatusFailed),
	})
	_ = ctx
}
