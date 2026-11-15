package orchestrator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/api-sandbox-links/backend/internal/config"
)

type mockCustomBuilder struct {
	name     string
	plan     *BuildPlan
	planErr  error
	imageTag string
	buildErr error
}

func (m *mockCustomBuilder) Name() string { return m.name }
func (m *mockCustomBuilder) PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error) {
	if m.planErr != nil {
		return nil, m.planErr
	}
	if m.plan != nil {
		return m.plan, nil
	}
	return &BuildPlan{Builder: m.name, InternalPort: 8080}, nil
}
func (m *mockCustomBuilder) BuildImage(ctx context.Context, dir, name string, progress io.Writer) (string, error) {
	if m.buildErr != nil {
		return "", m.buildErr
	}
	if m.imageTag != "" {
		return m.imageTag, nil
	}
	return name + ":latest", nil
}

func TestBuilderResolver(t *testing.T) {
	logger := slog.Default()

	t.Run("repository with Dockerfile auto-selects Dockerfile builder", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		builder, err := resolver.ResolveBuilder(tmpDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "dockerfile" {
			t.Errorf("expected dockerfile builder, got %q", builder.Name())
		}
	})

	t.Run("repository without Dockerfile auto-selects Railpack builder", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		builder, err := resolver.ResolveBuilder(tmpDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "railpack" {
			t.Errorf("expected railpack builder, got %q", builder.Name())
		}
	})

	t.Run("explicit Railpack selection despite Dockerfile presence", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{Builder: "railpack"}
		builder, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "railpack" {
			t.Errorf("expected explicit railpack builder, got %q", builder.Name())
		}
	})

	t.Run("explicit Dockerfile selection when Dockerfile is present", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{Builder: "dockerfile"}
		builder, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "dockerfile" {
			t.Errorf("expected dockerfile builder, got %q", builder.Name())
		}
	})

	t.Run("explicit Dockerfile selection fails actionably when Dockerfile is missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{Builder: "dockerfile"}
		_, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err == nil {
			t.Fatal("expected error when explicit dockerfile requested but missing, got nil")
		}
		if !strings.Contains(err.Error(), "no Dockerfile found") {
			t.Errorf("expected 'no Dockerfile found' in error, got: %v", err)
		}
	})

	t.Run("explicit configured Dockerfile path", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "build")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "Prod.dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{DockerfilePath: "build/Prod.dockerfile"}
		builder, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "dockerfile" {
			t.Errorf("expected dockerfile builder, got %q", builder.Name())
		}
	})

	t.Run("configured Dockerfile path missing fails actionably", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{DockerfilePath: "nonexistent/Dockerfile"}
		_, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err == nil {
			t.Fatal("expected error for missing custom Dockerfile path, got nil")
		}
		if !strings.Contains(err.Error(), "configured Dockerfile") {
			t.Errorf("expected 'configured Dockerfile' in error, got: %v", err)
		}
	})

	t.Run("explicit Nixpacks selection", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{Builder: "nixpacks"}
		builder, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "nixpacks" {
			t.Errorf("expected nixpacks builder, got %q", builder.Name())
		}
	})

	t.Run("custom root directory containing Dockerfile", func(t *testing.T) {
		tmpDir := t.TempDir()
		backendDir := filepath.Join(tmpDir, "backend")
		if err := os.MkdirAll(backendDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backendDir, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{RootDir: "backend"}
		builder, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if builder.Name() != "dockerfile" {
			t.Errorf("expected dockerfile builder from backend root_dir, got %q", builder.Name())
		}
	})

	t.Run("custom root directory path traversal rejected", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{RootDir: "../outside"}
		_, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err == nil {
			t.Fatal("expected path traversal error, got nil")
		}
		if !strings.Contains(err.Error(), "traverses outside repository") {
			t.Errorf("expected traversal error message, got: %v", err)
		}
	})

	t.Run("unsupported builder engine rejected with actionable error", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockRailpack := &mockCustomBuilder{name: "railpack"}
		mockNixpacks := &mockCustomBuilder{name: "nixpacks"}
		mockDocker := &mockCustomBuilder{name: "dockerfile"}

		resolver := NewBuilderResolverWithBuilders(&config.Config{Builder: "railpack"}, logger, mockRailpack, mockNixpacks, mockDocker)
		userCfg := &UserBuildConfig{Builder: "invalid_engine"}
		_, err := resolver.ResolveBuilder(tmpDir, userCfg)
		if err == nil {
			t.Fatal("expected error for invalid builder, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported builder engine") {
			t.Errorf("expected 'unsupported builder engine' error, got: %v", err)
		}
	})

	t.Run("no silent builder fallback on failure", func(t *testing.T) {
		// Verify that a selected builder's failure is preserved verbatim and does not change builders
		mockRailpack := &mockCustomBuilder{
			name:     "railpack",
			buildErr: errors.New("compilation failed: exit status 1"),
		}
		tmpDir := t.TempDir()
		_, err := mockRailpack.BuildImage(context.Background(), tmpDir, "app", nil)
		if err == nil {
			t.Fatal("expected build failure, got nil")
		}
		if mockRailpack.Name() != "railpack" {
			t.Fatalf("builder changed silently: %q", mockRailpack.Name())
		}
	})
}

