package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildConfigResolver(t *testing.T) {
	resolver := NewBuildConfigResolver(nil, nil)

	t.Run("Dockerfile preserves CMD and ENTRYPOINT when no user override", func(t *testing.T) {
		tmpDir := t.TempDir()
		df := `FROM python:3.11-slim
EXPOSE 5000
CMD ["gunicorn", "--bind", "0.0.0.0:5000", "back:app"]
`
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(df), 0o644); err != nil {
			t.Fatal(err)
		}

		builder := NewDockerfileBuilder("", nil)
		resolved, err := resolver.Resolve(context.Background(), builder, tmpDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resolved.PreserveCmd {
			t.Error("expected PreserveCmd=true for Dockerfile without user override")
		}
		if resolved.Port != 5000 {
			t.Errorf("expected port 5000, got %d", resolved.Port)
		}
		if resolved.StartCmd != "gunicorn --bind 0.0.0.0:5000 back:app" {
			t.Errorf("expected StartCmd from Dockerfile, got %q", resolved.StartCmd)
		}
		if resolved.Language != "python" {
			t.Errorf("expected language python, got %q", resolved.Language)
		}
	})

	t.Run("User start command overrides Dockerfile CMD", func(t *testing.T) {
		tmpDir := t.TempDir()
		df := `FROM python:3.11-slim
EXPOSE 5000
CMD ["python", "app.py"]
`
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(df), 0o644); err != nil {
			t.Fatal(err)
		}

		builder := NewDockerfileBuilder("", nil)
		userCfg := &UserBuildConfig{
			StartCmd: "python custom_entrypoint.py",
			Port:     9000,
		}
		resolved, err := resolver.Resolve(context.Background(), builder, tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.PreserveCmd {
			t.Error("expected PreserveCmd=false when user provides explicit StartCmd")
		}
		if resolved.StartCmd != "python custom_entrypoint.py" {
			t.Errorf("expected explicit start command, got %q", resolved.StartCmd)
		}
		if resolved.Port != 9000 {
			t.Errorf("expected port 9000 from user override, got %d", resolved.Port)
		}
	})

	t.Run("railpack.json startCommand is detected for Railpack", func(t *testing.T) {
		tmpDir := t.TempDir()
		rj := `{"deploy":{"startCommand":"uvicorn src.server:app --port 8080"}}`
		if err := os.WriteFile(filepath.Join(tmpDir, "railpack.json"), []byte(rj), 0o644); err != nil {
			t.Fatal(err)
		}

		mockBuilder := &mockCustomBuilder{
			name: "railpack",
			plan: &BuildPlan{Builder: "railpack", InternalPort: 8080},
		}

		resolved, err := resolver.Resolve(context.Background(), mockBuilder, tmpDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.StartCmd != "uvicorn src.server:app --port 8080" {
			t.Errorf("expected start command from railpack.json, got %q", resolved.StartCmd)
		}
	})

	t.Run("Procfile web command is detected as fallback start command", func(t *testing.T) {
		tmpDir := t.TempDir()
		proc := "web: bundle exec rails server -b 0.0.0.0\nworker: bundle exec sidekiq\n"
		if err := os.WriteFile(filepath.Join(tmpDir, "Procfile"), []byte(proc), 0o644); err != nil {
			t.Fatal(err)
		}

		mockBuilder := &mockCustomBuilder{
			name: "railpack",
			plan: &BuildPlan{Builder: "railpack", InternalPort: 3000},
		}

		resolved, err := resolver.Resolve(context.Background(), mockBuilder, tmpDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.StartCmd != "bundle exec rails server -b 0.0.0.0" {
			t.Errorf("expected start command from Procfile, got %q", resolved.StartCmd)
		}
	})

	t.Run("Custom root directory is normalized and resolved", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "src", "backend")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		df := "FROM node:20\nEXPOSE 4000\nCMD [\"node\", \"server.js\"]\n"
		if err := os.WriteFile(filepath.Join(srcDir, "Dockerfile"), []byte(df), 0o644); err != nil {
			t.Fatal(err)
		}

		builder := NewDockerfileBuilder("", nil)
		userCfg := &UserBuildConfig{
			RootDir: "src/backend",
		}
		resolved, err := resolver.Resolve(context.Background(), builder, tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.RootDir != srcDir {
			t.Errorf("expected RootDir %q, got %q", srcDir, resolved.RootDir)
		}
		if resolved.RelativeRootDir != filepath.Clean("src/backend") {
			t.Errorf("expected RelativeRootDir 'src/backend', got %q", resolved.RelativeRootDir)
		}
		if resolved.Port != 4000 {
			t.Errorf("expected port 4000, got %d", resolved.Port)
		}
	})

	t.Run("Environment variables are merged from plan and user config", func(t *testing.T) {
		tmpDir := t.TempDir()
		df := "FROM alpine\nENV DEFAULT_VAR=base\nEXPOSE 8080\n"
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(df), 0o644); err != nil {
			t.Fatal(err)
		}

		builder := NewDockerfileBuilder("", nil)
		userCfg := &UserBuildConfig{
			Env: map[string]string{
				"CUSTOM_VAR":  "override",
				"DEFAULT_VAR": "new_val",
			},
		}
		resolved, err := resolver.Resolve(context.Background(), builder, tmpDir, userCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.Variables["DEFAULT_VAR"] != "new_val" {
			t.Errorf("expected user env override DEFAULT_VAR=new_val, got %q", resolved.Variables["DEFAULT_VAR"])
		}
		if resolved.Variables["CUSTOM_VAR"] != "override" {
			t.Errorf("expected CUSTOM_VAR=override, got %q", resolved.Variables["CUSTOM_VAR"])
		}
	})
}

