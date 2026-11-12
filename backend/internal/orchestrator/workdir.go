package orchestrator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// cleanupWorkDirIfDestroyed removes the sandbox's working directory if and
// only if the row is already a tombstone (destroyed or expired). It returns
// whether a cleanup happened.
//
// This is the deploy-side half of the destroy/build race: destroy removes the
// directory, but a deploy that was already past its own tombstone check can
// re-create it (git clone, nixpacks). Every deploy failure/finish path calls
// this so a "deleted" sandbox converges back to having no files, while a LIVE
// sandbox is never touched — its directory may hold user edits, and it is
// reclaimed by the expiry reaper at expires_at instead.
func (d *Deployer) cleanupWorkDirIfDestroyed(sandboxID, subdomain string) bool {
	if d == nil || d.store == nil {
		return false
	}
	fresh, err := d.store.GetSandbox(sandboxID)
	if err != nil || fresh.DestroyedAt == nil {
		// Not destroyed (or row unreadable): never delete on uncertainty.
		return false
	}
	if err := RemoveSandboxWorkDir(d.cfg.SandboxWorkDir, subdomain); err != nil {
		d.orch.logger.Warn("deploy: removing work dir of destroyed sandbox",
			"sandbox", sandboxID, "err", err.Error())
		return false
	}
	return true
}

// RemoveSandboxWorkDir deletes the working directory <root>/<subdomain> after
// validating that the result stays strictly inside root.
//
// It is the ONE place that ever unlinks a sandbox work directory. Every deleter
// goes through it — the manual destroy handler, the expiry reaper, and the
// deploy-path race cleanups — so the path-safety rules cannot drift apart:
//
//   - empty/blank subdomain is refused (a naive filepath.Join(root, "") would
//     resolve to the work ROOT itself and delete every sandbox's files),
//   - separators, backslashes and ".." are refused (escape attempts),
//   - the cleaned target must be a strict child of the cleaned root,
//   - the root itself is never removed,
//   - a missing directory is success (cleanup paths are idempotent; repeated
//     cancellation/destroy must not turn into an error).
func RemoveSandboxWorkDir(workRoot, subdomain string) error {
	if strings.TrimSpace(subdomain) == "" {
		return errors.New("empty subdomain")
	}
	if strings.Contains(subdomain, "/") || strings.Contains(subdomain, "\\") || strings.Contains(subdomain, "..") {
		return errors.New("unsafe subdomain")
	}
	root := filepath.Clean(workRoot)
	target := filepath.Clean(filepath.Join(root, subdomain))
	// Ensure target is under root.
	if target != root && !strings.HasPrefix(target+string(os.PathSeparator), root+string(os.PathSeparator)) {
		return errors.New("path outside work root")
	}
	if target == root {
		return errors.New("refusing to remove work root")
	}
	if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
