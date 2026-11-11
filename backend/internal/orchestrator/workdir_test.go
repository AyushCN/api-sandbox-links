package orchestrator

// Path-safety regression tests for the ONE function that unlinks a sandbox
// work directory. The manual destroy handler delegates here, the expiry
// reaper calls it directly (it previously did a raw os.RemoveAll on
// root+subdomain — an empty subdomain would have deleted the whole work
// root), and the deploy race cleanups rely on it being idempotent.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveSandboxWorkDirRejectsUnsafeSubdomains(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"", "   ", ".", "..", "foo/../bar", "/etc", `foo\bar`, "..\\etc"} {
		if err := RemoveSandboxWorkDir(root, sub); err == nil {
			t.Errorf("RemoveSandboxWorkDir(%q) = nil, want an error", sub)
		}
	}
	// The root itself must survive every rejection attempt.
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("work root damaged by rejected removals: %v", err)
	}
}

func TestRemoveSandboxWorkDirRemovesOnlyTarget(t *testing.T) {
	root := t.TempDir()
	mk := func(sub string) string {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatalf("creating %s: %v", sub, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nested", "file.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", sub, err)
		}
		return dir
	}
	victim, bystander := mk("victim"), mk("bystander")

	if err := RemoveSandboxWorkDir(root, "victim"); err != nil {
		t.Fatalf("RemoveSandboxWorkDir: %v", err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Errorf("victim dir still present (err=%v), want removed", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Errorf("bystander sandbox dir damaged: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("work root damaged: %v", err)
	}

	// Idempotent: repeated removal (double destroy, destroy + reaper, deploy
	// cleanup racing the reaper) must succeed on the now-missing directory.
	if err := RemoveSandboxWorkDir(root, "victim"); err != nil {
		t.Errorf("repeated removal = %v, want nil (cleanup must be idempotent)", err)
	}
}

func TestRemoveSandboxWorkDirMissingIsSuccess(t *testing.T) {
	root := t.TempDir()
	if err := RemoveSandboxWorkDir(root, "never-existed"); err != nil {
		t.Errorf("removing a missing dir = %v, want nil", err)
	}
}
