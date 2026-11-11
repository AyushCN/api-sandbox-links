package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/git"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Lifetime constants shared with the frontend's picker.
const (
	// MaxLifetimeSeconds caps the planned lifetime (default 7 days, overridable
	// via MAX_LIFETIME_SECONDS).
	MaxLifetimeSeconds = 7 * 24 * 3600
	// MinLifetimeSeconds keeps a sandbox alive long enough to be useful: the
	// lifetime clock starts at first activation, so anything shorter would be
	// spent largely on the build itself. One hour is the floor.
	MinLifetimeSeconds = 60 * 60
)

// createSandboxRequest is the body of POST /api/sandboxes.
type createSandboxRequest struct {
	RepoURL            string            `json:"repo_url"`
	Branch             string            `json:"branch"`
	LifetimeSeconds    int64             `json:"lifetime_seconds"`
	IdleTimeoutSeconds int               `json:"idle_timeout_seconds"`
	Database           string            `json:"database"` // auto | postgres | mysql | none
	EnvVars            map[string]string `json:"env_vars"`
}

// handleListSandboxes returns the signed-in user's sandboxes, newest first.
// Expired sandboxes still appear (as tombstone rows) so the user can recreate.
func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	rows, err := s.store.ListSandboxes(user.ID)
	if err != nil {
		s.logger.Warn("list sandboxes failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "could not list sandboxes")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, sb := range rows {
		out = append(out, s.sandboxJSON(sb))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sandboxes": out})
}

// handleCreateSandbox accepts a repo URL plus lifetime/idle-timeout controls,
// creates the sandbox row, and kicks off the deploy pipeline in the
// background. The response is 202 with the queued sandbox.
func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var req createSandboxRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RepoURL == "" {
		writeErr(w, http.StatusBadRequest, "repo_url is required")
		return
	}
	info, err := git.ParseGitHubURL(req.RepoURL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Lifetime validation — this control permanently destroys data, so the
	// backend enforces the bounds and the frontend must show it prominently.
	lifetime := req.LifetimeSeconds
	if lifetime == 0 {
		lifetime = 24 * 3600 // schema default
	}
	if lifetime < MinLifetimeSeconds || lifetime > s.maxLifetime() {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("lifetime_seconds must be between %d and %d", MinLifetimeSeconds, s.maxLifetime()))
		return
	}
	idleTimeout := req.IdleTimeoutSeconds
	if idleTimeout < 60 {
		idleTimeout = 900 // schema default
	}

	branch := req.Branch
	if branch == "" {
		branch = info.DefaultBranch
	}

	repo, err := s.store.FindOrCreateRepository(user.ID, info.CloneURL, info.DefaultBranch)
	if err != nil {
		s.logger.Warn("repository lookup failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "could not store repository")
		return
	}

	subdomain, err := s.uniqueSubdomain(info.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not allocate subdomain")
		return
	}

	now := time.Now()
	sandbox := &db.Sandbox{
		ID:                 uuid.NewString(),
		RepositoryID:       repo.ID,
		BranchName:         branch,
		Subdomain:          subdomain,
		Status:             db.StatusQueued,
		IdleTimeoutSeconds: idleTimeout,
		LifetimeSeconds:    lifetime,
		ExpiresAt:          now.Add(time.Duration(lifetime) * time.Second),
		CreatedAt:          now,
	}
	if err := s.store.CreateSandboxWithinLimits(sandbox, user.ID, s.maxActiveSandboxes(), s.maxRunningSandboxes()); err != nil {
		if errors.Is(err, db.ErrSandboxQuotaExceeded) {
			s.writeSandboxQuotaExceeded(w)
			return
		}
		if errors.Is(err, db.ErrGlobalCapacityReached) {
			s.writeGlobalCapacityExceeded(w)
			return
		}
		s.logger.Warn("sandbox create failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "could not create sandbox")
		return
	}

	// Persist any user-supplied env vars (encrypted immediately).
	for key, value := range req.EnvVars {
		enc, err := s.box.Encrypt([]byte(value))
		if err != nil {
			continue
		}
		_ = s.store.UpsertEnvVar(sandbox.ID, key, enc, false)
	}

	// Deploy asynchronously; the client polls the sandbox until running.
	opts := orchestrator.DeployOptions{Trigger: db.TriggerInitial, DatabaseOverride: req.Database}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
		defer cancel()
		if err := s.deployer.DeploySandbox(ctx, sandbox, opts); err != nil {
			s.logger.Warn("deploy failed", slog.String("sandbox", sandbox.ID), slog.String("err", err.Error()))
		}
	}()

	writeJSON(w, http.StatusAccepted, s.sandboxJSON(sandbox))
}

// handleGetSandbox returns one sandbox's current state.
func (s *Server) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxJSON(sb))
}

// --- helpers -------------------------------------------------------------

// errSandboxForbidden marks a failed sandbox access check. It is deliberately
// uninformative: "not yours" and "does not exist" are the same answer, so the
// API never confirms that someone else's sandbox ID is real.
var errSandboxForbidden = errors.New("sandbox not accessible")

