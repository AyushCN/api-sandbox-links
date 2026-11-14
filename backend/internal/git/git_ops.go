// Package git wraps go-git for the three operations the platform needs:
// cloning a repo, committing local edits, and pushing them back to GitHub.
// Per the locked architecture every git operation goes through go-git's pure
// Go library — the backend never shells out to the git CLI binary.
package git

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// RepoInfo is the parsed, canonical form of a GitHub repo reference.
type RepoInfo struct {
	// Owner is the GitHub username or org.
	Owner string
	// Name is the repository name (without .git).
	Name string
	// CloneURL is the canonical https clone URL.
	CloneURL string
	// DefaultBranch is the repo's default branch (best-effort, falls back to
	// "main" when the GitHub API isn't reachable for the probe).
	DefaultBranch string
}

// ParseGitHubURL normalizes a pasted GitHub URL into a RepoInfo with a
// canonical https clone URL. It accepts https://, http://, git@ (ssh) and
// owner/repo shorthand forms.
func ParseGitHubURL(raw string) (*RepoInfo, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")

	switch {
	case strings.HasPrefix(raw, "git@"):
		// git@github.com:owner/name.git
		rest := strings.TrimPrefix(raw, "git@")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("git: unparseable ssh url %q", raw)
		}
		parts[1] = strings.TrimSuffix(parts[1], ".git")
		return build(parts[0], parts[1])
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("git: parsing url %q: %w", raw, err)
		}
		host, path := u.Host, strings.TrimPrefix(u.Path, "/")
		if path == "" || !strings.Contains(path, "/") {
			return nil, fmt.Errorf("git: url %q has no owner/name path", raw)
		}
		return build(host, path)
	case strings.Count(raw, "/") == 1 && !strings.ContainsAny(raw, "@: "):
		// Owner/repo shorthand.
		return build("github.com", raw)
	default:
		return nil, fmt.Errorf("git: unrecognized repo url %q (use an https://github.com/... URL)", raw)
	}
}

func build(host, ownerName string) (*RepoInfo, error) {
	// Only GitHub is supported for now; the error message teaches the constraint.
	if host != "github.com" {
		return nil, fmt.Errorf("git: only github.com repos are supported, got %q", host)
	}
	parts := strings.Split(ownerName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("git: expected owner/name, got %q", ownerName)
	}
	info := &RepoInfo{
		Owner:         parts[0],
		Name:          parts[1],
		CloneURL:      "https://github.com/" + parts[0] + "/" + parts[1] + ".git",
		DefaultBranch: "main",
	}
	return info, nil
}

// Clone performs a shallow, single-branch clone into directory dir. It returns
// the resolved branch that was checked out (useful for the default-branch
// fallback path in calls that did a HEAD clone).
func Clone(ctx context.Context, cloneURL, branch, dir string) (string, error) {
	// An empty branch means "let the remote choose": go-git then checks out the
	// repository's default branch, which is how /api/repos/analyze resolves the
	// branch when the caller did not supply one.
	opts := &git.CloneOptions{
		URL:          cloneURL,
		SingleBranch: true,
		Depth:        1,
		Auth:         nil,
		Progress:     nil,
	}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
	}
	repo, err := git.PlainCloneContext(ctx, dir, false, opts)
	if err != nil {
		return "", fmt.Errorf("git: cloning %s (branch %s): %w", cloneURL, branch, err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("git: reading clone head: %w", err)
	}
	return head.Name().Short(), nil
}

// ErrNoChanges is returned by CommitAll when the working tree has nothing to
// stage — callers translate it into a friendly 409 response.
var ErrNoChanges = fmt.Errorf("git: no changes to commit")

// CommitAll stages every change in the working tree and commits with the given
// message. It returns the commit hash.
func CommitAll(ctx context.Context, dir, message string) (string, error) {
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("git: opening repo at %s: %w", dir, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("git: opening worktree: %w", err)
	}
	// pathspec "." stages everything, including deletions.
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", fmt.Errorf("git: staging changes: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return "", fmt.Errorf("git: reading status: %w", err)
	}
	if status.IsClean() {
		return "", ErrNoChanges
	}
	hash, err := wt.Commit(message, &git.CommitOptions{})
	if err != nil {
		return "", fmt.Errorf("git: committing: %w", err)
	}
	return hash.String(), nil
}

// Push pushes the current branch to GitHub using the user's OAuth token. A
// token with repo (or `contents:write`) scope is required. force is used when
// pushing to the original branch from a fork-style sandbox that clobbered it.
func Push(ctx context.Context, dir, remoteBranch, token string, force bool) error {
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return fmt.Errorf("git: opening repo at %s: %w", dir, err)
	}
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("git: reading head: %w", err)
	}
	auth := &http.BasicAuth{
		// GitHub accepts the token as the password and any username; "x-token"
		// is a conventional placeholder.
		Username: "x-access-token",
		Password: token,
	}
	pushOpts := &git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec(head.Name().String() + ":" + plumbing.NewBranchReferenceName(remoteBranch).String()),
		},
		Auth:  auth,
		Force: force,
	}
	if err := repo.Push(pushOpts); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("git: pushing to %s: %w", remoteBranch, err)
	}
	return nil
}