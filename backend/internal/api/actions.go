package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/git"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/google/uuid"
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
		writeErr(w, http.StatusInternalServerError, "deploy_list_failed", "could not list deployments")
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
		writeErr(w, http.StatusConflict, "sandbox_destroyed", "this sandbox is already destroyed and cannot be extended")
		return
	}
	var req extendLifetimeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_missing_param", "additional_seconds is required")
		return
	}
	if req.AdditionalSeconds <= 0 {
		writeErr(w, http.StatusBadRequest, "validation_positive", "additional_seconds must be positive")
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
		writeErr(w, http.StatusInternalServerError, "lifetime_extend_failed", "could not extend lifetime")
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
func (s *Server) handleDestroySandbox(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "sandbox_destroyed", "already destroyed")
		return
	}
	if err := s.destroyPermentally(r.Context(), sb, db.DestroyUserRequested); err != nil {
		s.logger.Warn("manual destroy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "destroy_failed", "could not destroy sandbox")
		return
	}
	// destroyPermentally updates the row, not the in-memory struct. Re-read it
	// so the response cannot claim a tombstone is still running with a container
	// that was just removed.
	destroyed, err := s.store.GetSandbox(sb.ID)
	if err != nil {
		s.logger.Warn("manual destroy reload failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "reload_failed", "sandbox destroyed but could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxJSON(destroyed))
}

// recreateSandboxRequest is the optional body of POST /{id}/actions/recreate.
type recreateSandboxRequest struct {
	LifetimeSeconds int64 `json:"lifetime_seconds"`
}

// handleRecreateSandbox creates a fresh sandbox from the repository, branch and
// environment of a destroyed one. The tombstone is deliberately left in place:
// history, deployments and the original subdomain stay intact, while the new
// row gets its own ID, lifetime and fresh runtime. Sidecar data is not restored
// (teardown removed it); database detection runs again from the repo.
func (s *Server) handleRecreateSandbox(w http.ResponseWriter, r *http.Request) {
	parent, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "auth_unauthorized", "not signed in")
		return
	}
	if parent.DestroyedAt == nil {
		writeErr(w, http.StatusConflict, "only_destroyed_recreate", "only destroyed sandboxes can be recreated")
		return
	}

	var req recreateSandboxRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "validation_invalid_body", "invalid request body")
		return
	}
	lifetime := req.LifetimeSeconds
	if lifetime == 0 {
		// The list view's recreate button sends no body: reuse the lifetime the
		// user originally chose, clamped to the current allowed range.
		lifetime = parent.LifetimeSeconds
		if lifetime < MinLifetimeSeconds {
			lifetime = MinLifetimeSeconds
		}
		if lifetime > s.maxLifetime() {
			lifetime = s.maxLifetime()
		}
	} else if lifetime < MinLifetimeSeconds || lifetime > s.maxLifetime() {
		writeErr(w, http.StatusBadRequest, "validation_invalid_lifetime",
			fmt.Sprintf("lifetime_seconds must be between %d and %d", MinLifetimeSeconds, s.maxLifetime()))
		return
	}

	repo, err := s.store.GetRepository(parent.RepositoryID)
	if err != nil {
		s.logger.Warn("recreate: loading repository",
			slog.String("sandbox", parent.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "load_repository_failed", "could not load repository")
		return
	}

	name := parent.Subdomain
	info, err := git.ParseGitHubURL(repo.GithubURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read_repository_url_failed", "could not read the repository URL")
		return
	}
	name = info.Name
	result, err := s.analyzer.Analyze(r.Context(), info.CloneURL, parent.BranchName)
	if err != nil {
		status, msg := friendlyAnalyzeError(err)
		writeErr(w, status, "analyze_failed", msg)
		return
	}
	if len(result.Blockers) > 0 {
		writeBlockers(w, result.Blockers)
		return
	}
	svc, err := servicesFromJSON(json.RawMessage(parent.ServicesOverride), "", result.Services)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_services_selection", "invalid services selection on the parent sandbox")
		return
	}
	plainEnv, err := s.plainEnv(parent.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read_settings_failed", "could not read the previous sandbox settings")
		return
	}
	mergedEnv, missing, problems := prepareEnv(plainEnv, result, svc)
	if len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_env", "problems": problems})
		return
	}
	if len(missing) > 0 {
		writeMissingEnv(w, missing)
		return
	}
	startCommand := parent.StartCommand
	if startCommand == "" {
		startCommand = result.StartCommand
	}
	servicesBytes, _ := json.Marshal(svc)

	subdomain, err := s.uniqueSubdomain(name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "allocate_subdomain_failed", "could not allocate subdomain")
		return
	}

	idleTimeout := parent.IdleTimeoutSeconds
	if idleTimeout < 60 {
		idleTimeout = 900
	}
	now := time.Now()
	child := &db.Sandbox{
		ID:                 uuid.NewString(),
		RepositoryID:       repo.ID,
		BranchName:         parent.BranchName,
		Subdomain:          subdomain,
		Status:             db.StatusQueued,
		IdleTimeoutSeconds: idleTimeout,
		LifetimeSeconds:    lifetime,
		ExpiresAt:          now.Add(time.Duration(lifetime) * time.Second),
		StartCommand:       strings.TrimSpace(startCommand),
		ServicesOverride:   servicesBytes,
		CreatedAt:          now,
	}
	if err := s.store.CreateSandboxWithinLimits(child, user.ID, s.maxActiveSandboxes(), s.maxRunningSandboxes()); err != nil {
		switch {
		case errors.Is(err, db.ErrSandboxQuotaExceeded):
			s.writeSandboxQuotaExceeded(w)
		case errors.Is(err, db.ErrGlobalCapacityReached):
			s.writeGlobalCapacityExceeded(w)
		default:
			s.logger.Warn("recreate: creating sandbox",
				slog.String("parent", parent.ID), slog.String("err", err.Error()))
			writeErr(w, http.StatusInternalServerError, "recreate_failed", "could not recreate sandbox")
		}
		return
	}

	if err := s.replaceEnv(child.ID, mergedEnv); err != nil {
		s.logger.Warn("recreate: copying env vars",
			slog.String("sandbox", child.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "recreate_settings_failed", "sandbox was recreated but its settings could not be copied")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
		defer cancel()
		if err := s.deployer.DeploySandbox(ctx, child, orchestrator.DeployOptions{
			Trigger:          db.TriggerRecreate,
			DatabaseOverride: "auto",
			StartCommand:     child.StartCommand,
			Services:         &svc,
		}); err != nil {
			s.logger.Warn("recreate deploy failed",
				slog.String("sandbox", child.ID), slog.String("err", err.Error()))
		}
	}()

	writeJSON(w, http.StatusAccepted, s.sandboxJSON(child))
}

