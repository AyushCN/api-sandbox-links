package api

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"golang.org/x/sys/unix"
)

// setupTestWorkspace creates an isolated work directory with a sandbox subdirectory.
func setupTestWorkspace(t *testing.T, subdomain string) (string, string) {
	t.Helper()
	workDir := t.TempDir()
	sbDir := filepath.Join(workDir, subdomain)
	if err := os.MkdirAll(sbDir, 0o755); err != nil {
		t.Fatalf("failed to create sandbox dir: %v", err)
	}
	return workDir, sbDir
}

// 1. Normal file read and write within the workspace.
func TestFileReadWriteNormal(t *testing.T) {
	workDir, _ := setupTestWorkspace(t, "sb-test")
	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Write normal file
	content := []byte("hello world from sandbox")
	if err := saveFileContent(root, "hello.txt", content); err != nil {
		t.Fatalf("saveFileContent failed: %v", err)
	}

	// Read normal file
	res, err := readFileContent(root, "hello.txt")
	if err != nil {
		t.Fatalf("readFileContent failed: %v", err)
	}
	if !bytes.Equal(res.Data, content) {
		t.Fatalf("expected content %q, got %q", content, res.Data)
	}
	if res.Binary {
		t.Fatalf("expected text file, flagged as binary")
	}

	// Write binary file (contains null byte)
	binContent := []byte("hello\x00binary\x00world")
	if err := saveFileContent(root, "binary.bin", binContent); err != nil {
		t.Fatalf("saveFileContent binary failed: %v", err)
	}
	resBin, err := readFileContent(root, "binary.bin")
	if err != nil {
		t.Fatalf("readFileContent binary failed: %v", err)
	}
	if !resBin.Binary {
		t.Fatalf("expected binary flag true, got false")
	}

	// Overwrite existing regular file
	newContent := []byte("updated content")
	if err := saveFileContent(root, "hello.txt", newContent); err != nil {
		t.Fatalf("saveFileContent update failed: %v", err)
	}
	resUpdated, err := readFileContent(root, "hello.txt")
	if err != nil {
		t.Fatalf("readFileContent after update failed: %v", err)
	}
	if !bytes.Equal(resUpdated.Data, newContent) {
		t.Fatalf("expected updated content %q, got %q", newContent, resUpdated.Data)
	}
}

// 2. Normal nested directory operations.
func TestFileNestedDirectoryOperations(t *testing.T) {
	workDir, _ := setupTestWorkspace(t, "sb-test")
	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	nestedPath := "src/components/ui/Button.tsx"
	nestedData := []byte("export const Button = () => <button>Click</button>;")
	if err := saveFileContent(root, nestedPath, nestedData); err != nil {
		t.Fatalf("saveFileContent nested failed: %v", err)
	}

	res, err := readFileContent(root, nestedPath)
	if err != nil {
		t.Fatalf("readFileContent nested failed: %v", err)
	}
	if !bytes.Equal(res.Data, nestedData) {
		t.Fatalf("expected content %q, got %q", nestedData, res.Data)
	}

	// Read file tree
	tree, err := readFileTree(root)
	if err != nil {
		t.Fatalf("readFileTree failed: %v", err)
	}
	found := false
	for _, entry := range tree {
		if entry.Path == nestedPath && !entry.Dir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in file tree, got %+v", nestedPath, tree)
	}
}

// 3. Lexical path traversal (../, ..\, etc.) rejection.
func TestPathTraversalLexical(t *testing.T) {
	workDir, _ := setupTestWorkspace(t, "sb-test")
	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	badPaths := []string{
		"../outside.txt",
		"..\\outside.txt",
		"foo/../../outside.txt",
		"foo/..",
		"..",
		".",
		"/etc/passwd",
		"\\etc\\passwd",
		"C:\\Windows\\System32",
		"foo/\x00/bar",
	}

	for _, p := range badPaths {
		_, err := readFileContent(root, p)
		if err == nil {
			t.Errorf("expected readFileContent(%q) to fail, but succeeded", p)
		}
		err = saveFileContent(root, p, []byte("evil"))
		if err == nil {
			t.Errorf("expected saveFileContent(%q) to fail, but succeeded", p)
		}
		_, err = safeJoin(workDir, p)
		if err == nil {
			t.Errorf("expected safeJoin(%q) to fail, but succeeded", p)
		}
	}
}

// 4. Absolute path rejection.
func TestAbsolutePathRejection(t *testing.T) {
	workDir, _ := setupTestWorkspace(t, "sb-test")
	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	absPaths := []string{
		"/etc/passwd",
		"/tmp/evil.txt",
		"/var/log/syslog",
	}

	for _, p := range absPaths {
		if _, err := readFileContent(root, p); err == nil {
			t.Errorf("expected readFileContent(%q) to be rejected", p)
		}
		if err := saveFileContent(root, p, []byte("evil")); err == nil {
			t.Errorf("expected saveFileContent(%q) to be rejected", p)
		}
	}
}

