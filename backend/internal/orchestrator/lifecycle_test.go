package orchestrator

import (
	"testing"

	"github.com/api-sandbox-links/backend/internal/config"
)

func TestSandboxHostConfigIsolationDefaults(t *testing.T) {
	cfg := &config.Config{
		SandboxInit:     true,
		SandboxHardened: false,
		SandboxRuntime:  "",
	}

	hc := sandboxHostConfig(cfg)
	if hc.Init == nil || !*hc.Init {
		t.Error("expected hc.Init to be true by default")
	}
	if hc.ReadonlyRootfs {
		t.Error("expected hc.ReadonlyRootfs to be false by default")
	}
	if len(hc.Tmpfs) != 0 {
		t.Errorf("expected no tmpfs mounts by default, got %v", hc.Tmpfs)
	}
	if hc.Runtime != "" {
		t.Errorf("expected empty Runtime by default, got %q", hc.Runtime)
	}
	if len(hc.CapDrop) == 0 || hc.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop ALL, got %v", hc.CapDrop)
	}
}

func TestSandboxHostConfigHardened(t *testing.T) {
	cfg := &config.Config{
		SandboxInit:     true,
		SandboxHardened: true,
		SandboxRuntime:  "runsc",
	}

	hc := sandboxHostConfig(cfg)
	if hc.Init == nil || !*hc.Init {
		t.Error("expected hc.Init to be true")
	}
	if !hc.ReadonlyRootfs {
		t.Error("expected hc.ReadonlyRootfs to be true under SandboxHardened")
	}
	if hc.Tmpfs["/tmp"] == "" || hc.Tmpfs["/run"] == "" {
		t.Errorf("expected /tmp and /run tmpfs mounts, got %v", hc.Tmpfs)
	}
	if hc.Runtime != "runsc" {
		t.Errorf("expected Runtime 'runsc', got %q", hc.Runtime)
	}
}

func TestSandboxHostConfigDisabledInit(t *testing.T) {
	cfg := &config.Config{
		SandboxInit: false,
	}

	hc := sandboxHostConfig(cfg)
	if hc.Init == nil || *hc.Init {
		t.Error("expected hc.Init to be false when SandboxInit is false")
	}
}

func TestSidecarHostConfigDefaults(t *testing.T) {
	hcPostgres := sidecarHostConfig("vol_pg", "postgres")
	if hcPostgres.Init == nil || !*hcPostgres.Init {
		t.Error("expected sidecar postgres Init to be true")
	}
	if len(hcPostgres.Binds) != 1 || hcPostgres.Binds[0] != "vol_pg" {
		t.Errorf("expected bind vol_pg, got %v", hcPostgres.Binds)
	}

	hcMySQL := sidecarHostConfig("vol_my", "mysql")
	if hcMySQL.Init == nil || !*hcMySQL.Init {
		t.Error("expected sidecar mysql Init to be true")
	}
	if len(hcMySQL.Binds) != 1 || hcMySQL.Binds[0] != "vol_my:/var/lib/mysql" {
		t.Errorf("expected bind vol_my:/var/lib/mysql, got %v", hcMySQL.Binds)
	}
}

