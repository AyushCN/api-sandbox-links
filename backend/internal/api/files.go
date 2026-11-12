package api

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
)

// fileEntry is the shape of one node in the file tree.
type fileEntry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

// handleFileTree lists every file under the sandbox's working directory,
// recursively and sorted, so the editor sidebar can render a simple tree.
func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	root := filepath.Join(s.cfg.SandboxWorkDir, sb.Subdomain)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		writeErr(w, http.StatusNotFound, "working directory not found (sandbox may still be building or was destroyed)")
		return
	}

	var out []fileEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable subtrees (e.g. node_modules permission races)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		// The git internals are never editable through the UI; shows less noise.
		if strings.HasPrefix(rel, ".git") {
			return nil
		}
		out = append(out, fileEntry{
			Path: filepath.ToSlash(rel),
			Dir:  info.IsDir(),
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not walk working directory")
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// handleFileContent returns one file's content. Binary files are base64-encoded
// and flagged so the editor can warn before opening them.
func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.requireOwnedSandbox(w, r)
	if !ok {
		return
	}
	root, err := s.sandboxRoot(sb.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "working directory not found")
		return
	}
	rel := r.URL.Query().Get("path")
	full, err := safeJoin(root, rel)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		writeErr(w, http.StatusNotFound, "file not found")
		return
	}
	info, _ := os.Stat(full)
	isBinary := isBinary(data)
	resp := map[string]any{
		"path":   rel,
		"size":   len(data),
		"binary": isBinary,
		"mtime":  info.ModTime(),
	}
	if isBinary {
		resp["content"] = base64.StdEncoding.EncodeToString(data)
	} else {
		resp["content"] = string(data)
	}
	writeJSON(w, http.StatusOK, resp)
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
	if sb.DestroyedAt != nil {
		writeErr(w, http.StatusConflict, "sandbox is destroyed; nothing to edit")
		return
	}
	var req saveFileRequest
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}

	root, err := s.sandboxRoot(sb.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "working directory not found (sandbox may still be building)")
		return
	}
	full, err := safeJoin(root, req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.HasPrefix(filepath.Clean(req.Path), ".git") {
		writeErr(w, http.StatusBadRequest, "git internals are not editable")
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create directories")
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
	if err := os.WriteFile(full, data, 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not write file")
		return
	}
	_ = s.store.RecordFileEdit(sb.ID, req.Path, user.ID)

	// Trigger the redeploy in the background, deduped so rapid-fire saves don't
	// start three rebuilds at once.
	_, already := redeployLocks.LoadOrStore(sb.ID, struct{}{})
	if !already {
		go func() {
			defer redeployLocks.Delete(sb.ID)
			ctx, cancel := context.WithTimeout(context.Background(), orchestrator.DeployTimeout)
			defer cancel()
			if err := s.deployer.DeploySandbox(ctx, sb, orchestrator.DeployOptions{Trigger: db.TriggerSaveRedeploy}); err != nil {
				s.logger.Warn("save redeploy failed", slog.String("sandbox", sb.ID), slog.String("err", err.Error()))
			}
		}()
	}

	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "path": req.Path, "redeploying": true})
}

// sandboxRoot resolves the sandbox's on-disk working directory.
func (s *Server) sandboxRoot(sandboxID string) (string, error) {
	sb, err := s.store.GetSandbox(sandboxID)
	if err != nil {
		return "", err
	}
	root := filepath.Join(s.cfg.SandboxWorkDir, sb.Subdomain)
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	return root, nil
}

// safeJoin combines root with a user-supplied relative path, refusing any path
// that would escape root via ".." or absolute segments.
func safeJoin(root, rel string) (string, error) {
	rel = filepath.FromSlash(rel)
	clean := filepath.Clean(rel)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", httpErr("path must be relative and inside the working directory")
	}
	full := filepath.Join(root, clean)
	// Double-check the resolved path stays under root.
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
