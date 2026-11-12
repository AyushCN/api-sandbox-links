package orchestrator

import (
	"strings"
	"testing"
)

// TestNixpacksEnvAllowlist checks the pure policy function: only explicit
// non-secret version settings pass; everything else is dropped.
func TestNixpacksEnvAllowlist(t *testing.T) {
	t.Setenv("SOME_APP_SECRET_TOKEN", "nope")
	t.Setenv("MY_CUSTOM_CONFIG", "nope")
	t.Setenv("NIXPACKS_PRIVATE_PACKAGES", "credential-bearing-config")
	t.Setenv("NIXPACKS_NODE_VERSION", "20")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	privateHome := t.TempDir()

	joined := strings.Join(nixpacksEnv(privateHome), "\n")
	for _, forbidden := range []string{
		"SOME_APP_SECRET_TOKEN=", "MY_CUSTOM_CONFIG=", "NIXPACKS_PRIVATE_PACKAGES=", "DOCKER_HOST=",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("nixpacksEnv passed forbidden entry %q", forbidden)
		}
	}
	for _, want := range []string{"NIXPACKS_NODE_VERSION=20", "HOME=" + privateHome, "PATH="} {
		if !strings.Contains(joined, want) {
			t.Errorf("nixpacksEnv dropped required entry %q", want)
		}
	}
}
