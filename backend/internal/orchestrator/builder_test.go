package orchestrator

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/api-sandbox-links/backend/internal/config"
)

func TestNewBuilderFactory(t *testing.T) {
	logger := slog.Default()

	t.Run("default builder is nixpacks", func(t *testing.T) {
		cfg := &config.Config{
			Builder:     "",
			NixpacksBin: "nixpacks",
		}
		b, err := NewBuilder(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error creating default builder: %v", err)
		}
		if b.Name() != "nixpacks" {
			t.Errorf("expected builder name 'nixpacks', got %q", b.Name())
		}
		if _, ok := b.(*NixpacksBuilder); !ok {
			t.Errorf("expected *NixpacksBuilder, got %T", b)
		}
	})

	t.Run("explicit nixpacks builder", func(t *testing.T) {
		cfg := &config.Config{
			Builder:     "nixpacks",
			NixpacksBin: "/usr/local/bin/nixpacks",
		}
		b, err := NewBuilder(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error creating nixpacks builder: %v", err)
		}
		if b.Name() != "nixpacks" {
			t.Errorf("expected builder name 'nixpacks', got %q", b.Name())
		}
	})

	t.Run("railpack builder initialization", func(t *testing.T) {
		cfg := &config.Config{
			Builder:      "railpack",
			RailpackBin:  "railpack",
			BuildKitHost: "unix:///run/buildkit/buildkitd.sock",
		}
		b, err := NewBuilder(cfg, logger)
		if err != nil {
			t.Fatalf("unexpected error creating railpack builder: %v", err)
		}
		if b.Name() != "railpack" {
			t.Errorf("expected builder name 'railpack', got %q", b.Name())
		}
		rb, ok := b.(*RailpackBuilder)
		if !ok {
			t.Fatalf("expected *RailpackBuilder, got %T", b)
		}
		if rb.buildkitHost != "unix:///run/buildkit/buildkitd.sock" {
			t.Errorf("expected buildkit host unix:///run/buildkit/buildkitd.sock, got %q", rb.buildkitHost)
		}
	})

	t.Run("unsupported builder", func(t *testing.T) {
		cfg := &config.Config{
			Builder: "dockerfile",
		}
		if _, err := NewBuilder(cfg, logger); err == nil {
			t.Error("expected error for unsupported builder engine")
		}
	})

	t.Run("D-01: nixpacks selection emits structured deprecation warning", func(t *testing.T) {
		var logBuf bytes.Buffer
		captureLogger := slog.New(slog.NewTextHandler(&logBuf, nil))
		cfg := &config.Config{Builder: "nixpacks", NixpacksBin: "nixpacks"}
		_, err := NewBuilder(cfg, captureLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(logBuf.String(), "nixpacks is deprecated") {
			t.Errorf("expected deprecation warning in log, got: %s", logBuf.String())
		}
	})

	t.Run("D-02: railpack selection does not emit deprecation warning", func(t *testing.T) {
		var logBuf bytes.Buffer
		captureLogger := slog.New(slog.NewTextHandler(&logBuf, nil))
		cfg := &config.Config{Builder: "railpack", RailpackBin: "railpack"}
		_, err := NewBuilder(cfg, captureLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(logBuf.String(), "deprecated") {
			t.Errorf("unexpected deprecation warning for railpack in log: %s", logBuf.String())
		}
	})
}

func TestProviderDefaultPort(t *testing.T) {
	tests := []struct {
		provider string
		expected int
	}{
		{"node", 3000},
		{"nodejs", 3000},
		{"python", 8080},
		{"python3", 8080},
		{"flask", 8080},
		{"django", 8080},
		{"fastapi", 8080},
		{"go", 8080},
		{"golang", 8080},
		{"ruby", 4567},
		{"php", 8000},
		{"laravel", 8000},
		{"rust", 8000},
		{"cargo", 8000},
		{"static", 80},
		{"unknown", 3000},
		{"", 3000},
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			got := ProviderDefaultPort(tc.provider)
			if got != tc.expected {
				t.Errorf("ProviderDefaultPort(%q) = %d; want %d", tc.provider, got, tc.expected)
			}
		})
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		input   string
		port    int
		isValid bool
	}{
		{"3000", 3000, true},
		{"80", 80, true},
		{"65535", 65535, true},
		{"0", 0, false},
		{"-8080", 0, false},
		{"65536", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			port, ok := parsePort(tc.input)
			if ok != tc.isValid {
				t.Errorf("parsePort(%q) valid = %v; want %v", tc.input, ok, tc.isValid)
			}
			if ok && port != tc.port {
				t.Errorf("parsePort(%q) = %d; want %d", tc.input, port, tc.port)
			}
		})
	}
}