// authorizeSandboxAccess resolves the {id} path param and confirms the
// signed-in user owns it. Every per-sandbox route funnels through here.
//
// It deliberately does not touch the ResponseWriter. The live WebSocket
// endpoints need to fail *after* the handshake with a close code, by which
// point an HTTP status can no longer be sent; requireOwnedSandbox is the
// writing wrapper used by the REST handlers.
func (s *Server) authorizeSandboxAccess(r *http.Request) (*db.Sandbox, error) {
	user, ok := s.userFrom(r)
	if !ok {
		return nil, errSandboxForbidden
	}
	id := chi.URLParam(r, "id")
	owns, err := s.store.OwnsSandbox(id, user.ID)
	if err != nil {
		s.logger.Warn("sandbox access check failed",
			slog.String("sandbox", id), slog.String("err", err.Error()))
		return nil, errSandboxForbidden
	}
	if !owns {
		return nil, errSandboxForbidden
	}
	sb, err := s.store.GetSandbox(id)
	if err != nil {
		return nil, errSandboxForbidden
	}
	return sb, nil
}

// requireOwnedSandbox is the REST-shaped wrapper around
// authorizeSandboxAccess: it writes the error response and reports ok=false.
func (s *Server) requireOwnedSandbox(w http.ResponseWriter, r *http.Request) (*db.Sandbox, bool) {
	if _, ok := s.userFrom(r); !ok {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return nil, false
	}
	sb, err := s.authorizeSandboxAccess(r)
	if err != nil {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return nil, false
	}
	return sb, true
}

// sandboxJSON renders a sandbox row plus computed fields the UI needs.
func (s *Server) sandboxJSON(sb *db.Sandbox) map[string]any {
	return map[string]any{
		"id":                   sb.ID,
		"subdomain":            sb.Subdomain,
		"url":                  "http://" + sb.Subdomain + "." + s.cfg.SandboxBaseDomain,
		"status":               sb.Status,
		"branch":               sb.BranchName,
		"parent_sandbox_id":    sb.ParentSandboxID,
		"container_id":         sb.ContainerID,
		"internal_port":        sb.InternalPort,
		"idle_timeout_seconds": sb.IdleTimeoutSeconds,
		"last_request_at":      sb.LastRequestAt,
		"lifetime_seconds":     sb.LifetimeSeconds,
		"expires_at":           sb.ExpiresAt,
		// Null until the sandbox first goes live; the client uses it to show that
		// the lifetime clock has not started yet.
		"activated_at":       sb.ActivatedAt,
		"destroyed_at":       sb.DestroyedAt,
		"destruction_reason": sb.DestructionReason,
		"detected_language":  sb.DetectedLanguage,
		"build_config":       sb.BuildConfig,
		"created_at":         sb.CreatedAt,
		"updated_at":         sb.UpdatedAt,
	}
}

// uniqueSubdomain builds a routable subdomain: slugified repo name plus a
// short random suffix, retried against collisions.
func (s *Server) uniqueSubdomain(repoName string) (string, error) {
	base := slugify(repoName)
	if base == "" {
		base = "sandbox"
	}
	for i := 0; i < 5; i++ {
		suffix := randomHex(3) // 6 hex chars
		candidate := base + "-" + suffix
		if _, err := s.store.GetSandboxBySubdomain(candidate); err != nil {
			return candidate, nil // no collision found
		}
	}
	return "", fmt.Errorf("could not find a free subdomain")
}

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// slugify lowercases and collapses the repo name to a DNS-safe label.
func slugify(name string) string {
	name = strings.ToLower(name)
	name = nonSlug.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 40 {
		name = name[:40]
	}
	return strings.TrimSuffix(name, "-")
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	return hex.EncodeToString(b)
}

func (s *Server) maxLifetime() int64 {
	if m := s.cfg.MaxLifetimeSeconds; m > 0 {
		return m
	}
	return MaxLifetimeSeconds
}

// decodeJSON reads and closes the request body into v.
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// warningTitle returns the short heading for a sent warning tier.
func warningTitle(threshold string) string {
	switch threshold {
	case "24h":
		return "Less than 24 hours left"
	case "1h":
		return "Less than 1 hour left"
	case "5m":
		return "Less than 5 minutes left"
	case "p50":
		return "Half the lifetime is gone"
	case "p25":
		return "Three quarters of the lifetime is gone"
	default:
		return "Lifetime warning"
	}
}

// warningMessage renders the plain-language warning copy (section 2a reuse
// of the idle-vs-lifetime distinction where useful).
func warningMessage(threshold string, remaining time.Duration) string {
	h := int(remaining.Hours())
	m := int(remaining.Minutes()) % 60
	switch threshold {
	case "p50", "p25":
		return "This sandbox will be permanently deleted soon. Everything is destroyed at expiry. Extend the lifetime if you still need it."
	default:
		if h > 0 {
			return fmt.Sprintf("This sandbox will be permanently deleted in about %dh %dm. Data is lost at that point, for good. Extend if you still need it.", h, m)
		}
		return fmt.Sprintf("This sandbox will be permanently deleted in about %dm. There is no undo — extend it now if you still need it.", m)
	}
}
