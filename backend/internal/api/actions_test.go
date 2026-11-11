package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/secret"
	"gorm.io/gorm"
)

// authedServer builds the real Router bound to an off-the-shelf e2e server,
// with DevAuth off so ownership is enforced through an actual sealed cookie.
func authedServer(t *testing.T) (*Server, *gorm.DB, *httptest.Server, *secret.Box) {
	t.Helper()
	s, gdb := e2eServer(t)
	box, err := secret.NewBox(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	s.box = box
	s.cfg = &config.Config{FrontendURL: "http://localhost:3000", DevAuth: false, SandboxWorkDir: t.TempDir()}
	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)
	return s, gdb, srv, box
}

func sealedCookie(t *testing.T, box *secret.Box, githubID int64) string {
	t.Helper()
	cookie, err := box.Encrypt([]byte(strconv.FormatInt(githubID, 10)))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return string(cookie)
}

// authedRequest performs a real HTTP round-trip through the router with the
// session cookie attached, exactly as the browser would.
func authedRequest(t *testing.T, srv *httptest.Server, method, path, body, cookie string) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "asl_session", Value: cookie})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestDestroyResponseReflectsTombstone(t *testing.T) {
	// The handler used to return the pre-teardown struct, so a successful destroy
	// answered `status: running` with a container_id that had just been deleted.
	s, gdb, srv, box := authedServer(t)
	sb := e2eSandbox(t, s, gdb, "destroy-response", 324, "exit 0")
	cookie := sealedCookie(t, box, 324)

	resp, data := authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+sb.ID+"/actions/destroy", "", cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("destroy status = %d, want 200 (body %s)", resp.StatusCode, data)
	}
	var got struct {
		Status            string  `json:"status"`
		DestroyedAt       *string `json:"destroyed_at"`
		ContainerID       string  `json:"container_id"`
		DestructionReason string  `json:"destruction_reason"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if got.Status != string(db.StatusDeleted) {
		t.Errorf("status = %q, want deleted", got.Status)
	}
	if got.DestroyedAt == nil {
		t.Error("destroyed_at = null, want the tombstone timestamp")
	}
	if got.ContainerID != "" {
		t.Errorf("container_id = %q, want empty after teardown", got.ContainerID)
	}
	if got.DestructionReason != string(db.DestroyUserRequested) {
		t.Errorf("destruction_reason = %q, want user_requested", got.DestructionReason)
	}
}
