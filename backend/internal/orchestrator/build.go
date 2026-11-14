package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildPlan is the parsed, useful subset of `nixpacks plan` output. The
// platform needs exactly three things from it: the detected language family,
// the port the app listens on, and the start command.
type BuildPlan struct {
	Provider         string `json:"provider"`      // e.g. "node", "python", "go"
	StartCmd         string `json:"start_cmd"`     // full command nixpacks would run
	InternalPort     int    `json:"internal_port"` // resolved app port
	DetectedLanguage string `json:"detected_language"`
}

// planPayload mirrors just the fields of the nixpacks plan we consume. The
// full JSON is much larger; unmarshalling loosely keeps this robust to
// nixpacks version drift.
type planPayload struct {
	Providers []string          `json:"providers"`
	Variables map[string]string `json:"variables"`
	Start     struct {
		Cmd string `json:"cmd"`
	} `json:"start"`
}

// nixpacksEnv is a deny-by-default child environment for the legacy local
// development executor. It excludes platform secrets, all DOCKER_* values,
// proxy variables, and arbitrary NIXPACKS_* values (some may be credentials).
// HOME is passed through as the parent's real home, NOT reset to a throwaway
// directory: the Docker CLI resolves buildx from $HOME/.docker/cli-plugins,
// so replacing HOME makes a perfectly valid host install look like "buildx is
// not installed" and breaks every build. The allowlist still keeps platform
// secrets and DOCKER_* out of the child; HOME only exposes the same user's
// tool configuration, which the same-UID nixpacks process could read directly
// anyway. This reduces accidental inheritance but is not isolation.
func nixpacksEnv(home string) []string {
	return nixpacksEnvWith(home, nil)
}

// nixpacksEnvWith adds explicit, non-secret build settings chosen from the
// preflight result. Only these NIXPACKS_* keys are ever forwarded; arbitrary
// NIXPACKS_* values from the parent (which may contain private registry
// credentials) stay excluded.
func nixpacksEnvWith(home string, extra map[string]string) []string {
	allowed := map[string]bool{
		"PATH": true, "USER": true, "LOGNAME": true,
		"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TZ": true,
		"TMPDIR": true, "TMP": true, "TEMP": true,
		"NIXPACKS_NODE_VERSION": true, "NIXPACKS_PYTHON_VERSION": true,
		"NIXPACKS_GO_VERSION": true, "NIXPACKS_RUST_VERSION": true,
		"NIXPACKS_JAVA_VERSION": true, "NIXPACKS_START_CMD": true,
		"NIXPACKS_MAVEN_ARGS": true,
	}
	env := os.Environ()
	out := make([]string, 0, len(env)+len(extra)+1)
	seen := map[string]bool{}
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if ok && allowed[key] {
			if _, override := extra[key]; override {
				continue
			}
			out = append(out, kv)
			seen[key] = true
		}
	}
	for k, v := range extra {
		if v == "" || seen[k] {
			continue
		}
		out = append(out, k+"="+v)
	}
	return append(out, "HOME="+home)
}

// runProcessGroup starts cmd in its own process group and waits for it. On
// context cancellation it kills the whole group, not just the direct child.
func runProcessGroup(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-done
		return ctx.Err()
	}
}

