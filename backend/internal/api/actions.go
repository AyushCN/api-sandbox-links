package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
)

// extendLifetimeRequest is the body of POST /{id}/actions/extend.
type extendLifetimeRequest struct {
	AdditionalSeconds int64 `json:"additional_seconds"`
}

// handleListDeployments returns the sandbox's deploy/build history.
func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	rows, err := s.store.ListDeployments(sb.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list deployments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": rows})
}

// handleExtendLifetime pushes expires_at forward by the requested amount but
// never past the hard cap (MaxLifetimeSeconds from original creation), and
// recomputes lifetime_seconds so proportional warnings stay meaningful.
func (s *Server) handleExtendLifetime(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "this sandbox is already destroyed and cannot be extended")
		return
	}
	var req extendLifetimeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "additional_seconds is required")
		return
	}
	if req.AdditionalSeconds <= 0 {
		writeErr(w, http.StatusBadRequest, "additional_seconds must be positive")
		return
	}

	// The cap is "lifetime + extensions", measured from the same instant the
	// lifetime is. Since that instant is now activation rather than creation, a
	// slow build no longer eats into the user's extension allowance.
	clockStart := sb.CreatedAt
	if sb.ActivatedAt != nil {
		clockStart = *sb.ActivatedAt
	}
	softCap := clockStart.Add(time.Duration(s.maxLifetime()) * time.Second)
	newExpiry := sb.ExpiresAt.Add(time.Duration(req.AdditionalSeconds) * time.Second)
	if newExpiry.After(softCap) {
		newExpiry = softCap
	}
	if !newExpiry.After(sb.ExpiresAt) {
		writeJSON(w, http.StatusOK, map[string]any{
			"sandbox":     s.sandboxJSON(sb),
			"extended_by": int64(0),
			"at_cap":      true,
		})
		return
	}

	if err := s.store.UpdateSandbox(sb.ID, map[string]any{
		"expires_at":       newExpiry,
		"lifetime_seconds": int64(newExpiry.Sub(sb.CreatedAt).Seconds()),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not extend lifetime")
		return
	}
	sb.ExpiresAt = newExpiry
	sb.LifetimeSeconds = int64(newExpiry.Sub(sb.CreatedAt).Seconds())

	s.logger.Info("lifetime extended", slog.String("sandbox", sb.ID),
		slog.Time("expires_at", newExpiry))
	writeJSON(w, http.StatusOK, map[string]any{
		"sandbox":     s.sandboxJSON(sb),
		"extended_by": req.AdditionalSeconds,
		"at_cap":      newExpiry.Equal(softCap),
	})
}

// handleDestroySandbox is the manual, irreversible teardown. It reuses the
// same teardown code as the expiry reaper but records the reason as
// user_requested. Kept distinct from the automatic path in the UI.
//
// Idempotency: Repeated destroy calls on an already destroyed sandbox return
// 200 OK with the tombstone rather than a 409 error. In-flight builds/deploys
// are cancelled immediately.
func (s *Server) handleDestroySandbox(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	// Idempotency: If already destroyed, return success with current tombstone
	if sb.DestroyedAt != nil || sb.Status == db.StatusDeleted {
		writeJSON(w, http.StatusOK, s.sandboxJSON(sb))
		return
	}
	// Abort any active build or deployment in flight
	if s.deployer != nil {
		s.deployer.CancelDeploy(sb.ID)
	}
	if err := s.destroyPermentally(r.Context(), sb, db.DestroyUserRequested); err != nil {
		s.logger.Warn("manual destroy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "could not destroy sandbox")
		return
	}
	// destroyPermentally updates the row, not the in-memory struct. Re-read it
	// so the response cannot claim a tombstone is still running with a container
	// that was just removed.
	destroyed, err := s.store.GetSandbox(sb.ID)
	if err != nil {
		s.logger.Warn("manual destroy reload failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "sandbox destroyed but could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxJSON(destroyed))
}

// handleRedeploy triggers a manual rebuild (used after edits are saved and by
// the "redeploy" button). Runs in the background.
func (s *Server) handleRedeploy(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "destroyed sandboxes cannot be redeployed; recreate instead")
		return
	}
	opts := orchestrator.DeployOptions{Trigger: db.TriggerManual}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
		defer cancel()
		if err := s.deployer.DeploySandbox(ctx, sb, opts); err != nil {
			s.logger.Warn("manual redeploy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"redeploying": true})
}

func (s *Server) handleUpdateBranch(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	var req struct {
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Branch) == "" {
		writeErr(w, http.StatusBadRequest, "branch is required")
		return
	}
	targetBranch := strings.TrimSpace(req.Branch)
	if err := s.store.UpdateSandbox(sb.ID, map[string]any{"branch_name": targetBranch}); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update branch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"branch":  targetBranch,
	})
}

