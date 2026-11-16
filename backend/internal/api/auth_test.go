package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/secret"
)

func authTestServer(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	box, err := secret.NewBox(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, db.NewStore(nil), nil, nil, nil, box, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s.Router()
}

func oauthConfig(appEnv string) *config.Config {
	return &config.Config{
		AppEnv:                 appEnv,
		FrontendURL:            "http://localhost:3000",
		GitHubClientID:         "client-id",
		GitHubClientSecret:     "client-secret",
		GitHubOAuthCallbackURL: "http://localhost:8080/api/auth/github/callback",
	}
}

func stateCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthStateCookieName {
			return c
		}
	}
	t.Fatal("OAuth state cookie not set")
	return nil
}

func TestAuthStartSetsHttpOnlyStateCookie(t *testing.T) {
	h := authTestServer(t, oauthConfig("development"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("redirect has no OAuth state")
	}
	c := stateCookie(t, rec)
	if c.Value != state {
		t.Errorf("state cookie = %q, redirect state = %q", c.Value, state)
	}
	if !c.HttpOnly {
		t.Error("state cookie is not HttpOnly")
	}
	if c.Secure {
		t.Error("state cookie is Secure in development")
	}
	if c.MaxAge <= 0 || c.MaxAge > 600 {
		t.Errorf("state cookie MaxAge = %d, want short-lived", c.MaxAge)
	}
}

func TestAuthCallbackRejectsMissingOrMismatchedState(t *testing.T) {
	h := authTestServer(t, oauthConfig("development"))
	cases := []struct {
		name   string
		target string
		cookie string
	}{
		{"missing cookie", "/api/auth/github/callback?code=x&state=s", ""},
		{"mismatched", "/api/auth/github/callback?code=x&state=s", "other"},
		{"missing state", "/api/auth/github/callback?code=x", "s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: tc.cookie})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, "auth_error=invalid_state") {
				t.Fatalf("Location = %q, want an auth_error=invalid_state redirect", loc)
			}
		})
	}
}

func TestAuthCallbackAcceptsMatchingStateBeforeExchange(t *testing.T) {
	h := authTestServer(t, oauthConfig("development"))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?state=s", nil)
	req.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "s"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// No code was supplied, so it gets past the state check and fails on the
	// code instead of exercising the network.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "auth_error=missing_code") {
		t.Fatalf("Location = %q, want an auth_error=missing_code redirect", loc)
	}
}

func TestCookiesAreSecureOutsideDevelopment(t *testing.T) {
	for _, appEnv := range []string{"production", "staging"} {
		t.Run(appEnv, func(t *testing.T) {
			h := authTestServer(t, oauthConfig(appEnv))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github", nil))
			if !stateCookie(t, rec).Secure {
				t.Errorf("state cookie is not Secure with APP_ENV=%s", appEnv)
			}

			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))
			var session *http.Cookie
			for _, c := range rec.Result().Cookies() {
				if c.Name == oauthStateCookie {
					session = c
				}
			}
			if session == nil || !session.Secure {
				t.Errorf("logout cookie Secure = %v with APP_ENV=%s, want true", session, appEnv)
			}
		})
	}
}
