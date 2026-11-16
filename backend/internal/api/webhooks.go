package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/api-sandbox-links/backend/internal/analyze"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/git"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
)

// githubPushPayload is the subset of GitHub's push payload the redeploy flow
// reads. Everything else (commits, sender, head_commit) is ignored.
type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		CloneURL string `json:"clone_url"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// handleGithubWebhook serves POST /api/webhooks/github. It is public (GitHub
// cannot hold a session) and authenticated by the HMAC signature instead: a
// request whose body does not match X-Hub-Signature-256 is rejected before the
// payload is parsed. A push to a tracked branch redeploys every live sandbox
// for that repository and branch.
func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GitHubWebhookSecret == "" {
		writeErr(w, http.StatusNotFound, "webhook_disabled",
			"GitHub webhooks are not configured (set GITHUB_WEBHOOK_SECRET)")
		return
	}
	// Cap the body: GitHub push payloads are small, and an unbounded read is a
	// free way to exhaust memory on a public endpoint.
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "webhook_read_failed", "could not read the webhook body")
		return
	}
	if !validWebhookSignature(s.cfg.GitHubWebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeErr(w, http.StatusUnauthorized, "webhook_invalid_signature",
			"the webhook signature does not match GITHUB_WEBHOOK_SECRET")
		return
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pong": true})
		return
	case "push":
		// fall through to the redeploy flow
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"ignored": true, "reason": "unsupported event"})
		return
	}

	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "webhook_invalid_payload", "the webhook body is not valid JSON")
		return
	}
	branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
	if branch == "" || branch == payload.Ref || payload.Deleted {
		writeJSON(w, http.StatusAccepted, map[string]any{"ignored": true, "reason": "not a branch push"})
		return
	}
	if payload.Repository.CloneURL == "" {
		writeErr(w, http.StatusBadRequest, "webhook_invalid_payload", "the payload has no repository.clone_url")
		return
	}

	candidates, err := s.store.ListRedeployCandidates(payload.Repository.CloneURL, branch)
	if err != nil {
		s.logger.Warn("webhook: listing redeploy candidates failed",
			slog.String("repo", payload.Repository.FullName), slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "redeploy_lookup_failed", "could not look up sandboxes to redeploy")
		return
	}

	redeployed := make([]string, 0, len(candidates))
	for _, sb := range candidates {
		go s.redeployFromPush(sb, branch)
		redeployed = append(redeployed, sb.ID)
	}
	s.logger.Info("webhook: push received",
		slog.String("repo", payload.Repository.FullName),
		slog.String("branch", branch),
		slog.Int("redeployed", len(redeployed)))

	writeJSON(w, http.StatusAccepted, map[string]any{
		"repository":  payload.Repository.FullName,
		"branch":      branch,
		"redeploying": len(redeployed),
		"sandbox_ids": redeployed,
	})
}

// validWebhookSignature compares the GitHub HMAC digest in constant time. The
// header is "sha256=<hex>"; a missing or malformed header never matches.
func validWebhookSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// redeployFromPush refreshes one sandbox's working copy to origin/<branch> and
// rebuilds it. Runs in its own goroutine: the webhook response must not wait for
// a build, and one sandbox failing must not stop the others.
func (s *Server) redeployFromPush(sb *db.Sandbox, branch string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("webhook: redeploy panicked",
				slog.String("sandbox", sb.ID), slog.Any("panic", rec))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
	defer cancel()

	workDir := filepath.Join(s.cfg.SandboxWorkDir, sb.Subdomain)
	if err := git.SyncToRemote(ctx, workDir, branch); err != nil {
		s.logger.Warn("webhook: syncing working copy failed",
			slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		return
	}

	opts := orchestrator.DeployOptions{
		Trigger:          db.TriggerGithubPush,
		DatabaseOverride: "auto",
		StartCommand:     sb.StartCommand,
	}
	if len(sb.ServicesOverride) > 0 {
		var ov serviceOverride
		if err := json.Unmarshal(sb.ServicesOverride, &ov); err == nil {
			svc := effectiveServices(&ov, "auto", analyze.Services{})
			opts.Services = &svc
		}
	}
	if err := s.deployer.DeploySandbox(ctx, sb, opts); err != nil {
		s.logger.Warn("webhook: redeploy failed",
			slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
	}
}