// handleSandboxWarnings returns the lifetime-warning state for a sandbox so
// the frontend can render banners on list and detail views (section 2a req 3).
func (s *Server) handleSandboxWarnings(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	thresholds, err := s.store.ListWarningThresholds(sb.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not read warnings")
		return
	}
	remaining := time.Until(sb.ExpiresAt)

	warnings := make([]map[string]any, 0, len(thresholds))
	for _, t := range thresholds {
		warnings = append(warnings, map[string]any{
			"threshold": t,
			"title":     warningTitle(t),
			"message":   warningMessage(t, remaining),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sandbox_id":        sb.ID,
		"expires_at":        sb.ExpiresAt,
		"remaining_seconds": int64(remaining / time.Second),
		"warnings":          warnings,
	})
}

// statsRange is one selectable window in the metrics history view. The row
// limit is part of the definition, not a global constant: a 7d window at the
// 10s flush cadence is ~60k rows, and the chart cannot draw that many points
// anyway, so each window is capped at a density the chart stays readable at.
type statsRange struct {
	window time.Duration
	limit  int
}

// statsRanges is the allowlist for GET /api/sandboxes/{id}/stats/history?range=.
// Arbitrary durations are rejected rather than parsed: an unvalidated window
// becomes an unbounded scan of a table that only ever grows, which is a cheap
// way for one request to hurt every other sandbox.
var statsRanges = map[string]statsRange{
	"1h":  {window: time.Hour, limit: 360},
	"6h":  {window: 6 * time.Hour, limit: 720},
	"24h": {window: 24 * time.Hour, limit: 1000},
	"7d":  {window: 7 * 24 * time.Hour, limit: 1400},
}

// defaultStatsRange is what an absent ?range= resolves to, matching the
// dashboard's initial "last hour" selection.
const defaultStatsRange = "1h"

// handleStatsHistory serves persisted resource snapshots for the metrics
// chart's non-live ranges. Unlike the WebSocket feed it is a plain REST call:
// history is a fixed set of rows, so there is nothing to stream.
func (s *Server) handleStatsHistory(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = defaultStatsRange
	}
	span, ok := statsRanges[name]
	if !ok {
		writeErr(w, http.StatusBadRequest, "range must be one of 1h, 6h, 24h, 7d")
		return
	}

	now := time.Now()
	rows, err := s.store.QueryResourceSnapshots(sb.ID, now.Add(-span.window), now, span.limit)
	if err != nil {
		s.logger.Warn("stats history query failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "could not query resource history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":     name,
		"since":     now.Add(-span.window),
		"until":     now,
		"snapshots": rows,
	})
}

// removeSandboxWorkDir deletes the working directory for a subdomain after
// validating the path stays within the configured work root. The validation
// itself lives in orchestrator.RemoveSandboxWorkDir so every deleter (this
// handler, the expiry reaper, deploy-race cleanup) shares one implementation.
func removeSandboxWorkDir(workRoot, subdomain string) error {
	return orchestrator.RemoveSandboxWorkDir(workRoot, subdomain)
}