func TestDetectPort(t *testing.T) {
	t.Run("explicit PORT variable wins", func(t *testing.T) {
		vars := map[string]string{
			"PORT":          "9000",
			"NIXPACKS_PORT": "5000",
		}
		port := detectPort("node", vars)
		if port != 9000 {
			t.Errorf("expected port 9000, got %d", port)
		}
	})

	t.Run("explicit NIXPACKS_PORT when PORT is empty", func(t *testing.T) {
		vars := map[string]string{
			"NIXPACKS_PORT": "5000",
		}
		port := detectPort("node", vars)
		if port != 5000 {
			t.Errorf("expected port 5000, got %d", port)
		}
	})

	t.Run("fallback to provider default when no port variables provided", func(t *testing.T) {
		port := detectPort("python", nil)
		if port != 8080 {
			t.Errorf("expected provider default port 8080, got %d", port)
		}
	})
}

func TestNixpacksEnvFiltering(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("SECRETS_ENCRYPTION_KEY", "super-secret")
	t.Setenv("NIXPACKS_NODE_VERSION", "22")

	env := nixpacksEnv("/home/testuser")

	var hasHome, hasPath, hasNodeVer, hasDockerHost, hasSecret bool
	for _, kv := range env {
		switch {
		case kv == "HOME=/home/testuser":
			hasHome = true
		case strings.HasPrefix(kv, "PATH="):
			hasPath = true
		case kv == "NIXPACKS_NODE_VERSION=22":
			hasNodeVer = true
		case strings.HasPrefix(kv, "DOCKER_HOST="):
			hasDockerHost = true
		case strings.HasPrefix(kv, "SECRETS_ENCRYPTION_KEY="):
			hasSecret = true
		}
	}

	if !hasHome {
		t.Error("expected nixpacksEnv to include HOME=/home/testuser")
	}
	if !hasPath {
		t.Error("expected nixpacksEnv to include PATH")
	}
	if !hasNodeVer {
		t.Error("expected nixpacksEnv to include NIXPACKS_NODE_VERSION")
	}
	if hasDockerHost {
		t.Error("expected nixpacksEnv to filter out DOCKER_HOST")
	}
	if hasSecret {
		t.Error("expected nixpacksEnv to filter out SECRETS_ENCRYPTION_KEY")
	}
}

// mockBuilder records calls and returns canned responses.
type mockBuilder struct {
	name        string
	plan        *BuildPlan
	planErr     error
	imageTag    string
	buildErr    error
	planCalled  bool
	buildCalled bool
}

func (m *mockBuilder) Name() string { return m.name }

func (m *mockBuilder) PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error) {
	m.planCalled = true
	return m.plan, m.planErr
}

func (m *mockBuilder) BuildImage(ctx context.Context, dir, name string, progress io.Writer) (string, error) {
	m.buildCalled = true
	if progress != nil {
		_, _ = progress.Write([]byte("build output"))
	}
	return m.imageTag, m.buildErr
}

func TestOrchestratorBuilderDelegation(t *testing.T) {
	orch := &Orchestrator{}

	t.Run("error when builder not configured", func(t *testing.T) {
		if _, err := orch.PlanAnalyze(context.Background(), "/tmp"); err == nil {
			t.Error("expected error when no builder is configured")
		}
		if _, err := orch.BuildImage(context.Background(), "/tmp", "test", nil); err == nil {
			t.Error("expected error when no builder is configured")
		}
	})

	t.Run("delegates to configured builder", func(t *testing.T) {
		expectedPlan := &BuildPlan{
			Builder:          "mock",
			Provider:         "golang",
			StartCmd:         "./main",
			InternalPort:     8080,
			DetectedLanguage: "go",
		}
		mock := &mockBuilder{
			name:     "mock",
			plan:     expectedPlan,
			imageTag: "mock-image:latest",
		}

		orch.SetBuilder(mock)
		if orch.Builder().Name() != "mock" {
			t.Errorf("expected builder name 'mock', got %q", orch.Builder().Name())
		}

		ctx := context.Background()
		plan, err := orch.PlanAnalyze(ctx, "/tmp/dir")
		if err != nil {
			t.Fatalf("unexpected error during PlanAnalyze: %v", err)
		}
		if !mock.planCalled {
			t.Error("expected PlanAnalyze to be called on builder")
		}
		if plan.InternalPort != 8080 || plan.Provider != "golang" {
			t.Errorf("unexpected plan returned: %+v", plan)
		}

		var buf bytes.Buffer
		tag, err := orch.BuildImage(ctx, "/tmp/dir", "test-img", &buf)
		if err != nil {
			t.Fatalf("unexpected error during BuildImage: %v", err)
		}
		if !mock.buildCalled {
			t.Error("expected BuildImage to be called on builder")
		}
		if tag != "mock-image:latest" {
			t.Errorf("expected image tag 'mock-image:latest', got %q", tag)
		}
		if buf.String() != "build output" {
			t.Errorf("expected buffer to capture progress output, got %q", buf.String())
		}
	})
}

