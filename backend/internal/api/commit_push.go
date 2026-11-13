package api

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/git"
)

// commitPushRequest is the body of POST /{id}/commit.
//
// The default target branch is ALWAYS a new branch (Phase G requirement): a
// mishap in the editor sandbox must not clobber the user's real work. Pushing
// directly to the original branch requires the caller to explicitly opt in.
type commitPushRequest struct {
	Message          string `json:"message"`
	Branch           string `json:"branch"`             // optional target branch name (new)
	DirectToOriginal bool   `json:"direct_to_original"` // explicit opt-in toggle
	Force            bool   `json:"force"`
}

// handleCommitPush stages the working tree, commits with the given message,
// and pushes to GitHub (a new branch by default).
func (s *Server) handleCommitPush(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	s.touchSandbox(r.Context(), sb)
	if sb.DestroyedAt != nil || sb.Status == db.StatusDeleted || sb.Status == db.StatusExpired {
		writeErr(w, http.StatusConflict, "sandbox is destroyed; nothing to commit")
		return
	}

	var req commitPushRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "message is required")
		return
	}
	message := req.Message
	if message == "" {
		message = "Sandbox edits from API Sandbox Links"
	}

	workDir := filepath.Join(s.cfg.SandboxWorkDir, sb.Subdomain)
	hash, err := git.CommitAll(r.Context(), workDir, message)
	if err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			writeErr(w, http.StatusConflict, "nothing to commit (no changes in the sandbox working directory)")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not commit: "+err.Error())
		return
	}

	targetBranch := req.Branch
	if targetBranch == "" {
		targetBranch = "sandbox-edits-" + sb.Subdomain
	}
	if req.DirectToOriginal {
		targetBranch = sb.BranchName
	}

	token, err := s.decryptUserToken(user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not read GitHub credentials")
		return
	}

	if err := git.Push(r.Context(), workDir, targetBranch, token, req.Force); err != nil {
		s.logger.Warn("push failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
		writeErr(w, http.StatusBadGateway, "push failed: "+err.Error())
		return
	}

	_ = s.store.MarkFileEditsPushed(sb.ID)
	s.logger.Info("commit pushed", slog.String("sandbox", sb.ID),
		slog.String("branch", targetBranch), slog.String("hash", hash))

	writeJSON(w, http.StatusOK, map[string]any{
		"commit":    hash,
		"branch":    targetBranch,
		"direct":    req.DirectToOriginal,
		"pushed_at": time.Now(),
	})
}

// decryptUserToken loads and decrypts the user's stored GitHub token.
func (s *Server) decryptUserToken(userID string) (string, error) {
	u, err := s.store.GetUser(userID)
	if err != nil {
		return "", err
	}
	plain, err := s.box.Decrypt(u.GithubToken)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