// destroyPermentally tears a sandbox down and records its tombstone. The typo
// in the name is intentional documentation: this is permanent and unforgettable.
func (s *Server) destroyPermentally(ctx context.Context, sb *db.Sandbox, reason db.DestructionReason) error {
	// Cancel any active build or deployment in flight
	if s.deployer != nil {
		s.deployer.CancelDeploy(sb.ID)
	}
	if err := s.orch.TeardownSandbox(ctx, sb); err != nil {
		return fmt.Errorf("teardown: %w", err)
	}
	// Remove the on-disk working copy too, mirroring expiry cleanup. Validate
	// the path stays within the configured sandbox work root to avoid deleting
	// the root or another sandbox's files.
	if sb.Subdomain != "" {
		if err := removeSandboxWorkDir(s.cfg.SandboxWorkDir, sb.Subdomain); err != nil {
			s.logger.Warn("manual destroy: removing work dir failed",
				slog.String("sandbox", sb.ID),
				slog.String("subdomain", sb.Subdomain),
				slog.String("err", err.Error()))
			return fmt.Errorf("teardown: removing work dir: %w", err)
		}
	}
	now := time.Now()
	if err := s.store.UpdateSandbox(sb.ID, map[string]any{
		"status":             string(db.StatusDeleted),
		"destroyed_at":       now,
		"destruction_reason": string(reason),
		"container_id":       "",
		"image_tag":          "",
	}); err != nil {
		return err
	}
	// Tombstone is committed: sweep once more for containers a racing deploy
	// created between the teardown pass above and this commit. Best-effort —
	// a sweep failure is logged because the destroy itself already succeeded,
	// and any container created after the commit makes its own deploy fail on
	// ErrSandboxDestroyed and tear down (see SweepSandboxContainers).
	if err := s.orch.SweepSandboxContainers(ctx, sb.ID); err != nil {
		s.logger.Warn("destroy: post-tombstone container sweep failed",
			slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
	}
	return nil
}

// SandboxLogsResponse conveys state-aware logs and lifecycle descriptions.
type SandboxLogsResponse struct {
	Status    string   `json:"status"`
	Available bool     `json:"available"`
	Message   string   `json:"message"`
	Output    string   `json:"output,omitempty"`
	Lines     []string `json:"lines,omitempty"`
}

// handleGetSandboxLogs provides a state-aware REST endpoint for container output.
func (s *Server) handleGetSandboxLogs(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}

	if sb.DestroyedAt != nil || sb.Status == db.StatusDeleted {
		writeJSON(w, http.StatusOK, SandboxLogsResponse{
			Status:    "DESTROYED",
			Available: false,
			Message:   "This sandbox has been destroyed and torn down.",
		})
		return
	}

	if sb.Status == db.StatusFailed {
		writeJSON(w, http.StatusOK, SandboxLogsResponse{
			Status:    "FAILED",
			Available: false,
			Message:   "Container failed during image build and is not running. Please inspect the Build Logs panel.",
		})
		return
	}

	if sb.Status == db.StatusBuilding || sb.Status == db.StatusQueued {
		writeJSON(w, http.StatusOK, SandboxLogsResponse{
			Status:    "BUILDING",
			Available: false,
			Message:   "Container image is currently being built. Application output will appear once the container starts.",
		})
		return
	}

	if sb.Status == db.StatusHibernated {
		writeJSON(w, http.StatusOK, SandboxLogsResponse{
			Status:    "STOPPED",
			Available: false,
			Message:   "Container is hibernated due to inactivity. Click Restart to wake it.",
		})
		return
	}

	if sb.Status == db.StatusRunning {
		if sb.ContainerID == "" {
			writeJSON(w, http.StatusOK, SandboxLogsResponse{
				Status:    "STARTING",
				Available: false,
				Message:   "Waiting for application container to start...",
			})
			return
		}

		output, err := s.orch.GetRecentContainerLogs(r.Context(), sb.ContainerID, 150)
		if err != nil {
			writeJSON(w, http.StatusOK, SandboxLogsResponse{
				Status:    "HEALTHCHECK_PENDING",
				Available: false,
				Message:   "Container is starting; waiting for application readiness...",
			})
			return
		}

		rawLines := strings.Split(strings.TrimSpace(output), "\n")
		var cleanLines []string
		for _, l := range rawLines {
			if trimmed := strings.TrimRight(l, "\r"); trimmed != "" {
				cleanLines = append(cleanLines, trimmed)
			}
		}

		writeJSON(w, http.StatusOK, SandboxLogsResponse{
			Status:    "RUNNING",
			Available: true,
			Message:   "Application is running.",
			Output:    output,
			Lines:     cleanLines,
		})
		return
	}

	writeJSON(w, http.StatusOK, SandboxLogsResponse{
		Status:    string(sb.Status),
		Available: false,
		Message:   fmt.Sprintf("Sandbox is in %s state.", sb.Status),
	})
}