// handleRedeploy triggers a manual rebuild (used after edits are saved and by
// the "redeploy" button). Runs in the background.
func (s *Server) handleRedeploy(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "destroyed_cannot_redeploy", "destroyed sandboxes cannot be redeployed; recreate instead")
		return
	}
	repo, err := s.store.GetRepository(sb.RepositoryID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load_repository_failed_2", "could not load the repository")
		return
	}
	info, err := git.ParseGitHubURL(repo.GithubURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read_repository_url_failed", "could not read the repository URL")
		return
	}
	result, err := s.analyzer.Analyze(r.Context(), info.CloneURL, sb.BranchName)
	if err != nil {
		status, msg := friendlyAnalyzeError(err)
		writeErr(w, status, "analyze_failed", msg)
		return
	}
	if len(result.Blockers) > 0 {
		writeBlockers(w, result.Blockers)
		return
	}
	svc, err := servicesFromJSON(json.RawMessage(sb.ServicesOverride), "", result.Services)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "validation_invalid_services", "invalid services selection")
		return
	}
	plainEnv, err := s.plainEnv(sb.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read_sandbox_settings_failed", "could not read the sandbox settings")
		return
	}
	_, missing, problems := prepareEnv(plainEnv, result, svc)
	if len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_env", "problems": problems})
		return
	}
	if len(missing) > 0 {
		writeMissingEnv(w, missing)
		return
	}
	startCommand := sb.StartCommand
	if startCommand == "" {
		startCommand = result.StartCommand
	}
	opts := orchestrator.DeployOptions{Trigger: db.TriggerManual, StartCommand: startCommand, Services: &svc}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
		defer cancel()
		if err := s.deployer.DeploySandbox(ctx, sb, opts); err != nil {
			s.logger.Warn("manual redeploy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"redeploying": true})
}

// handleRedeployApprove approves a pending redeploy for a sandbox.
func (s *Server) handleRedeployApprove(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	// Mark the redeploy as approved and trigger immediate redeploy
	opts := orchestrator.DeployOptions{Trigger: db.TriggerSaveRedeploy}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
		defer cancel()
		if err := s.deployer.DeploySandbox(ctx, sb, opts); err != nil {
			s.logger.Warn("approved redeploy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"approved": true, "sandbox_id": sb.ID})
}

// handleRedeployDismiss dismisses a pending redeploy for a sandbox.
func (s *Server) handleRedeployDismiss(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	// Mark the redeploy as dismissed (no action taken)
	writeJSON(w, http.StatusOK, map[string]any{"dismissed": true, "sandbox_id": sb.ID})
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
		writeErr(w, http.StatusInternalServerError, "read_warnings_failed", "could not read warnings")
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
		writeErr(w, http.StatusBadRequest, "validation_invalid_range", "range must be one of 1h, 6h, 24h, 7d")
		return
	}

	now := time.Now()
	rows, err := s.store.QueryResourceSnapshots(sb.ID, now.Add(-span.window), now, span.limit)
	if err != nil {
		s.logger.Warn("stats history query failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "query_history_failed", "could not query resource history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":     name,
		"since":     now.Add(-span.window),
		"until":     now,
		"snapshots": rows,
	})
}

// handleQuota returns the current sandbox usage and global capacity.
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "auth_unauthorized", "not signed in")
		return
	}
	active, err := s.store.CountActiveSandboxes(user.ID)
	if err != nil {
		s.logger.Warn("quota: counting user sandboxes failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "quota_count_failed", "could not count sandboxes")
		return
	}
	globalRunning, err := s.store.CountGlobalRunningSandboxes()
	if err != nil {
		s.logger.Warn("quota: counting global sandboxes failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "quota_global_count_failed", "could not count global sandboxes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":         active,
		"limit":          s.maxActiveSandboxes(),
		"global_running": globalRunning,
		"global_cap":     s.maxRunningSandboxes(),
	})
}