// 5. Symlink pointing to /etc/passwd.
func TestSymlinkToEtcPasswd(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Create symlink pointing to /etc/passwd
	symlinkPath := filepath.Join(sbDir, "passwd_link")
	if err := os.Symlink("/etc/passwd", symlinkPath); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Read attempt must be rejected
	res, err := readFileContent(root, "passwd_link")
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: readFileContent allowed reading /etc/passwd symlink! Data: %s", string(res.Data))
	}

	// Write attempt must be rejected
	err = saveFileContent(root, "passwd_link", []byte("root:evil:0:0::/root:/bin/bash\n"))
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: saveFileContent allowed writing to /etc/passwd symlink!")
	}
}

// 6. Symlink pointing to an external writable file.
// Tests must verify that external files remain unchanged.
func TestSymlinkToExternalWritableFile(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Create external file in isolated directory
	extDir := t.TempDir()
	extFile := filepath.Join(extDir, "victim.txt")
	originalData := []byte("VICTIM_INTACT_DATA_DO_NOT_OVERWRITE")
	if err := os.WriteFile(extFile, originalData, 0o644); err != nil {
		t.Fatalf("failed to create extFile: %v", err)
	}

	// Create symlink inside sandbox pointing to extFile
	linkPath := filepath.Join(sbDir, "victim_link")
	if err := os.Symlink(extFile, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Read must be rejected
	res, err := readFileContent(root, "victim_link")
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: readFileContent read external file: %s", string(res.Data))
	}

	// Write must be rejected
	err = saveFileContent(root, "victim_link", []byte("MALICIOUS_OVERWRITE"))
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: saveFileContent wrote through symlink!")
	}

	// Verify external file remains completely unchanged
	currentData, err := os.ReadFile(extFile)
	if err != nil {
		t.Fatalf("failed to read extFile: %v", err)
	}
	if !bytes.Equal(currentData, originalData) {
		t.Fatalf("SECURITY VIOLATION: external file was modified! Expected %q, got %q", originalData, currentData)
	}
}

