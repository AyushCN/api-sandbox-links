package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/api-sandbox-links/backend/internal/analyze"
	"github.com/api-sandbox-links/backend/internal/git"
)

// analyzeRepoRequest is the body of POST /api/repos/analyze.
type analyzeRepoRequest struct {
	RepoURL string `json:"repo_url"`
	Branch  string `json:"branch"`
}

// handleAnalyzeRepo performs the preflight and returns the complete report.
// It is authenticated and limited per user so a caller cannot turn it into an
// unbounded cloner.
func (s *Server) handleAnalyzeRepo(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "auth_unauthorized", "not signed in")
		return
	}
	if wait, ok := s.ratelim.allow(user.ID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "Too many repository checks. Please wait a moment and try again.",
		})
		return
	}

	var req analyzeRepoRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_missing_param", "Send a JSON body with a repo_url.")
		return
	}
	info, err := git.ParseGitHubURL(req.RepoURL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "validation_invalid_repo_url", "That is not a GitHub repository URL. Use https://github.com/owner/repo.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), analyze.CloneTimeout+analyze.PlanTimeout+10*time.Second)
	defer cancel()
	result, err := s.analyzer.Analyze(ctx, info.CloneURL, strings.TrimSpace(req.Branch))
	if err != nil {
		status, msg := friendlyAnalyzeError(err)
		s.logger.Warn("repo analysis failed", slog.String("repo", info.CloneURL), slog.String("err", err.Error()))
		writeErr(w, status, "analyze_failed", msg)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func friendlyAnalyzeError(err error) (int, string) {
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "context deadline exceeded"):
		return http.StatusGatewayTimeout, "Reading this repository took too long. Check the URL and try again."
	case strings.Contains(msg, "authentication required"),
		strings.Contains(msg, "repository not found"),
		strings.Contains(msg, "not found"),
		strings.Contains(msg, "could not read username"),
		strings.Contains(msg, "remote repository is empty"):
		return http.StatusNotFound, "Repository not found. It may be private, or the URL may be wrong. Only public GitHub repositories can be deployed."
	case strings.Contains(msg, "only github.com"):
		return http.StatusBadRequest, "Only GitHub repositories are supported right now."
	case strings.Contains(msg, "larger than"):
		return http.StatusRequestEntityTooLarge, "This repository is too large for the sandbox builder (250 MB limit after a shallow clone)."
	default:
		return http.StatusBadGateway, "We could not read that repository. Check that it is public and try again."
	}
}

// repoRateLimiter is a tiny per-user sliding window. It deliberately lives in
// memory: the analyze endpoint is a convenience, and a restart resetting the
// window is harmless.
type repoRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

func newRepoRateLimiter(limit int, window time.Duration) *repoRateLimiter {
	return &repoRateLimiter{limit: limit, window: window, hits: map[string][]time.Time{}, now: time.Now}
}

func (l *repoRateLimiter) allow(userID string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	list := l.hits[userID]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.hits[userID] = kept
	if len(kept) >= l.limit {
		return kept[0].Add(l.window).Sub(now), false
	}
	l.hits[userID] = append(l.hits[userID], now)
	return 0, true
}