func TestRailpackEnvFiltering(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("SECRETS_ENCRYPTION_KEY", "super-secret")
	t.Setenv("RAILPACK_NODE_VERSION", "22")
	t.Setenv("MISE_DATA_DIR", "/tmp/mise")

	env := railpackEnv("/home/testuser", "tcp://127.0.0.1:1234")

	var hasHome, hasPath, hasNodeVer, hasMise, hasBuildKit, hasDockerHost, hasSecret bool
	for _, kv := range env {
		switch {
		case kv == "HOME=/home/testuser":
			hasHome = true
		case strings.HasPrefix(kv, "PATH="):
			hasPath = true
		case kv == "RAILPACK_NODE_VERSION=22":
			hasNodeVer = true
		case kv == "MISE_DATA_DIR=/tmp/mise":
			hasMise = true
		case kv == "BUILDKIT_HOST=tcp://127.0.0.1:1234":
			hasBuildKit = true
		case strings.HasPrefix(kv, "DOCKER_HOST="):
			hasDockerHost = true
		case strings.HasPrefix(kv, "SECRETS_ENCRYPTION_KEY="):
			hasSecret = true
		}
	}

	if !hasHome {
		t.Error("expected railpackEnv to include HOME")
	}
	if !hasPath {
		t.Error("expected railpackEnv to include PATH")
	}
	if !hasNodeVer {
		t.Error("expected railpackEnv to include RAILPACK_NODE_VERSION")
	}
	if !hasMise {
		t.Error("expected railpackEnv to include MISE_DATA_DIR")
	}
	if !hasBuildKit {
		t.Error("expected railpackEnv to include BUILDKIT_HOST")
	}
	if hasDockerHost {
		t.Error("expected railpackEnv to filter out DOCKER_HOST")
	}
	if hasSecret {
		t.Error("expected railpackEnv to filter out SECRETS_ENCRYPTION_KEY")
	}
}

func TestParseRailpackPlanGoldenFixtures(t *testing.T) {
	t.Run("nodejs plan with deploy startCommand and variables", func(t *testing.T) {
		fixture := []byte(`{
			"providers": ["node"],
			"deploy": {
				"startCommand": "node server.js"
			},
			"variables": {
				"PORT": "3000",
				"NODE_ENV": "production"
			}
		}`)

		plan, err := parseRailpackPlan(fixture)
		if err != nil {
			t.Fatalf("unexpected error parsing nodejs plan: %v", err)
		}
		if plan.Builder != "railpack" {
			t.Errorf("expected builder railpack, got %q", plan.Builder)
		}
		if plan.Provider != "node" {
			t.Errorf("expected provider node, got %q", plan.Provider)
		}
		if plan.StartCmd != "node server.js" {
			t.Errorf("expected startCmd 'node server.js', got %q", plan.StartCmd)
		}
		if plan.InternalPort != 3000 {
			t.Errorf("expected port 3000, got %d", plan.InternalPort)
		}
	})

	t.Run("python fastapi plan with provider string and fallback port", func(t *testing.T) {
		fixture := []byte(`{
			"provider": "python",
			"deploy": {
				"start_cmd": "uvicorn main:app --host 0.0.0.0 --port 8080"
			}
		}`)

		plan, err := parseRailpackPlan(fixture)
		if err != nil {
			t.Fatalf("unexpected error parsing python plan: %v", err)
		}
		if plan.Provider != "python" {
			t.Errorf("expected provider python, got %q", plan.Provider)
		}
		if plan.StartCmd != "uvicorn main:app --host 0.0.0.0 --port 8080" {
			t.Errorf("expected startCmd from start_cmd, got %q", plan.StartCmd)
		}
		if plan.InternalPort != 8080 {
			t.Errorf("expected default python port 8080, got %d", plan.InternalPort)
		}
	})

	t.Run("go plan with languages array and start.cmd", func(t *testing.T) {
		fixture := []byte(`{
			"languages": ["go"],
			"start": {
				"cmd": "./bin/server"
			},
			"env": {
				"PORT": "8080"
			}
		}`)

		plan, err := parseRailpackPlan(fixture)
		if err != nil {
			t.Fatalf("unexpected error parsing go plan: %v", err)
		}
		if plan.Provider != "go" {
			t.Errorf("expected provider go, got %q", plan.Provider)
		}
		if plan.StartCmd != "./bin/server" {
			t.Errorf("expected startCmd './bin/server', got %q", plan.StartCmd)
		}
		if plan.InternalPort != 8080 {
			t.Errorf("expected port 8080, got %d", plan.InternalPort)
		}
	})

	t.Run("explicit RAILPACK_PORT override", func(t *testing.T) {
		fixture := []byte(`{
			"provider": "ruby",
			"deploy": {
				"startCommand": "bundle exec puma"
			},
			"variables": {
				"RAILPACK_PORT": "9292"
			}
		}`)

		plan, err := parseRailpackPlan(fixture)
		if err != nil {
			t.Fatalf("unexpected error parsing ruby plan: %v", err)
		}
		if plan.InternalPort != 9292 {
			t.Errorf("expected port 9292 from RAILPACK_PORT, got %d", plan.InternalPort)
		}
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		if _, err := parseRailpackPlan([]byte(`{not-json}`)); err == nil {
			t.Error("expected error for invalid json")
		}
	})
}