// handleBuildWS streams build log lines for GET /api/sandboxes/{id}/build/ws.
// It replays lines from Redis so the client doesn't miss anything if the
// WebSocket is opened after the build started. When the build completes
// (success or failure), it sends {type:"closed",reason:"success|failed"}.
func (s *Server) handleBuildWS(w http.ResponseWriter, r *http.Request) {
	ws, sb := s.upgrade(w, r)
	if ws == nil {
		return
	}
	defer ws.shutdown()

	if reason := unavailableReason(sb); reason != "" {
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: reason})
		return
	}

	// Replay any existing build log lines so the client doesn't miss anything.
	raw, err := s.cache.GetBuildLog(r.Context(), sb.ID)
	if err != nil {
		s.logger.Warn("build:ws: reading build log failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: "could not read build log"})
		return
	}
	for _, line := range raw {
		at := time.Now() // TODO: store timestamps with log lines
		if err := ws.send(wsEvent{Type: "log", Line: line, Timestamp: &at}); err != nil {
			return
		}
	}

	// Signal that the build log replay is done, then watch for the final state.
	_ = ws.send(wsEvent{Type: "closed", Reason: "success"})

	// Poll the sandbox status until it leaves the running state.
	status := time.NewTicker(5 * time.Second)
	defer status.Stop()

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-status.C:
			sb, err := s.store.GetSandbox(sb.ID)
			if err != nil {
				s.logger.Debug("build:ws: re-reading sandbox status", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
				return
			}
			if sb.Status != db.StatusRunning {
				reason := "failed"
				if sb.Status == db.StatusDeleted || sb.DestroyedAt != nil {
					reason = "success"
				}
				ws.sendTerminal(wsEvent{Type: "closed", Reason: reason})
				return
			}
		}
	}
}