// PlanAnalyzes runs `nixpacks plan` in dir and returns the resolved build
// plan. The port may be absent from the plan; detectPort then falls back to
// sensible defaults derived from the provider.
func (o *Orchestrator) PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error) {
	cmd := exec.Command(o.cfg.NixpacksBin, "plan", dir)
	cmd.Env = nixpacksEnv(os.Getenv("HOME"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runProcessGroup(ctx, cmd); err != nil {
		return nil, fmt.Errorf("orchestrator: nixpacks plan failed: %w: %s", err, stderr.String())
	}

	var p planPayload
	if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
		return nil, fmt.Errorf("orchestrator: parsing nixpacks plan output: %w", err)
	}

	provider := ""
	if len(p.Providers) > 0 {
		provider = p.Providers[0]
	} else if meta := strings.TrimSpace(p.Variables["NIXPACKS_METADATA"]); meta != "" {
		// nixpacks 1.41 leaves "providers" empty and reports the detected
		// stack in NIXPACKS_METADATA instead. Fall back to it so the language
		// and provider-specific port defaults are not lost.
		if first, _, ok := strings.Cut(meta, ","); ok {
			meta = first
		}
		provider = strings.TrimSpace(meta)
	}

	bp := &BuildPlan{
		Provider:         provider,
		StartCmd:         p.Start.Cmd,
		DetectedLanguage: provider,
		InternalPort:     detectPort(provider, p.Variables),
	}
	return bp, nil
}

// BuildOptions carries the preflight decisions that must influence the image.
type BuildOptions struct {
	StartCommand    string
	Language        string
	Framework       string
	LanguageVersion string
	Builder         string
}

// BuildImage runs the appropriate builder for the directory and returns the
// image tag it produced. A Dockerfile (case-insensitive, repository root) wins
// over nixpacks; this priority is deliberate and documented in docs/SETUP.md:
// a repository that ships a Dockerfile has already made its build/run decisions
// and the platform should honour them.
func (o *Orchestrator) BuildImage(ctx context.Context, dir, name string, progress *bytes.Buffer, opts ...BuildOptions) (string, error) {
	var opt BuildOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if dockerfile := findDockerfile(dir); dockerfile != "" {
		args := []string{"build", "-t", name + ":latest", "-f", dockerfile, dir}
		cmd := exec.Command("docker", args...)
		cmd.Env = dockerEnv(o.cfg.DockerHost)
		if progress != nil {
			cmd.Stdout = progress
			cmd.Stderr = progress
		}
		if err := runProcessGroup(ctx, cmd); err != nil {
			return "", fmt.Errorf("orchestrator: docker build failed: %w", err)
		}
		return name + ":latest", nil
	}

	extra := map[string]string{}
	if opt.StartCommand != "" {
		extra["NIXPACKS_START_CMD"] = opt.StartCommand
	}
	switch opt.Language {
	case "go":
		if opt.LanguageVersion != "" {
			extra["NIXPACKS_GO_VERSION"] = opt.LanguageVersion
		}
	case "node":
		if opt.LanguageVersion != "" {
			extra["NIXPACKS_NODE_VERSION"] = opt.LanguageVersion
		}
	case "python":
		if opt.LanguageVersion != "" {
			extra["NIXPACKS_PYTHON_VERSION"] = opt.LanguageVersion
		}
	case "java":
		if opt.LanguageVersion != "" {
			extra["NIXPACKS_JAVA_VERSION"] = opt.LanguageVersion
		}
		// Integration projects are slow to test; the deploy flow builds the
		// app, it does not run the repository's test suite.
		extra["NIXPACKS_MAVEN_ARGS"] = "-DskipTests"
	}
	cmd := exec.Command(o.cfg.NixpacksBin, "build", "--name", name, dir)
	cmd.Env = nixpacksEnvWith(os.Getenv("HOME"), extra)
	if progress != nil {
		cmd.Stdout = progress
		cmd.Stderr = progress
	}
	if err := runProcessGroup(ctx, cmd); err != nil {
		// The image name generated by nixpacks is "name:latest" on success.
		return "", fmt.Errorf("orchestrator: nixpacks build failed: %w", err)
	}
	return name + ":latest", nil
}

func findDockerfile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "Dockerfile") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// dockerEnv keeps platform secrets out of the Docker CLI while retaining the
// daemon endpoint and the user's Docker config.
func dockerEnv(host string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "DOCKER_CONFIG": true}
	var out []string
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && allowed[key] {
			out = append(out, kv)
		}
	}
	if host != "" {
		out = append(out, "DOCKER_HOST="+host)
	}
	return out
}

// detectPort resolves the internal port for a sandbox container. Preference
// order: explicit PORT/NIXPACKS_PORT variable from the plan, then a provider
// default. Ports here must match what the app actually binds; the platform
// also injects PORT=<port> into the running container so most frameworks
// listen on exactly this value.
func detectPort(provider string, variables map[string]string) int {
	if v := variables["PORT"]; v != "" {
		if n, ok := parsePort(v); ok {
			return n
		}
	}
	if v := variables["NIXPACKS_PORT"]; v != "" {
		if n, ok := parsePort(v); ok {
			return n
		}
	}
	return ProviderDefaultPort(provider)
}

// ProviderDefaultPort returns a sensible default listening port per ecosystem.
func ProviderDefaultPort(provider string) int {
	switch provider {
	case "nodejs", "node":
		return 3000
	case "python", "python3", "flask", "django", "fastapi":
		return 8080
	case "golang", "go":
		return 8080
	case "ruby":
		return 4567
	case "php", "laravel":
		return 8000
	case "static":
		return 80
	case "rust", "cargo":
		return 8000
	default:
		// Conservative default covers most nixpacks providers.
		return 3000
	}
}

func parsePort(s string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 || n > 65535 {
		return 0, false
	}
	return n, true
}
