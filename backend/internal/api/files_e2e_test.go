package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// This drives the real router and a real sandbox row. It reproduces the
// reported PoC: a repository commits a symlink to a host file, then the editor
// reads through it and writes through it.
//
// Requires ASL_TEST_DATABASE_URL and a Docker daemon, like the other e2e tests.
func TestFileAPISymlinkEscapeLive(t *testing.T) {
	s, gdb, srv, box := authedServer(t)
	sb := e2eSandbox(t, s, gdb, "symlink-api", 325, "sleep 30")
	cookie := sealedCookie(t, box, 325)

	root := filepath.Join(s.cfg.SandboxWorkDir, sb.Subdomain)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("host secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}

	resp, data := authedRequest(t, srv, http.MethodGet,
		"/api/sandboxes/"+sb.ID+"/file?path=leak", "", cookie)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("GET followed a symlink: status %d body %s", resp.StatusCode, data)
	}

	resp, data = authedRequest(t, srv, http.MethodPut,
		"/api/sandboxes/"+sb.ID+"/file", `{"path":"leak","content":"overwritten"}`, cookie)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("PUT wrote through a symlink: status %d body %s", resp.StatusCode, data)
	}
	if got, err := os.ReadFile(secret); err != nil || string(got) != "host secret" {
		t.Fatalf("outside file changed: %q, err=%v", got, err)
	}
}
