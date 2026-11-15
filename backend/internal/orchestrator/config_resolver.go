package orchestrator

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/api-sandbox-links/backend/internal/config"
)

// BuildConfigResolver creates a normalized, deterministic build configuration
// honoring the precedence: Explicit Deployment Settings > Project Config > Builder Defaults/Detection.
type BuildConfigResolver struct {
	cfg    *config.Config
	logger *slog.Logger
}

// NewBuildConfigResolver creates a new BuildConfigResolver.
func NewBuildConfigResolver(cfg *config.Config, logger *slog.Logger) *BuildConfigResolver {
	return &BuildConfigResolver{
		cfg:    cfg,
		logger: logger,
	}
}

// Resolve analyzes the repository using the selected builder and user options to construct a ResolvedBuildConfig.
func (r *BuildConfigResolver) Resolve(ctx context.Context, builder Builder, workDir string, userCfg *UserBuildConfig) (*ResolvedBuildConfig, error) {
	if builder == nil {
		return nil, fmt.Errorf("config resolver: builder cannot be nil")
	}

	relativeRootDir := "."
	effectiveDir := workDir
	if userCfg != nil && strings.TrimSpace(userCfg.RootDir) != "" {
		clean := filepath.Clean(strings.TrimSpace(userCfg.RootDir))
		if strings.HasPrefix(clean, "..") {
			return nil, fmt.Errorf("config resolver: invalid root_dir %q traverses outside repository", userCfg.RootDir)
		}
		relativeRootDir = clean
		effectiveDir = filepath.Join(workDir, clean)
	}

	plan, err := builder.PlanAnalyze(ctx, effectiveDir)
	if err != nil {
		return nil, err
	}

	var (
		dockerfilePath = ""
		preserveCmd    = false
		startCmd       = ""
		buildCmd       = ""
		installCmd     = ""
		language       = plan.DetectedLanguage
		runtimeVersion = ""
		packageManager = ""
		port           = plan.InternalPort
		variables      = make(map[string]string)
	)

	// Copy discovered variables from plan
	for k, v := range plan.Variables {
		variables[k] = v
	}

	// 1. Dockerfile-specific path resolution and CMD preservation
	if builder.Name() == "dockerfile" {
		if userCfg != nil && userCfg.DockerfilePath != "" {
			df, err := resolveDockerfilePath(effectiveDir, userCfg.DockerfilePath)
			if err != nil {
				return nil, fmt.Errorf("config resolver: %w", err)
			}
			dockerfilePath = df
		} else {
			df, _ := findDockerfile(effectiveDir)
			dockerfilePath = df
		}
		// Dockerfile CMD/ENTRYPOINT is preserved by default unless the user explicitly overrides StartCmd
		preserveCmd = true
		startCmd = plan.StartCmd
	}

	// 2. Project-level configuration (Procfile, railpack.json)
	if startCmd == "" && builder.Name() != "dockerfile" {
		// Try railpack.json
		if rjStart := readRailpackJSONStart(effectiveDir); rjStart != "" {
			startCmd = rjStart
		} else if procStart := readProcfileWeb(effectiveDir); procStart != "" {
			// Try Procfile
			startCmd = procStart
		} else if plan.StartCmd != "" {
			startCmd = plan.StartCmd
		}
	}

	// 3. Explicit user configuration (Precedence 1 - overrides all detected/project values)
	if userCfg != nil {
		if strings.TrimSpace(userCfg.StartCmd) != "" {
			startCmd = strings.TrimSpace(userCfg.StartCmd)
			preserveCmd = false // Explicit user command overrides image CMD/ENTRYPOINT
		}
		if strings.TrimSpace(userCfg.BuildCmd) != "" {
			buildCmd = strings.TrimSpace(userCfg.BuildCmd)
		}
		if strings.TrimSpace(userCfg.InstallCmd) != "" {
			installCmd = strings.TrimSpace(userCfg.InstallCmd)
		}
		if userCfg.Port > 0 {
			port = userCfg.Port
		}
		if strings.TrimSpace(userCfg.Language) != "" {
			language = strings.TrimSpace(userCfg.Language)
		}
		if strings.TrimSpace(userCfg.RuntimeVersion) != "" {
			runtimeVersion = strings.TrimSpace(userCfg.RuntimeVersion)
		}
		for k, v := range userCfg.Env {
			variables[k] = v
		}
	}

	if port <= 0 {
		port = 8080
	}

	return &ResolvedBuildConfig{
		Builder:         builder.Name(),
		DockerfilePath:  dockerfilePath,
		RootDir:         effectiveDir,
		RelativeRootDir: relativeRootDir,
		Language:        language,
		RuntimeVersion:  runtimeVersion,
		PackageManager:  packageManager,
		InstallCmd:      installCmd,
		BuildCmd:        buildCmd,
		StartCmd:        startCmd,
		Port:            port,
		PreserveCmd:     preserveCmd,
		Variables:       variables,
	}, nil
}

// readProcfileWeb parses Procfile in dir looking for "web: <command>".
func readProcfileWeb(dir string) string {
	procPath := filepath.Join(dir, "Procfile")
	f, err := os.Open(procPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "web:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "web:"))
		}
	}
	return ""
}

// readRailpackJSONStart parses deploy.startCommand or start.cmd from railpack.json if present.
func readRailpackJSONStart(dir string) string {
	rpPath := filepath.Join(dir, "railpack.json")
	data, err := os.ReadFile(rpPath)
	if err != nil {
		return ""
	}
	var payload struct {
		Deploy struct {
			StartCommand string `json:"startCommand"`
			StartCmd     string `json:"start_cmd"`
		} `json:"deploy"`
		Start struct {
			Cmd string `json:"cmd"`
		} `json:"start"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		if payload.Deploy.StartCommand != "" {
			return payload.Deploy.StartCommand
		}
		if payload.Deploy.StartCmd != "" {
			return payload.Deploy.StartCmd
		}
		if payload.Start.Cmd != "" {
			return payload.Start.Cmd
		}
	}
	return ""
}