// 7. Nested symlink traversal.
func TestNestedSymlinkTraversal(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// External dir with sensitive file
	extDir := t.TempDir()
	extFile := filepath.Join(extDir, "secret.json")
	if err := os.WriteFile(extFile, []byte(`{"apiKey": "secret123"}`), 0o644); err != nil {
		t.Fatalf("failed to create extFile: %v", err)
	}

	// Intermediate directory symlink: sbDir/ext_dir_link -> extDir
	dirLink := filepath.Join(sbDir, "ext_dir_link")
	if err := os.Symlink(extDir, dirLink); err != nil {
		t.Fatalf("failed to create dir symlink: %v", err)
	}

	// Chain of symlinks: link_chain -> link_target -> extFile
	linkTarget := filepath.Join(sbDir, "link_target")
	if err := os.Symlink(extFile, linkTarget); err != nil {
		t.Fatalf("failed to create link_target: %v", err)
	}
	linkChain := filepath.Join(sbDir, "link_chain")
	if err := os.Symlink("link_target", linkChain); err != nil {
		t.Fatalf("failed to create link_chain: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Read through directory symlink
	if _, err := readFileContent(root, "ext_dir_link/secret.json"); err == nil {
		t.Fatalf("SECURITY VIOLATION: read through directory symlink succeeded!")
	}

	// Write through directory symlink
	if err := saveFileContent(root, "ext_dir_link/new.txt", []byte("evil")); err == nil {
		t.Fatalf("SECURITY VIOLATION: write through directory symlink succeeded!")
	}

	// Read through symlink chain
	if _, err := readFileContent(root, "link_chain"); err == nil {
		t.Fatalf("SECURITY VIOLATION: read through symlink chain succeeded!")
	}

	// Write through symlink chain
	if err := saveFileContent(root, "link_chain", []byte("evil")); err == nil {
		t.Fatalf("SECURITY VIOLATION: write through symlink chain succeeded!")
	}

	// Verify extFile still untouched
	extData, _ := os.ReadFile(extFile)
	if !bytes.Equal(extData, []byte(`{"apiKey": "secret123"}`)) {
		t.Fatalf("external file was corrupted!")
	}
}

// 8. Dangling symlinks.
func TestDanglingSymlinks(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Symlink pointing to nonexistent file
	os.Symlink("nonexistent.txt", filepath.Join(sbDir, "dangling.txt"))

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Read dangling symlink: must return error (not found), no panic
	_, err = readFileContent(root, "dangling.txt")
	if err == nil {
		t.Fatalf("expected error reading dangling symlink, got nil")
	}

	// Write to dangling symlink: must be rejected (cannot overwrite symlink)
	err = saveFileContent(root, "dangling.txt", []byte("data"))
	if err == nil {
		t.Fatalf("expected error writing to dangling symlink, got nil")
	}

	// Ensure nonexistent target was NOT created
	if _, err := os.Stat(filepath.Join(sbDir, "nonexistent.txt")); !os.IsNotExist(err) {
		t.Fatalf("nonexistent.txt should not have been created")
	}
}

// 9. Symlink replacement TOCTOU race test.
func TestSymlinkReplacementTOCTOU(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	victimDir := t.TempDir()
	victimFile := filepath.Join(victimDir, "host_secret.txt")
	victimOriginal := []byte("HOST_SECRET_NEVER_MODIFY")
	if err := os.WriteFile(victimFile, victimOriginal, 0o644); err != nil {
		t.Fatalf("failed to create victimFile: %v", err)
	}

	targetPath := filepath.Join(sbDir, "racing_target.txt")
	if err := os.WriteFile(targetPath, []byte("INIT"), 0o644); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Concurrently toggle racing_target.txt between regular file and symlink to victimFile
	wg.Add(1)
	go func() {
		defer wg.Done()
		isSym := false
		for {
			select {
			case <-stop:
				return
			default:
				_ = os.Remove(targetPath)
				if isSym {
					_ = os.WriteFile(targetPath, []byte("REGULAR"), 0o644)
					isSym = false
				} else {
					_ = os.Symlink(victimFile, targetPath)
					isSym = true
				}
			}
		}
	}()

	// Concurrently write to racing_target.txt through root handle
	for i := 0; i < 200; i++ {
		_ = saveFileContent(root, "racing_target.txt", []byte("NEW_VALUE"))
	}

	close(stop)
	wg.Wait()

	// Verify victim file remained 100% UNCHANGED
	currentVictim, err := os.ReadFile(victimFile)
	if err != nil {
		t.Fatalf("failed to read victim: %v", err)
	}
	if !bytes.Equal(currentVictim, victimOriginal) {
		t.Fatalf("CRITICAL SECURITY VULNERABILITY: TOCTOU race modified victim file! Expected %q, got %q", victimOriginal, currentVictim)
	}
}

// 10. Symlink pointing inside the workspace (chosen policy).
// Policy:
// - Reading internal symlinks is allowed if target is a legitimate file within root.
// - Writing through symlinks is REFUSED to protect the link target from write-through.
// - Symlinks targeting .git are strictly rejected for both read and write.
func TestSymlinkPolicyInsideWorkspace(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Create legit internal file
	legitData := []byte("legitimate internal data")
	if err := os.WriteFile(filepath.Join(sbDir, "legit.txt"), legitData, 0o644); err != nil {
		t.Fatalf("failed to write legit: %v", err)
	}

	// Symlink to legit internal file
	if err := os.Symlink("legit.txt", filepath.Join(sbDir, "internal_link.txt")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Create .git directory and file
	gitDir := filepath.Join(sbDir, ".git")
	_ = os.MkdirAll(gitDir, 0o755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("git-config-secret"), 0o644)

	// Symlink targeting .git
	if err := os.Symlink(".git/config", filepath.Join(sbDir, "git_link")); err != nil {
		t.Fatalf("failed to create git symlink: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Reading internal symlink: allowed
	res, err := readFileContent(root, "internal_link.txt")
	if err != nil {
		t.Fatalf("expected reading internal symlink to succeed, got %v", err)
	}
	if !bytes.Equal(res.Data, legitData) {
		t.Fatalf("expected data %q, got %q", legitData, res.Data)
	}

	// Writing directly to internal symlink: rejected to prevent write-through target corruption
	err = saveFileContent(root, "internal_link.txt", []byte("overwrite attempt"))
	if err == nil {
		t.Fatalf("expected saveFileContent on symlink to be rejected, but succeeded")
	}
	// Verify target was not modified
	currentLegit, _ := os.ReadFile(filepath.Join(sbDir, "legit.txt"))
	if !bytes.Equal(currentLegit, legitData) {
		t.Fatalf("legit.txt was overwritten through symlink!")
	}

	// Reading symlink targeting .git: rejected
	if _, err := readFileContent(root, "git_link"); err == nil {
		t.Fatalf("reading symlink targeting .git must be rejected")
	}

	// Writing symlink targeting .git: rejected
	if err := saveFileContent(root, "git_link", []byte("evil")); err == nil {
		t.Fatalf("writing symlink targeting .git must be rejected")
	}
}

// 11. Workspace root itself being a symlink.
func TestWorkspaceRootIsSymlink(t *testing.T) {
	tempBase := t.TempDir()

	realWork := filepath.Join(tempBase, "real_work")
	symWork := filepath.Join(tempBase, "sym_work")
	_ = os.MkdirAll(filepath.Join(realWork, "sb-test"), 0o755)
	_ = os.WriteFile(filepath.Join(realWork, "sb-test", "file.txt"), []byte("hello"), 0o644)

	// sym_work -> real_work
	if err := os.Symlink(realWork, symWork); err != nil {
		t.Skipf("symlink creation failed: %v", err)
	}

	// External secret
	victimFile := filepath.Join(tempBase, "outside_secret.txt")
	_ = os.WriteFile(victimFile, []byte("SECRET"), 0o644)
	_ = os.Symlink(victimFile, filepath.Join(realWork, "sb-test", "evil.txt"))

	root, err := openSandboxRoot(symWork, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed on symlinked work root: %v", err)
	}
	defer root.Close()

	// Reading legitimate file within sandbox succeeds
	res, err := readFileContent(root, "file.txt")
	if err != nil {
		t.Fatalf("readFileContent failed: %v", err)
	}
	if string(res.Data) != "hello" {
		t.Fatalf("unexpected content %q", res.Data)
	}

	// Traversal outside real workspace is blocked
	if _, err := readFileContent(root, "evil.txt"); err == nil {
		t.Fatalf("reading evil.txt through symlinked work root must be blocked")
	}
	if _, err := readFileContent(root, "../outside_secret.txt"); err == nil {
		t.Fatalf("reading ../ outside must be blocked")
	}
}

// 12. Existing Finding F-05: Sandbox A cannot access Sandbox B files.
func TestFindingF05FileIsolationRegression(t *testing.T) {
	workDir := t.TempDir()

	// Create Sandbox A and Sandbox B side by side
	sbADir := filepath.Join(workDir, "sb-tenant-a")
	sbBDir := filepath.Join(workDir, "sb-tenant-b")
	_ = os.MkdirAll(sbADir, 0o755)
	_ = os.MkdirAll(sbBDir, 0o755)

	// Sandbox B holds private secret
	secretB := []byte("TENANT_B_CONFIDENTIAL_KEY")
	if err := os.WriteFile(filepath.Join(sbBDir, "secret.key"), secretB, 0o644); err != nil {
		t.Fatalf("failed to write tenant B secret: %v", err)
	}

	// Sandbox A contains a symlink targeting Sandbox B's secret
	if err := os.Symlink("../sb-tenant-b/secret.key", filepath.Join(sbADir, "link_to_b.key")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Open Sandbox A root
	rootA, err := openSandboxRoot(workDir, "sb-tenant-a")
	if err != nil {
		t.Fatalf("openSandboxRoot A failed: %v", err)
	}
	defer rootA.Close()

	// Sandbox A attempts to read Sandbox B's secret via relative path
	if _, err := readFileContent(rootA, "../sb-tenant-b/secret.key"); err == nil {
		t.Fatalf("F-05 VIOLATION: Sandbox A read Sandbox B's file via ../ traversal!")
	}

	// Sandbox A attempts to read Sandbox B's secret via symlink
	if res, err := readFileContent(rootA, "link_to_b.key"); err == nil {
		t.Fatalf("F-05 VIOLATION: Sandbox A read Sandbox B's file via symlink! Data: %s", string(res.Data))
	}

	// Sandbox A attempts to overwrite Sandbox B's secret via symlink
	if err := saveFileContent(rootA, "link_to_b.key", []byte("CORRUPTED_BY_A")); err == nil {
		t.Fatalf("F-05 VIOLATION: Sandbox A wrote to Sandbox B's file via symlink!")
	}

	// Verify Tenant B's secret is completely intact
	currentB, _ := os.ReadFile(filepath.Join(sbBDir, "secret.key"))
	if !bytes.Equal(currentB, secretB) {
		t.Fatalf("F-05 VIOLATION: Tenant B secret was modified! Expected %q, got %q", secretB, currentB)
	}
}

// 13. Subdomain root validation (openSandboxRoot).
func TestOpenSandboxRootValidation(t *testing.T) {
	workDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workDir, "valid-sb"), 0o755)

	// Valid sandbox opens cleanly
	root, err := openSandboxRoot(workDir, "valid-sb")
	if err != nil {
		t.Fatalf("openSandboxRoot valid-sb failed: %v", err)
	}
	root.Close()

	// Invalid subdomains rejected
	invalids := []string{"", "   ", "../escape", "sub/dir", "sub\\dir", ".", ".."}
	for _, sub := range invalids {
		if _, err := openSandboxRoot(workDir, sub); err == nil {
			t.Errorf("expected openSandboxRoot(%q) to fail", sub)
		}
	}

	// Symlink subdomain pointing outside workDir rejected
	extDir := t.TempDir()
	_ = os.Symlink(extDir, filepath.Join(workDir, "symlink-sb"))
	if _, err := openSandboxRoot(workDir, "symlink-sb"); err == nil {
		t.Errorf("expected openSandboxRoot on symlink subdomain to fail")
	}
}

// 14. safeJoin security verification.
func TestSafeJoinConfinement(t *testing.T) {
	root := t.TempDir()

	valid, err := safeJoin(root, "hello.txt")
	if err != nil || valid != filepath.Join(root, "hello.txt") {
		t.Fatalf("safeJoin valid failed: path=%s, err=%v", valid, err)
	}

	invalids := []string{
		"../escape.txt",
		"..\\escape.txt",
		"/etc/passwd",
		"foo/../../bar",
		"",
	}
	for _, inv := range invalids {
		if _, err := safeJoin(root, inv); err == nil {
			t.Errorf("expected safeJoin(%q) to fail", inv)
		}
	}
}

// 15. Adversarial: Internal symlink replacement during write.
// Tests that an internal symlink pointing to another file in the workspace
// cannot redirect a write to that victim file, even under a concurrent race.
func TestAdversarialInternalSymlinkReplacement(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Protected internal victim file
	victimPath := filepath.Join(sbDir, "internal_victim.txt")
	victimOriginal := []byte("CRITICAL_INTERNAL_STATE_DO_NOT_CORRUPT")
	if err := os.WriteFile(victimPath, victimOriginal, 0o644); err != nil {
		t.Fatalf("failed to create victim file: %v", err)
	}

	targetPath := filepath.Join(sbDir, "racing_file.txt")
	if err := os.WriteFile(targetPath, []byte("INIT"), 0o644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Concurrently swap target between regular file and internal symlink to victim
	wg.Add(1)
	go func() {
		defer wg.Done()
		isSym := false
		for {
			select {
			case <-stop:
				return
			default:
				_ = os.Remove(targetPath)
				if isSym {
					_ = os.WriteFile(targetPath, []byte("REGULAR"), 0o644)
					isSym = false
				} else {
					_ = os.Symlink("internal_victim.txt", targetPath)
					isSym = true
				}
			}
		}
	}()

	// Concurrently attempt to write through targetPath
	for i := 0; i < 200; i++ {
		_ = saveFileContent(root, "racing_file.txt", []byte("CORRUPTION_PAYLOAD"))
	}

	close(stop)
	wg.Wait()

	// Verify victim file is 100% UNCHANGED
	currentVictim, err := os.ReadFile(victimPath)
	if err != nil {
		t.Fatalf("failed to read victim: %v", err)
	}
	if !bytes.Equal(currentVictim, victimOriginal) {
		t.Fatalf("CRITICAL SECURITY FAILURE: internal victim file was modified via symlink redirection! Got %q", string(currentVictim))
	}
}

// 16. Adversarial: Intermediate directory symlink replacement during write.
// Tests that if an intermediate directory is replaced with an internal or external symlink,
// write is rejected atomically by the kernel without following the directory symlink.
func TestAdversarialIntermediateDirectorySymlink(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Create sensitive directory
	sensitiveDir := filepath.Join(sbDir, "sensitive_dir")
	_ = os.MkdirAll(sensitiveDir, 0o755)
	sensitiveFile := filepath.Join(sensitiveDir, "confidential.txt")
	sensitiveOriginal := []byte("CONFIDENTIAL_DO_NOT_OVERWRITE")
	_ = os.WriteFile(sensitiveFile, sensitiveOriginal, 0o644)

	// Create intermediate directory symlink: sbDir/link_dir -> sensitive_dir
	linkDir := filepath.Join(sbDir, "link_dir")
	if err := os.Symlink("sensitive_dir", linkDir); err != nil {
		t.Fatalf("failed to create dir symlink: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Writing to link_dir/confidential.txt must be rejected
	err = saveFileContent(root, "link_dir/confidential.txt", []byte("ATTACK_PAYLOAD"))
	if err == nil {
		t.Fatalf("SECURITY VIOLATION: write through intermediate directory symlink succeeded!")
	}

	// Verify sensitive file was NOT modified
	currentSensitive, _ := os.ReadFile(sensitiveFile)
	if !bytes.Equal(currentSensitive, sensitiveOriginal) {
		t.Fatalf("SECURITY VIOLATION: sensitive file was overwritten through intermediate directory symlink!")
	}
}

// 17. Adversarial: Nested symlink access to .git internals.
func TestAdversarialNestedGitSymlinkAccess(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	// Create .git with secret
	gitDir := filepath.Join(sbDir, ".git")
	_ = os.MkdirAll(gitDir, 0o755)
	gitConfig := filepath.Join(gitDir, "config")
	origConfig := []byte("git-config-credential-helper-secret")
	_ = os.WriteFile(gitConfig, origConfig, 0o644)

	// Symlink chain: chain1 -> chain2 -> .git/config
	chain2 := filepath.Join(sbDir, "chain2")
	_ = os.Symlink(".git/config", chain2)
	chain1 := filepath.Join(sbDir, "chain1")
	_ = os.Symlink("chain2", chain1)

	// Directory symlink: git_dir_link -> .git
	gitDirLink := filepath.Join(sbDir, "git_dir_link")
	_ = os.Symlink(".git", gitDirLink)

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Read through chain must be blocked
	if _, err := readFileContent(root, "chain1"); err == nil {
		t.Fatalf("SECURITY VIOLATION: readFileContent read .git/config through symlink chain!")
	}

	// Read through dir link must be blocked
	if _, err := readFileContent(root, "git_dir_link/config"); err == nil {
		t.Fatalf("SECURITY VIOLATION: readFileContent read .git through directory symlink!")
	}

	// Write through chain must be blocked
	if err := saveFileContent(root, "chain1", []byte("evil")); err == nil {
		t.Fatalf("SECURITY VIOLATION: saveFileContent wrote to .git through symlink chain!")
	}

	// Write through dir link must be blocked
	if err := saveFileContent(root, "git_dir_link/hooks/pre-commit", []byte("evil")); err == nil {
		t.Fatalf("SECURITY VIOLATION: saveFileContent wrote to .git through directory symlink!")
	}

	// Verify .git/config remains intact
	cfgData, _ := os.ReadFile(gitConfig)
	if !bytes.Equal(cfgData, origConfig) {
		t.Fatalf(".git/config was modified!")
	}
}

// 18. Adversarial: Directory listing with malicious symlinks.
// Verifies readFileTree does not loop on cyclic symlinks or escape into external dirs.
func TestAdversarialDirectoryListingMaliciousSymlinks(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	extDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(extDir, "ext_secret.txt"), []byte("SECRET"), 0o644)

	// External dir symlink
	_ = os.Symlink(extDir, filepath.Join(sbDir, "link_to_external"))

	// Cyclic directory symlink: cyclic -> .
	_ = os.Symlink(".", filepath.Join(sbDir, "cyclic"))

	// Normal file
	_ = os.WriteFile(filepath.Join(sbDir, "regular.txt"), []byte("REGULAR"), 0o644)

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	tree, err := readFileTree(root)
	if err != nil {
		t.Fatalf("readFileTree failed: %v", err)
	}

	// Verify ext_secret.txt is NOT in the tree
	for _, entry := range tree {
		if strings.Contains(entry.Path, "ext_secret.txt") {
			t.Fatalf("SECURITY VIOLATION: external file appeared in file tree: %s", entry.Path)
		}
	}
}

// 19. Adversarial: Atomic creation of new file colliding with existing symlink.
func TestAdversarialAtomicNewFileCreationSymlinkCollision(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	victimFile := filepath.Join(t.TempDir(), "victim.txt")
	victimData := []byte("VICTIM_INTACT")
	_ = os.WriteFile(victimFile, victimData, 0o644)

	// Pre-existing symlink in workspace
	os.Symlink(victimFile, filepath.Join(sbDir, "new_file.txt"))

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Attempt to create new_file.txt: must fail because destination is a symlink
	err = saveFileContent(root, "new_file.txt", []byte("NEW_CONTENT"))
	if err == nil {
		t.Fatalf("expected saveFileContent on existing symlink to be rejected, but succeeded")
	}

	// Verify victim file is completely untouched
	vData, _ := os.ReadFile(victimFile)
	if !bytes.Equal(vData, victimData) {
		t.Fatalf("SECURITY VIOLATION: victim file was overwritten via atomic create collision!")
	}
}

// 20. Adversarial: Hard link behavior.
func TestAdversarialHardLinkBehavior(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	f1 := filepath.Join(sbDir, "original.txt")
	f1Data := []byte("ORIGINAL_CONTENT")
	_ = os.WriteFile(f1, f1Data, 0o644)

	// Hard link inside workspace
	f2 := filepath.Join(sbDir, "hardlink.txt")
	if err := os.Link(f1, f2); err != nil {
		t.Skipf("hard links not supported: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Reading both works
	r1, err := readFileContent(root, "original.txt")
	if err != nil || !bytes.Equal(r1.Data, f1Data) {
		t.Fatalf("readFileContent original failed: %v", err)
	}
	r2, err := readFileContent(root, "hardlink.txt")
	if err != nil || !bytes.Equal(r2.Data, f1Data) {
		t.Fatalf("readFileContent hardlink failed: %v", err)
	}

	// Updating original.txt works
	updatedData := []byte("UPDATED_CONTENT")
	if err := saveFileContent(root, "original.txt", updatedData); err != nil {
		t.Fatalf("saveFileContent original failed: %v", err)
	}

	// Both remain confined beneath root
	r1Updated, _ := readFileContent(root, "original.txt")
	if !bytes.Equal(r1Updated.Data, updatedData) {
		t.Fatalf("expected updated data")
	}
}

// 21. Special file test: FIFOs must not block or hang write operations.
func TestSpecialFileFIFORejectionNoHang(t *testing.T) {
	workDir, sbDir := setupTestWorkspace(t, "sb-test")

	fifoPath := filepath.Join(sbDir, "pipe.fifo")
	// Create a FIFO (named pipe) in the workspace
	if err := unix.Mkfifo(fifoPath, 0666); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Writing to the FIFO must fail immediately and NOT hang
	errCh := make(chan error, 1)
	go func() {
		errCh <- saveFileContent(root, "pipe.fifo", []byte("FIFO_TEST_PAYLOAD"))
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected write to FIFO to fail, but succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("CRITICAL DOS FAILURE: saveFileContent hung while attempting to open FIFO!")
	}
}

// 22. Hard-link isolation: Writing to a hard-linked file must break the link
// (break-on-write) to ensure external host files sharing the inode are never modified.
func TestExternalHardlinkBreakOnWrite(t *testing.T) {
	tempBase := t.TempDir()

	// External file representing sensitive host file on same filesystem
	extFile := filepath.Join(tempBase, "external_host_file.txt")
	extOriginal := []byte("HOST_SECRET_DATA_DO_NOT_TOUCH")
	if err := os.WriteFile(extFile, extOriginal, 0644); err != nil {
		t.Fatalf("failed to create extFile: %v", err)
	}

	workDir := filepath.Join(tempBase, "work")
	sbDir := filepath.Join(workDir, "sb-test")
	_ = os.MkdirAll(sbDir, 0755)

	// Hard link the external file into the sandbox workspace
	sbFile := filepath.Join(sbDir, "hardlinked_file.txt")
	if err := os.Link(extFile, sbFile); err != nil {
		t.Skipf("hard links across dirs not supported on this filesystem: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-test")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	// Save new content to hardlinked_file.txt in sandbox
	newContent := []byte("SANDBOX_LOCAL_MODIFICATION")
	if err := saveFileContent(root, "hardlinked_file.txt", newContent); err != nil {
		t.Fatalf("saveFileContent failed: %v", err)
	}

	// Verify sandbox file contains newContent
	res, err := readFileContent(root, "hardlinked_file.txt")
	if err != nil || !bytes.Equal(res.Data, newContent) {
		t.Fatalf("sandbox file did not get updated: %v", err)
	}

	// Verify external host file remains completely UNTOUCHED
	currentExt, err := os.ReadFile(extFile)
	if err != nil {
		t.Fatalf("failed to read extFile: %v", err)
	}
	if !bytes.Equal(currentExt, extOriginal) {
		t.Fatalf("CRITICAL SECURITY FAILURE: modifying sandbox hardlink modified external host file! Expected %q, got %q", extOriginal, currentExt)
	}
}

func TestConcurrentHardlinkReplacementAdversarial(t *testing.T) {
	tempBase := t.TempDir()
	extFile := filepath.Join(tempBase, "external_host_secret.txt")
	extOriginal := []byte("HOST_SECRET_CANNOT_BE_OVERWRITTEN")
	if err := os.WriteFile(extFile, extOriginal, 0644); err != nil {
		t.Fatalf("failed to create extFile: %v", err)
	}

	workDir := filepath.Join(tempBase, "work")
	sbDir := filepath.Join(workDir, "sb-hardlink-race")
	_ = os.MkdirAll(sbDir, 0755)

	targetPath := filepath.Join(sbDir, "target.txt")
	if err := os.WriteFile(targetPath, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	root, err := openSandboxRoot(workDir, "sb-hardlink-race")
	if err != nil {
		t.Fatalf("openSandboxRoot failed: %v", err)
	}
	defer root.Close()

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// Attacker goroutine: constantly attempts to replace target.txt with a hard link to extFile
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
				// Attempt to slip in a hard link to extFile
				_ = os.Link(extFile, targetPath)
			}
		}
	}()

	// Victim writer goroutines: attempt to write to target.txt concurrently
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			payload := []byte(fmt.Sprintf("writer-payload-%d", id))
			for j := 0; j < 50; j++ {
				_ = saveFileContent(root, "target.txt", payload)
			}
		}(i)
	}

	// Let writers finish
	done := make(chan struct{})
	go func() {
		// Wait for writers (all except attacker)
		time.Sleep(200 * time.Millisecond)
		close(stopCh)
		wg.Wait()
		close(done)
	}()

	<-done

	// Verify external host file content was NEVER modified or truncated!
	currentExt, err := os.ReadFile(extFile)
	if err != nil {
		t.Fatalf("failed to read extFile: %v", err)
	}
	if !bytes.Equal(currentExt, extOriginal) {
		t.Fatalf("CRITICAL SECURITY FAILURE: concurrent hardlink race corrupted external host file! Expected %q, got %q", extOriginal, currentExt)
	}
}

func TestAdversarialForkSymlinkIsolation(t *testing.T) {
	tempBase := t.TempDir()

	// External sensitive directory
	extDir := filepath.Join(tempBase, "external_secret_dir")
	_ = os.MkdirAll(extDir, 0755)
	_ = os.WriteFile(filepath.Join(extDir, "secret.key"), []byte("SECRET_PRIVATE_KEY"), 0600)

	// External sensitive file
	extFile := filepath.Join(tempBase, "external_file.txt")
	_ = os.WriteFile(extFile, []byte("EXTERNAL_CONFIDENTIAL"), 0644)

	// Parent workspace
	parentDir := filepath.Join(tempBase, "parent_workspace")
	_ = os.MkdirAll(parentDir, 0755)
	_ = os.WriteFile(filepath.Join(parentDir, "app.js"), []byte("console.log('hello');"), 0644)

	// Adversarial symlinks inside parent workspace
	// 1. Symlink to external directory
	_ = os.Symlink(extDir, filepath.Join(parentDir, "ext_dir_link"))
	// 2. Symlink to external file
	_ = os.Symlink(extFile, filepath.Join(parentDir, "ext_file_link"))
	// 3. Safe internal relative symlink
	_ = os.Symlink("app.js", filepath.Join(parentDir, "internal_link.js"))

	// Subdirectories with relative symlinks
	sub1 := filepath.Join(parentDir, "sub1")
	sub2 := filepath.Join(parentDir, "sub2")
	_ = os.MkdirAll(sub1, 0755)
	_ = os.MkdirAll(sub2, 0755)
	_ = os.WriteFile(filepath.Join(sub2, "safe.txt"), []byte("sub2 safe content"), 0644)
	// Escapes workspace root via ../..
	_ = os.Symlink("../../external_file.txt", filepath.Join(sub1, "escape_link.txt"))
	// Stays inside workspace via ..
	_ = os.Symlink("../sub2/safe.txt", filepath.Join(sub1, "internal_rel_link.txt"))

	// Child workspace
	childDir := filepath.Join(tempBase, "child_workspace")

	// Perform copyDir as in fork
	if err := copyDir(parentDir, childDir); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// 1. Verify external directory contents were NOT copied into child
	leakedKey := filepath.Join(childDir, "ext_dir_link", "secret.key")
	if _, err := os.Stat(leakedKey); err == nil {
		t.Fatalf("CRITICAL SECURITY FAILURE: external directory secret was copied into child workspace at %s", leakedKey)
	}

	// 2. Verify external file symlinks were not copied or leaked
	if _, err := os.Lstat(filepath.Join(childDir, "ext_file_link")); err == nil {
		t.Fatalf("CRITICAL SECURITY FAILURE: external file symlink was preserved in child workspace")
	}
	if _, err := os.Lstat(filepath.Join(childDir, "sub1", "escape_link.txt")); err == nil {
		t.Fatalf("CRITICAL SECURITY FAILURE: relative escape symlink was preserved in child workspace")
	}

	// 3. Verify legitimate files were copied
	appJs, err := os.ReadFile(filepath.Join(childDir, "app.js"))
	if err != nil || string(appJs) != "console.log('hello');" {
		t.Fatalf("expected app.js to be copied into child, got err: %v", err)
	}

	// 4. Verify safe internal symlinks were preserved
	linkTarget, err := os.Readlink(filepath.Join(childDir, "internal_link.js"))
	if err != nil || linkTarget != "app.js" {
		t.Fatalf("expected internal_link.js to point to app.js, got target %q, err: %v", linkTarget, err)
	}
	relLinkTarget, err := os.Readlink(filepath.Join(childDir, "sub1", "internal_rel_link.txt"))
	if err != nil || relLinkTarget != "../sub2/safe.txt" {
		t.Fatalf("expected internal_rel_link.txt to point to ../sub2/safe.txt, got target %q, err: %v", relLinkTarget, err)
	}
}

func TestFileOperationsRejectDestroyedAndDeletingSandboxes(t *testing.T) {
	destroyedAt := time.Now()
	testCases := []struct {
		name         string
		sandbox      *db.Sandbox
		shouldReject bool
	}{
		{
			name:         "active sandbox",
			sandbox:      &db.Sandbox{Status: db.StatusRunning},
			shouldReject: false,
		},
		{
			name:         "tombstone with destroyed_at",
			sandbox:      &db.Sandbox{Status: db.StatusRunning, DestroyedAt: &destroyedAt},
			shouldReject: true,
		},
		{
			name:         "status deleted",
			sandbox:      &db.Sandbox{Status: db.StatusDeleted},
			shouldReject: true,
		},
		{
			name:         "status expired",
			sandbox:      &db.Sandbox{Status: db.StatusExpired},
			shouldReject: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rejected := tc.sandbox.DestroyedAt != nil || tc.sandbox.Status == db.StatusDeleted || tc.sandbox.Status == db.StatusExpired
			if rejected != tc.shouldReject {
				t.Fatalf("expected shouldReject=%v, got %v", tc.shouldReject, rejected)
			}
		})
	}
}

// TestAdversarialForkConcurrentSymlinkReplacement tests that concurrent replacement of a
// source file with a symlink to a sensitive external file during copyDir CANNOT leak the
// external file's contents due to descriptor-relative O_NOFOLLOW opens.
func TestAdversarialForkConcurrentSymlinkReplacement(t *testing.T) {
	tempBase := t.TempDir()

	extFile := filepath.Join(tempBase, "external_host_secret.txt")
	extContent := []byte("TOP_SECRET_HOST_DATA_DO_NOT_LEAK")
	if err := os.WriteFile(extFile, extContent, 0600); err != nil {
		t.Fatalf("failed to create extFile: %v", err)
	}

	parentDir := filepath.Join(tempBase, "parent")
	_ = os.MkdirAll(parentDir, 0755)
	targetFile := filepath.Join(parentDir, "app.js")
	legitContent := []byte("console.log('legitimate code');")
	_ = os.WriteFile(targetFile, legitContent, 0644)

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// Attacker goroutine: constantly swaps app.js between a symlink to extFile and a regular file
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
				_ = os.Remove(targetFile)
				_ = os.Symlink(extFile, targetFile)
				_ = os.Remove(targetFile)
				_ = os.WriteFile(targetFile, legitContent, 0644)
			}
		}
	}()

	// Run multiple fork copy attempts while attacker is actively racing
	for i := 0; i < 20; i++ {
		childDir := filepath.Join(tempBase, fmt.Sprintf("child_%d", i))
		_ = copyDir(parentDir, childDir)

		childAppJs := filepath.Join(childDir, "app.js")
		if data, err := os.ReadFile(childAppJs); err == nil {
			if bytes.Equal(data, extContent) {
				close(stopCh)
				wg.Wait()
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: copyDir followed concurrent symlink and copied external secret data!")
			}
		}
	}

	close(stopCh)
	wg.Wait()
}
