// Package api implements the REST + WebSocket surface of the backend. Handlers
// stay thin: they validate/authorize requests, then delegate to the store,
// orchestrator, deployer, and other internal packages.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	gh "github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"
	oauth2github "golang.org/x/oauth2/github"
)

// oauthStateCookie is the session cookie name. The value is an AES-encrypted
// GitHub numeric ID; decryption doubles as authenticity (attacker-crafted
// cookies fail authentication). See secret.Box.
const oauthStateCookie = "asl_session"

// devUser is used only when OAuth is not configured (DevAuth mode) so the
// deploy loop is testable end-to-end without a GitHub App.
var devUser = db.User{
	ID:          "00000000-0000-0000-0000-000000000001",
	GithubID:    1,
	Username:    "devuser",
	GithubToken: []byte("dev"), // non-empty so the NOT NULL column is satisfied
}

type ctxKey int

const (
	ctxUserKey ctxKey = iota
)

// githubOAuthConfig builds the oauth2 config from server config. It returns
// nil when GitHub OAuth is not configured (DevAuth mode).
func (s *Server) githubOAuthConfig() *oauth2.Config {
	if s.cfg.GitHubClientID == "" {
		return nil
	}
	return &oauth2.Config{
		ClientID:     s.cfg.GitHubClientID,
		ClientSecret: s.cfg.GitHubClientSecret,
		RedirectURL:  s.cfg.GitHubOAuthCallbackURL,
		Scopes:       []string{"read:user", "user:email", "repo"},
		Endpoint:     oauth2github.Endpoint,
	}
}

// handleAuthStart redirects the user to GitHub's consent screen.
func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	cfg := s.githubOAuthConfig()
	if cfg == nil {
		http.Error(w, "GitHub OAuth is not configured (GITHUB_CLIENT_ID missing). DevAuth is active; try the health endpoint.", http.StatusNotImplemented)
		return
	}
	state := fmt.Sprintf("%d", time.Now().UnixNano())
	url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusFound)
}

// handleAuthCallback exchanges the code, fetches the GitHub profile, upserts
// the user, and sets the session cookie.
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	cfg := s.githubOAuthConfig()
	if cfg == nil {
		http.Error(w, "OAuth not configured", http.StatusNotImplemented)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing OAuth code", http.StatusBadRequest)
		return
	}

	tok, err := cfg.Exchange(r.Context(), code)
	if err != nil {
		s.logger.Warn("auth: token exchange failed", slog.String("err", err.Error()))
		http.Error(w, "GitHub token exchange failed", http.StatusUnauthorized)
		return
	}

	profile, err := s.fetchGithubUser(r.Context(), tok.AccessToken)
	if err != nil {
		s.logger.Warn("auth: profile fetch failed", slog.String("err", err.Error()))
		http.Error(w, "Could not fetch GitHub profile", http.StatusBadGateway)
		return
	}

	encToken, err := s.box.Encrypt([]byte(tok.AccessToken))
	if err != nil {
		http.Error(w, "could not store access token", http.StatusInternalServerError)
		return
	}

	user := &db.User{
		GithubID:    profile.GithubID,
		Username:    profile.Login,
		Email:       s.truncate(profile.Email, 512),
		AvatarURL:   s.truncate(profile.AvatarURL, 1024),
		GithubToken: encToken,
	}
	if err := s.store.UpsertUser(user); err != nil {
		s.logger.Warn("auth: upserting user failed", slog.String("err", err.Error()))
		http.Error(w, "could not store user", http.StatusInternalServerError)
		return
	}

	session, err := s.box.Encrypt([]byte(strconv.FormatInt(user.GithubID, 10)))
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    string(session),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil,
		MaxAge:   30 * 24 * 3600, // 30 days
	})
	http.Redirect(w, r, s.cfg.FrontendURL+"/dashboard?authed=1", http.StatusFound)
}

// handleMe returns the current signed-in user (used by the frontend to decide
// whether to show a login prompt).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFrom(r)
	if !ok {
		http.Error(w, "not signed in", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"github_id":  user.GithubID,
		"username":   user.Username,
		"avatar_url": user.AvatarURL,
		"dev":        s.cfg.DevAuth,
	})
}

// handleLogout clears the session cookie.
//
// It is intentionally unauthenticated and idempotent: a caller with a missing,
// corrupt or expired session must still be able to clear the cookie, otherwise
// the only way out is to wait out the cookie's MaxAge. It answers 200 whether or
// not a session was actually present, so the client's logout flow never has to
// distinguish the two cases.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Mirror the attributes used when the session was set. Browsers identify a
	// cookie by name+domain+path, but keeping HttpOnly/SameSite in step avoids a
	// silent no-op if the set-cookie above ever gains Secure or a narrower Path.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// requireUser is middleware ensuring a session exists; it resolves the user
// from the signed session cookie and stashes the *db.User in context. In
// DevAuth mode it fabricates the dev user instead of failing.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.sessionUser(r)
		if err != nil && !s.cfg.DevAuth {
			http.Error(w, "not signed in", http.StatusUnauthorized)
			return
		}
		if err != nil {
			// DevAuth: fabricate the dev identity. Make sure it exists in the
			// DB so repositories/sandboxes get a valid user_id foreign key.
			if err := s.store.UpsertUser(&devUser); err != nil {
				s.logger.Warn("dev auth: could not seed dev user", slog.String("err", err.Error()))
			}
			user = &devUser
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sessionUser decodes and authenticates the session cookie.
func (s *Server) sessionUser(r *http.Request) (*db.User, error) {
	cookie, err := r.Cookie(oauthStateCookie)
	if err != nil {
		return nil, err
	}
	plain, err := s.box.Decrypt([]byte(cookie.Value))
	if err != nil {
		return nil, fmt.Errorf("invalid session cookie: %w", err)
	}
	githubID, err := strconv.ParseInt(string(plain), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed session: %w", err)
	}
	user, err := s.store.GetUserByGithubID(githubID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// userFrom pulls the authenticated user out of the request context.
func (s *Server) userFrom(r *http.Request) (*db.User, bool) {
	u, ok := r.Context().Value(ctxUserKey).(*db.User)
	return u, ok
}

func (s *Server) truncate(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

// githubProfile is the minimal subset of go-github's User we read.
type githubProfile struct {
	GithubID  int64
	Email     string
	AvatarURL string
	Login     string
}

// fetchGithubUser calls GitHub's /user endpoint with the OAuth token using the
// official go-github client.
func (s *Server) fetchGithubUser(ctx context.Context, token string) (*githubProfile, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := gh.NewClient(oauth2.NewClient(ctx, ts))
	ghUser, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fetching github user: %w", err)
	}
	return &githubProfile{
		GithubID:  ghUser.GetID(),
		Email:     ghUser.GetEmail(),
		AvatarURL: ghUser.GetAvatarURL(),
		Login:     ghUser.GetLogin(),
	}, nil
}
