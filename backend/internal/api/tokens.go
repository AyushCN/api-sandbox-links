package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// APITokenPrefix marks our tokens so a leaked one is recognisable in a scan.
const APITokenPrefix = "asl_"

// apiTokenJSON is the list/create shape. The plaintext secret appears only in
// the create response; listing returns the prefix and metadata.
func apiTokenJSON(t *db.APIToken) map[string]any {
	return map[string]any{
		"id":           t.ID,
		"name":         t.Name,
		"prefix":       t.Prefix,
		"created_at":   t.CreatedAt,
		"last_used_at": t.LastUsedAt,
	}
}

// hashAPIToken is the one-way digest stored at rest. SHA-256 is enough here
// because the secret is 256 bits of randomness, not a user-chosen password.
func hashAPIToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// handleListTokens serves GET /api/tokens.
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "auth_unauthorized", "not signed in")
		return
	}
	rows, err := s.store.ListAPITokens(user.ID)
	if err != nil {
		s.logger.Warn("tokens: listing failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "list_tokens_failed", "could not list API tokens")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		out = append(out, apiTokenJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// handleCreateToken serves POST /api/tokens. The plaintext secret is returned
// exactly once, in this response body.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "auth_unauthorized", "not signed in")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_invalid_body", "send a JSON body with an optional name")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "api token"
	}
	if len(name) > 100 {
		writeErr(w, http.StatusBadRequest, "validation_invalid_name", "name must be 100 characters or fewer")
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		s.logger.Warn("tokens: generating secret failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "token_create_failed", "could not generate a token")
		return
	}
	secret := APITokenPrefix + base64.RawURLEncoding.EncodeToString(raw)

	t := &db.APIToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Name:      name,
		TokenHash: hashAPIToken(secret),
		Prefix:    secret[:len(APITokenPrefix)+6],
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateAPIToken(t); err != nil {
		s.logger.Warn("tokens: creating failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "token_create_failed", "could not create the token")
		return
	}

	out := apiTokenJSON(t)
	out["token"] = secret
	writeJSON(w, http.StatusCreated, out)
}

// handleRevokeToken serves DELETE /api/tokens/{id}.
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "auth_unauthorized", "not signed in")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.RevokeAPIToken(user.ID, id); err != nil {
		if errors.Is(err, db.ErrAPITokenNotFound) {
			writeErr(w, http.StatusNotFound, "token_not_found", "no such API token")
			return
		}
		s.logger.Warn("tokens: revoking failed", slog.String("err", err.Error()))
		writeErr(w, http.StatusInternalServerError, "token_revoke_failed", "could not revoke the token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true, "id": id})
}

// bearerUser resolves an `Authorization: Bearer <secret>` header to its user.
// It returns (nil, nil) when the request carries no bearer header at all, so
// the cookie path can take over.
func (s *Server) bearerUser(r *http.Request) (*db.User, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, nil
	}
	secret := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if secret == "" {
		return nil, errNoBearerUser
	}
	t, err := s.store.GetAPITokenByHash(hashAPIToken(secret))
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(t.UserID)
	if err != nil {
		return nil, err
	}
	// Best effort: last_used_at is telemetry, never a gate.
	if err := s.store.TouchAPITokenLastUsed(t.ID); err != nil {
		s.logger.Debug("tokens: touch last_used_at failed", slog.String("err", err.Error()))
	}
	return user, nil
}

var errNoBearerUser = errors.New("api: empty bearer token")
