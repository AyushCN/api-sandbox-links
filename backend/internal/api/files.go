package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/api-sandbox-links/backend/internal/workspacelock"
)

// fileEntry is the shape of one node in the file tree.
type fileEntry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

// FileContentResult holds the data and metadata for an inspected file.
type FileContentResult struct {
	Path    string
	Data    []byte
	Size    int64
	Binary  bool
	ModTime time.Time
}

// handleFileTree lists every file under the sandbox's working directory,
// recursively and sorted, so the editor sidebar can render a simple tree.
func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil || sb.Status == db.StatusDeleted || sb.Status == db.StatusExpired {
		writeErr(w, http.StatusConflict, "sandbox is destroyed; nothing to read")
		return
	}
	rootHandle, err := s.sandboxRoot(sb.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "working directory not found (sandbox may still be building or was destroyed)")
		return
	}
	defer rootHandle.Close()

	out, err := readFileTree(rootHandle)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not walk working directory")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// readFileTree lists all non-git files within the workspace root handle.
func readFileTree(rootHandle *os.Root) ([]fileEntry, error) {
	var out []fileEntry
	err := fs.WalkDir(rootHandle.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable subtrees (e.g. node_modules permission races)
		}
		if path == "." {
			return nil
		}
		// The git internals are never editable through the UI; shows less noise.
		if isGitPath(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, fileEntry{
			Path: filepath.ToSlash(path),
			Dir:  d.IsDir(),
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// handleFileContent returns one file's content. Binary files are base64-encoded
// and flagged so the editor can warn before opening them.
func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	if sb.DestroyedAt != nil || sb.Status == db.StatusDeleted || sb.Status == db.StatusExpired {
		writeErr(w, http.StatusConflict, "sandbox is destroyed; nothing to read")
		return
	}
	rootHandle, err := s.sandboxRoot(sb.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "working directory not found")
		return
	}
	defer rootHandle.Close()

	rel := r.URL.Query().Get("path")
	res, err := readFileContent(rootHandle, rel)
	if err != nil {
		var hErr httpErr
		if errors.As(err, &hErr) {
			writeErr(w, http.StatusBadRequest, hErr.Error())
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			writeErr(w, http.StatusNotFound, "file not found")
			return
		}
		writeErr(w, http.StatusNotFound, "file not found")
		return
	}

	resp := map[string]any{
		"path":   res.Path,
		"size":   res.Size,
		"binary": res.Binary,
		"mtime":  res.ModTime,
	}
	if res.Binary {
		resp["content"] = base64.StdEncoding.EncodeToString(res.Data)
	} else {
		resp["content"] = string(res.Data)
	}
	writeJSON(w, http.StatusOK, resp)
}

// readFileContent safely opens and reads a file strictly confined within rootHandle.
func readFileContent(rootHandle *os.Root, relPath string) (*FileContentResult, error) {
	cleanPath, err := validateRelativePath(relPath)
	if err != nil {
		return nil, err
	}
	if isGitPath(cleanPath) {
		return nil, httpErr("git internals are not accessible")
	}

	f, err := rootHandle.Open(filepath.FromSlash(cleanPath))
	if err != nil {
		if isPathEscape(err) {
			return nil, httpErr("path escapes the working directory")
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, os.ErrNotExist
	}
	if info.IsDir() {
		return nil, httpErr("path is a directory")
	}

	if isFdInGit(f, rootHandle.Name()) {
		return nil, httpErr("git internals are not accessible")
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("could not read file: %w", err)
	}

	isBin := isBinary(data)
	return &FileContentResult{
		Path:    cleanPath,
		Data:    data,
		Size:    int64(len(data)),
		Binary:  isBin,
		ModTime: info.ModTime(),
	}, nil
}

// saveFileRequest is the body of PUT /file.
type saveFileRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	IsBase64 bool   `json:"is_base64"`
}

// redeployLocks prevents stacking redeploys when several saves land quickly.
var redeployLocks sync.Map

// handleSaveFile writes the edited file to the working directory and triggers
// a save-redeploy so the public URL serves the new behavior (Phase F: Save &
// Redeploy).
func (s *Server) handleSaveFile(w http.ResponseWriter, r *http.Request) {
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
		writeErr(w, http.StatusConflict, "sandbox is destroyed; nothing to edit")
		return
	}

	var req saveFileRequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}

	cleanPath, err := validateRelativePath(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if isGitPath(cleanPath) {
		writeErr(w, http.StatusBadRequest, "git internals are not editable")
		return
	}

	var data []byte
	if req.IsBase64 {
		data, err = base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid base64 content")
			return
		}
	} else {
		data = []byte(req.Content)
	}

	saveLock, err := s.locks().AcquireSave(r.Context(), sb.ID)
	if err != nil {
		if errors.Is(err, workspacelock.ErrForkActive) {
			writeErr(w, http.StatusConflict, "sandbox is currently being forked; please retry")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not acquire workspace lock")
		return
	}
	defer saveLock.Release()

	if s.forkHooks != nil && s.forkHooks.onBeforeSaveWrite != nil {
		s.forkHooks.onBeforeSaveWrite(sb.ID)
	}

	rootHandle, err := s.sandboxRoot(sb.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "working directory not found (sandbox may still be building)")
		return
	}
	defer rootHandle.Close()

	if err := saveFileContent(rootHandle, req.Path, data); err != nil {
		var hErr httpErr
		if errors.As(err, &hErr) {
			writeErr(w, http.StatusBadRequest, hErr.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not write file")
		return
	}

	if s.store != nil {
		_ = s.store.RecordFileEdit(sb.ID, cleanPath, user.ID)
	}

	// Release workspace lock: disk writes and audit logging are complete.
	// Releasing the write lock before spawning the background redeploy ensures
	// the background build never races with an in-progress write or deadlocks
	// against a lock held by the launching HTTP request.
	saveLock.Release()

	// Trigger the redeploy in the background, deduped so rapid-fire saves don't
	// start three rebuilds at once.
	if s.deployer != nil {
		_, already := redeployLocks.LoadOrStore(sb.ID, struct{}{})
		if !already {
			started := s.goBackground(func(ctx context.Context) {
				defer redeployLocks.Delete(sb.ID)
				if err := s.deployer.DeploySandbox(ctx, sb, orchestrator.DeployOptions{Trigger: db.TriggerSaveRedeploy}); err != nil {
					if s.logger != nil {
						s.logger.Warn("save redeploy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
					}
				}
			})
			if !started {
				redeployLocks.Delete(sb.ID)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "path": cleanPath, "redeploying": true})
}

// saveFileContent securely writes data to a file strictly confined within rootHandle.
// It guarantees that NO symbolic links (internal or external, intermediate or terminal)
// are followed during path resolution, preventing arbitrary file overwrite and write-through
// symlink redirection atomically.
//
// Truncation ordering:
// 1. Open without O_TRUNC (using O_NONBLOCK to prevent hanging on FIFOs).
// 2. Verify descriptor is a regular file.
// 3. Verify descriptor does not resolve to .git.
// 4. Truncate descriptor.
// 5. Write data.
func saveFileContent(rootHandle *os.Root, relPath string, data []byte) error {
	cleanPath, err := validateRelativePath(relPath)
	if err != nil {
		return err
	}
	if isGitPath(cleanPath) {
		return httpErr("git internals are not editable")
	}

	relOS := filepath.FromSlash(cleanPath)

	f, err := openNoSymlinkWrite(rootHandle, relOS)
	if err != nil {
		return err
	}
	defer f.Close()

	if isFdInGit(f, rootHandle.Name()) {
		return httpErr("git internals are not editable")
	}

	// Safe to truncate now that descriptor is confirmed to be an isolated regular file.
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("could not truncate file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("could not write file: %w", err)
	}
	return f.Close()
}

// sandboxRoot opens the sandbox's on-disk working directory as an *os.Root.
// The caller is responsible for calling Close() on the returned Root.
func (s *Server) sandboxRoot(sandboxID string) (*os.Root, error) {
	sb, err := s.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	return openSandboxRoot(s.cfg.SandboxWorkDir, sb.Subdomain)
}

// openSandboxRoot opens the sandbox working directory beneath workDir as an *os.Root.
// It verifies that subdomain is valid, exists, and is a real directory (not a symlink).
func openSandboxRoot(workDir, subdomain string) (*os.Root, error) {
	subdomain = strings.TrimSpace(subdomain)
	if subdomain == "" {
		return nil, errors.New("empty subdomain")
	}
	if subdomain == "." || subdomain == ".." || strings.Contains(subdomain, "/") || strings.Contains(subdomain, "\\") || strings.Contains(subdomain, "..") {
		return nil, errors.New("unsafe subdomain")
	}

	parentRoot, err := os.OpenRoot(workDir)
	if err != nil {
		return nil, err
	}
	defer parentRoot.Close()

	fi, err := parentRoot.Lstat(subdomain)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("sandbox workspace is not a valid directory")
	}

	return parentRoot.OpenRoot(subdomain)
}

// validateRelativePath validates that rel is a clean relative path that cannot
// escape the workspace root lexically.
func validateRelativePath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", httpErr("path is required")
	}
	if strings.ContainsRune(rel, 0) {
		return "", httpErr("path contains invalid characters")
	}
	// Reject colons (Windows drive letters, ADS, scheme prefixes)
	if strings.Contains(rel, ":") {
		return "", httpErr("path must be relative and inside the working directory")
	}
	// Normalize backslashes to forward slashes across all platforms
	normalized := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", httpErr("path must be relative and inside the working directory")
	}

	clean := path.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", httpErr("path must be relative and inside the working directory")
	}
	return clean, nil
}

// isGitPath reports whether the path targets .git internals.
func isGitPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == ".git" || strings.HasPrefix(clean, ".git/")
}

// isPathEscape reports whether err was caused by an attempt to escape an os.Root.
func isPathEscape(err error) bool {
	if err == nil {
		return false
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		if pe.Err != nil && strings.Contains(strings.ToLower(pe.Err.Error()), "escapes") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "escapes")
}

// safeJoin combines root with a user-supplied relative path, refusing any path
// that would escape root via ".." or absolute segments.
func safeJoin(root, rel string) (string, error) {
	cleanRel, err := validateRelativePath(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Clean(filepath.Join(root, filepath.FromSlash(cleanRel)))
	relRoot, err := filepath.Rel(root, full)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return "", httpErr("path escapes the working directory")
	}
	return full, nil
}

// httpErr is a tiny error type whose message is safe to show to users.
type httpErr string

func (e httpErr) Error() string { return string(e) }

// isBinary guesses whether content is binary by scanning the first chunk for a
// NUL byte (a reliable signal for almost all text encodings we serve).
func isBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