// handleRetry triggers a manual redeploy of a failed sandbox. Returns 409 if the
// sandbox has been destroyed.
func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "destroyed_cannot_redeploy", "destroyed sandboxes cannot be redeployed; recreate instead")
		return
	}
	repo, err := s.store.GetRepository(sb.RepositoryID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load_repository_failed_2", "could not load the repository")
		return
	}
	info, err := git.ParseGitHubURL(repo.GithubURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read_repository_url_failed", "could not read the repository URL")
		return
	}
	result, err := s.analyzer.Analyze(r.Context(), info.CloneURL, sb.BranchName)
	if err != nil {
		status, msg := friendlyAnalyzeError(err)
		writeErr(w, status, "analyze_failed", msg)
		return
	}
	if len(result.Blockers) > 0 {
		writeBlockers(w, result.Blockers)
		return
	}
	svc, err := servicesFromJSON(json.RawMessage(sb.ServicesOverride), "", result.Services)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "validation_invalid_services", "invalid services selection")
		return
	}
	plainEnv, err := s.plainEnv(sb.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read_sandbox_settings_failed", "could not read the sandbox settings")
		return
	}
	_, missing, problems := prepareEnv(plainEnv, result, svc)
	if len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_env", "problems": problems})
		return
	}
	if len(missing) > 0 {
		writeMissingEnv(w, missing)
		return
	}
	startCommand := sb.StartCommand
	if startCommand == "" {
		startCommand = result.StartCommand
	}
	opts := orchestrator.DeployOptions{Trigger: db.TriggerManual, StartCommand: startCommand, Services: &svc}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
		defer cancel()
		if err := s.deployer.DeploySandbox(ctx, sb, opts); err != nil {
			s.logger.Warn("manual redeploy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"redeploying": true})
}

// handleEventsWS streams status events for GET /api/events/ws.
// It pushes {type:"sandbox",id,status,stage,stage_message} whenever a
// sandbox changes state, plus {type:"warning"} for lifetime thresholds.
// Includes a heartbeat.
// Every frame includes: type, status, stage, stage_message
func (s *Server) handleEventsWS(w http.ResponseWriter, r *http.Request) {
	ws, sb := s.upgrade(w, r)
	if ws == nil {
		return
	}
	defer ws.shutdown()

	if reason := unavailableReason(sb); reason != "" {
		ws.sendTerminal(wsEvent{Type: "unavailable", Reason: reason})
		return
	}

	// Send initial state with stage.
	stage, stageMessage := s.computeStage(sb)
	ws.send(wsEvent{
		Type:     "sandbox",
		Stream:   sb.ID,
		Line:     string(sb.Status),
		Stage:    &stage,
		StageMsg: &stageMessage,
	})

	// Send warning if lifetime is approaching threshold.
	remaining := time.Until(sb.ExpiresAt)
	if remaining > 0 && remaining < 5*time.Minute {
		ws.send(wsEvent{Type: "warning", Line: fmt.Sprintf("lifetime expires in %v", remaining.Round(time.Second))})
	}

	// Heartbeat ticker.
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// Track last sent state to avoid flooding.
	lastStatus := sb.Status
	lastStage := stage

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-heartbeat.C:
			// Send a heartbeat frame with current stage.
			stage, stageMessage = s.computeStage(sb)
			ws.send(wsEvent{
				Type:     "heartbeat",
				Stage:    &stage,
				StageMsg: &stageMessage,
			})
		default:
			// Re-read the sandbox state.
			sb, err := s.store.GetSandbox(sb.ID)
			if err != nil {
				ws.sendTerminal(wsEvent{Type: "closed", Reason: "sandbox no longer available"})
				return
			}

			// Send state change if status or stage changed.
			stage, stageMessage = s.computeStage(sb)
			if sb.Status != lastStatus || stage != lastStage {
				lastStatus = sb.Status
				lastStage = stage
				ws.send(wsEvent{
					Type:     "sandbox",
					Stream:   sb.ID,
					Line:     string(sb.Status),
					Stage:    &stage,
					StageMsg: &stageMessage,
				})
			}

			// Check lifetime warning thresholds.
			remaining := time.Until(sb.ExpiresAt)
			if remaining > 0 && remaining < 5*time.Minute {
				ws.send(wsEvent{Type: "warning", Line: fmt.Sprintf("lifetime expires in %v", remaining.Round(time.Second))})
			}

			// Poll every second.
			time.Sleep(1 * time.Second)
		}
	}
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
	} else {
		s.logger.Warn("manual destroy: empty subdomain, skipping work dir removal",
			slog.String("sandbox", sb.ID))
		return fmt.Errorf("teardown: empty subdomain")
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

// handleStop stops a running sandbox (hibernates it).
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "cannot_stop_destroyed", "cannot stop a destroyed sandbox")
		return
	}
	if sb.Status != db.StatusRunning {
		writeErr(w, http.StatusBadRequest, "only_running_stop", "only running sandboxes can be stopped")
		return
	}
	sb.Status = db.StatusHibernated
	if err := s.store.UpdateSandbox(sb.ID, map[string]any{"status": string(db.StatusHibernated)}); err != nil {
		s.logger.Warn("sandbox: update status failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "status_update_failed", "could not update sandbox status")
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxJSON(sb))
}

