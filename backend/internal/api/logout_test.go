package api

import (
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/secret"
)

// Signing out is the one action a user reaches for when their session is already
// broken, so the route must work precisely in the state where requireUser would
// reject it. Behind the guard it answered 401, the browser kept the bad cookie,
// and the client's redirect never ran — leaving no way out of the page.
//
// These tests drive the real router with a real secret box, so requireUser does
// the same decryption it does in production.

// logoutServer builds a router whose only dependency is the session box, since
// neither requireUser's cookie check nor handleLogout touches the database when
// the session is absent or undecryptable.
func logoutServer(t *testing.T) http.Handler {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("key: %v", err)
	}
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	s := New(&config.Config{}, db.NewStore(nil), nil, nil, nil, box, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s.Router()
}

func post(t *testing.T, h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, nil)
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: "asl_session", Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// clearsSessionCookie asserts the response expires the session cookie, which is
// the only thing the browser needs to do for sign-out to be real.
func clearsSessionCookie(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var found *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "asl_session" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("no asl_session cookie in the response; the cookie was not cleared")
	}
	if found.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want a negative value to delete the cookie", found.MaxAge)
	}
	if found.Path != "/" {
		t.Errorf("Path = %q, want / so it matches the cookie it must delete", found.Path)
	}
}

func TestLogoutWithoutAnySession(t *testing.T) {
	h := logoutServer(t)
	w := post(t, h, "/api/auth/logout", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; signing out must not require a session", w.Code)
	}
	clearsSessionCookie(t, w)
}

// The reported failure: a cookie that no longer decrypts, e.g. after the server
// key is rotated or the cookie is tampered with.
func TestLogoutWithUndecryptableSession(t *testing.T) {
	h := logoutServer(t)
	w := post(t, h, "/api/auth/logout", "not-a-valid-ciphertext")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; a bad cookie is exactly when logout is needed", w.Code)
	}
	clearsSessionCookie(t, w)
}

func TestLogoutIsIdempotent(t *testing.T) {
	h := logoutServer(t)
	for i := range 2 {
		w := post(t, h, "/api/auth/logout", "")
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200", i+1, w.Code)
		}
		clearsSessionCookie(t, w)
	}
}

// Moving logout out of the protected group must not have weakened the routes
// that genuinely need a session.
func TestProtectedRoutesStillRejectBadSessions(t *testing.T) {
	h := logoutServer(t)
	for _, path := range []string{"/api/me", "/api/sandboxes/"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "asl_session", Value: "not-a-valid-ciphertext"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s with a bad session: status = %d, want 401", path, w.Code)
		}
	}
}
