package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RailpackBuilder implements the Builder interface using the Railpack CLI and BuildKit backend.
type RailpackBuilder struct {
	bin          string
	buildkitHost string
	logger       *slog.Logger
}

// NewRailpackBuilder constructs a new RailpackBuilder.
func NewRailpackBuilder(bin, buildkitHost string, logger *slog.Logger) *RailpackBuilder {
	if bin == "" {
		bin = "railpack"
	}
	if buildkitHost == "" {
		buildkitHost = "unix:///var/run/buildkit/buildkitd.sock"
	}
	return &RailpackBuilder{
		bin:          bin,
		buildkitHost: buildkitHost,
		logger:       logger,
	}
}

// Name returns the canonical identifier "railpack".
func (b *RailpackBuilder) Name() string {
	return "railpack"
}

// railpackPlanPayload captures the essential fields from Railpack build plan and info artifacts.
// Railpack serializes high-level metadata (providers, startCommand, env) across railpack plan
// and railpack prepare outputs. Permissive unmarshaling tolerates schema variations.
type railpackPlanPayload struct {
	Providers []string `json:"providers"`
	Provider  string   `json:"provider"`
	Languages []string `json:"languages"`
	Deploy    struct {
		StartCommand string `json:"startCommand"`
		StartCmd     string `json:"start_cmd"`
	} `json:"deploy"`
	Start struct {
		Cmd string `json:"cmd"`
	} `json:"start"`
	Variables map[string]string `json:"variables"`
	Env       map[string]string `json:"env"`
}

// railpackEnv constructs a safe child environment for Railpack and BuildKit execution.
// It allowlists essential system paths, toolchain version overrides (RAILPACK_* and MISE_*),
// passes through BUILDKIT_HOST, and prevents accidental inheritance of platform secrets.
func railpackEnv(home, buildkitHost string) []string {
	allowed := map[string]bool{
		"PATH": true, "USER": true, "LOGNAME": true,
		"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TZ": true,
		"TMPDIR": true, "TMP": true, "TEMP": true,
	}

	env := os.Environ()
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if allowed[key] || strings.HasPrefix(key, "RAILPACK_") || strings.HasPrefix(key, "MISE_") {
			out = append(out, kv)
		}
	}

	out = append(out, "HOME="+home)
	if buildkitHost != "" {
		out = append(out, "BUILDKIT_HOST="+buildkitHost)
	}
	return out
}

// PlanAnalyze inspects dir using Railpack and returns a normalized BuildPlan.
// It first attempts `railpack plan <dir> --json`, falling back to `railpack prepare <dir> --info-out <file>`.
func (b *RailpackBuilder) PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error) {
	home := os.Getenv("HOME")
	cmdEnv := railpackEnv(home, b.buildkitHost)

	// Attempt 1: railpack plan <dir> --json
	cmd := exec.CommandContext(ctx, b.bin, "plan", dir, "--json")
	cmd.Env = cmdEnv
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil && stdout.Len() > 0 {
		return parseRailpackPlan(stdout.Bytes())
	}

	// Attempt 2: railpack prepare <dir> --info-out <tmp>
	tmpInfoFile := filepath.Join(os.TempDir(), fmt.Sprintf("railpack-info-%d.json", os.Getpid()))
	defer os.Remove(tmpInfoFile)

	prepareCmd := exec.CommandContext(ctx, b.bin, "prepare", dir, "--info-out", tmpInfoFile)
	prepareCmd.Env = cmdEnv
	var prepStderr bytes.Buffer
	prepareCmd.Stderr = &prepStderr

	if prepErr := prepareCmd.Run(); prepErr != nil {
		return nil, fmt.Errorf("railpack builder: plan inspection failed: %w (stderr: %s)", prepErr, prepStderr.String())
	}

	infoData, err := os.ReadFile(tmpInfoFile)
	if err != nil {
		return nil, fmt.Errorf("railpack builder: reading prepare info output: %w", err)
	}

	return parseRailpackPlan(infoData)
}

// parseRailpackPlan unmarshals raw JSON bytes and maps them to a normalized BuildPlan.
func parseRailpackPlan(data []byte) (*BuildPlan, error) {
	var p railpackPlanPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("railpack builder: parsing plan output: %w", err)
	}

	provider := p.Provider
	if provider == "" && len(p.Providers) > 0 {
		provider = p.Providers[0]
	}
	if provider == "" && len(p.Languages) > 0 {
		provider = p.Languages[0]
	}

	startCmd := p.Deploy.StartCommand
	if startCmd == "" {
		startCmd = p.Deploy.StartCmd
	}
	if startCmd == "" {
		startCmd = p.Start.Cmd
	}

	variables := p.Variables
	if variables == nil && p.Env != nil {
		variables = p.Env
	}

	port := detectRailpackPort(provider, variables)

	return &BuildPlan{
		Builder:          "railpack",
		Provider:         provider,
		StartCmd:         startCmd,
		InternalPort:     port,
		DetectedLanguage: provider,
		Variables:        variables,
	}, nil
}

// BuildImage compiles source code in dir into an image named `name` using `railpack build`.
func (b *RailpackBuilder) BuildImage(ctx context.Context, dir, name string, progress io.Writer) (string, error) {
	args := []string{"build", "--name", name, dir}
	cmd := exec.CommandContext(ctx, b.bin, args...)
	cmd.Env = railpackEnv(os.Getenv("HOME"), b.buildkitHost)

	if progress != nil {
		cmd.Stdout = progress
		cmd.Stderr = progress
	}

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("railpack builder: build failed: %w", err)
	}

	return name + ":latest", nil
}

// detectRailpackPort determines the listening port for Railpack containers.
func detectRailpackPort(provider string, variables map[string]string) int {
	if variables != nil {
		if v := variables["PORT"]; v != "" {
			if n, ok := parsePort(v); ok {
				return n
			}
		}
		if v := variables["RAILPACK_PORT"]; v != "" {
			if n, ok := parsePort(v); ok {
				return n
			}
		}
	}
	return ProviderDefaultPort(provider)
}