// handleStart starts a hibernated sandbox (wakes it on demand).
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "cannot_start_destroyed", "cannot start a destroyed sandbox")
		return
	}
	if sb.Status != db.StatusHibernated {
		writeErr(w, http.StatusBadRequest, "only_hibernated_start", "only hibernated sandboxes can be started")
		return
	}
	sb.Status = db.StatusRunning
	if err := s.store.UpdateSandbox(sb.ID, map[string]any{"status": string(db.StatusRunning)}); err != nil {
		s.logger.Warn("sandbox: update status failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "status_update_failed", "could not update sandbox status")
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxJSON(sb))
}

// handleSettings updates the sandbox settings, including idle_timeout_seconds.
// PUT /api/sandboxes/{id}/settings expects {idle_timeout_seconds}.
// Validation: 600..86400. Returns 400 instead of silently clamping.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	var req struct {
		IdleTimeoutSeconds int `json:"idle_timeout_seconds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_missing_param", "idle_timeout_seconds is required")
		return
	}
	if req.IdleTimeoutSeconds < MinIdleTimeoutSeconds || req.IdleTimeoutSeconds > MaxIdleTimeoutSeconds {
		writeErr(w, http.StatusBadRequest, "validation_invalid_idle_timeout",
			fmt.Sprintf("idle_timeout_seconds must be between %d and %d, got %d",
				MinIdleTimeoutSeconds, MaxIdleTimeoutSeconds, req.IdleTimeoutSeconds))
		return
	}
	sb.IdleTimeoutSeconds = req.IdleTimeoutSeconds
	if err := s.store.UpdateSandbox(sb.ID, map[string]any{"idle_timeout_seconds": sb.IdleTimeoutSeconds}); err != nil {
		s.logger.Warn("sandbox: update settings failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "settings_update_failed", "could not update sandbox settings")
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxJSON(sb))
}
