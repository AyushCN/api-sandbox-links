package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A repository can contain symlinks. The file API must never follow one out of
// the sandbox work directory, either for reading or for writing.
func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("host secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}

	if got, err := safeJoin(root, "leak"); err == nil {
		t.Fatalf("safeJoin(followed symlink) = %q, nil; want an error", got)
	}
}

func TestSafeJoinRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("host secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-dir")); err != nil {
		t.Fatal(err)
	}

	if got, err := safeJoin(root, "outside-dir/secret.txt"); err == nil {
		t.Fatalf("safeJoin(symlinked directory) = %q, nil; want an error", got)
	}
}

func TestSafeJoinAllowsNormalNestedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := safeJoin(root, "src/new-file.js")
	if err != nil {
		t.Fatalf("safeJoin: %v", err)
	}
	want := filepath.Join(root, "src", "new-file.js")
	if got != want {
		t.Fatalf("safeJoin = %q, want %q", got, want)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../etc/passwd", "/etc/passwd", "a/../../etc/passwd", "."} {
		if _, err := safeJoin(root, rel); err == nil {
			t.Errorf("safeJoin(%q) = nil error, want rejection", rel)
		}
	}
}

func TestReadFileLimit(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "big.txt")
	if err := os.WriteFile(name, []byte(strings.Repeat("x", maxFileBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readSandboxFile(root, "big.txt"); err == nil {
		t.Fatal("readSandboxFile read a file over the limit, want an error")
	}
}

func TestWriteFileLimit(t *testing.T) {
	root := t.TempDir()
	err := writeSandboxFile(root, "big.txt", []byte(strings.Repeat("x", maxFileBytes+1)))
	if err == nil {
		t.Fatal("writeSandboxFile wrote a file over the limit, want an error")
	}
	if _, statErr := os.Stat(filepath.Join(root, "big.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("oversized file was created (stat err=%v)", statErr)
	}
}
