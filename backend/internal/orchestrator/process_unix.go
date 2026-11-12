//go:build unix

package orchestrator

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the child in its own process group so a build that
// spawns helpers can be killed as a unit.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the child and everything it spawned. Killing only the
// nixpacks process leaves its docker/buildx/nix children running and holding
// the build slot and disk cache.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
