package orchestrator

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/api-sandbox-links/backend/internal/config"
)

// UserBuildConfig captures user-supplied or repository-configured build options.
type UserBuildConfig struct {
	Builder        string            `json:"builder,omitempty"`         // "dockerfile", "railpack", "nixpacks", "auto"
	DockerfilePath string            `json:"dockerfile_path,omitempty"` // relative to RootDir or repo root
	RootDir        string            `json:"root_dir,omitempty"`        // relative root directory
	StartCmd       string            `json:"start_cmd,omitempty"`       // custom start command override
	BuildCmd       string            `json:"build_cmd,omitempty"`       // custom build command override
	InstallCmd     string            `json:"install_cmd,omitempty"`     // custom install command override
	Port           int               `json:"port,omitempty"`            // custom port override
	Language       string            `json:"language,omitempty"`        // custom runtime language
	RuntimeVersion string            `json:"runtime_version,omitempty"` // custom runtime version
	Env            map[string]string `json:"env,omitempty"`             // extra build env
}

// ResolvedBuildConfig is the normalized, deterministic build configuration passed to builders and deployers.
type ResolvedBuildConfig struct {
	Builder         string            `json:"builder"`                     // "dockerfile", "railpack", "nixpacks"
	DockerfilePath  string            `json:"dockerfile_path,omitempty"`   // absolute path to Dockerfile if applicable
	RootDir         string            `json:"root_dir"`                    // absolute path to effective source directory
	RelativeRootDir string            `json:"relative_root_dir,omitempty"` // e.g. "backend" or "."
	Language        string            `json:"language,omitempty"`
	RuntimeVersion  string            `json:"runtime_version,omitempty"`
	PackageManager  string            `json:"package_manager,omitempty"`
	InstallCmd      string            `json:"install_cmd,omitempty"`
	BuildCmd        string            `json:"build_cmd,omitempty"`
	StartCmd        string            `json:"start_cmd,omitempty"`
	Port            int               `json:"port"`
	PreserveCmd     bool              `json:"preserve_cmd"`                // true when Dockerfile CMD/ENTRYPOINT is preserved
	Variables       map[string]string `json:"variables,omitempty"`
}

// BuilderResolver deterministically selects the correct builder engine for a repository.
type BuilderResolver struct {
	cfg               *config.Config
	logger            *slog.Logger
	railpackBuilder   Builder
	nixpacksBuilder   Builder
	dockerfileBuilder Builder
}

// NewBuilderResolver creates a new BuilderResolver.
func NewBuilderResolver(cfg *config.Config, logger *slog.Logger) *BuilderResolver {
	var (
		dockerHost  string
		buildkit    string
		railpackBin = "railpack"
		nixpacksBin = "nixpacks"
	)
	if cfg != nil {
		dockerHost = cfg.DockerHost
		buildkit = cfg.BuildKitHost
		if cfg.RailpackBin != "" {
			railpackBin = cfg.RailpackBin
		}
		if cfg.NixpacksBin != "" {
			nixpacksBin = cfg.NixpacksBin
		}
	}
	return &BuilderResolver{
		cfg:               cfg,
		logger:            logger,
		railpackBuilder:   NewRailpackBuilder(railpackBin, buildkit, logger),
		nixpacksBuilder:   NewNixpacksBuilder(nixpacksBin, logger),
		dockerfileBuilder: NewDockerfileBuilder(dockerHost, logger),
	}
}

// NewBuilderResolverWithBuilders allows injecting mock builders for testing.
func NewBuilderResolverWithBuilders(cfg *config.Config, logger *slog.Logger, railpack, nixpacks, dockerfile Builder) *BuilderResolver {
	return &BuilderResolver{
		cfg:               cfg,
		logger:            logger,
		railpackBuilder:   railpack,
		nixpacksBuilder:   nixpacks,
		dockerfileBuilder: dockerfile,
	}
}

// ResolveBuilder determines which Builder to use according to the deterministic precedence:
// 1. Explicit user-selected builder
// 2. Explicit configured Dockerfile path
// 3. Repository Dockerfile when builder is not explicitly selected
// 4. Railpack auto-detection
// 5. Explicit Nixpacks selection
func (r *BuilderResolver) ResolveBuilder(workDir string, userCfg *UserBuildConfig) (Builder, error) {
	effectiveDir := workDir
	if userCfg != nil && strings.TrimSpace(userCfg.RootDir) != "" {
		clean := filepath.Clean(strings.TrimSpace(userCfg.RootDir))
		if strings.HasPrefix(clean, "..") {
			return nil, fmt.Errorf("builder resolver: invalid root_dir %q traverses outside repository", userCfg.RootDir)
		}
		effectiveDir = filepath.Join(workDir, clean)
	}

	// 1. Explicit user-selected builder
	if userCfg != nil && strings.TrimSpace(userCfg.Builder) != "" && strings.ToLower(strings.TrimSpace(userCfg.Builder)) != "auto" {
		switch strings.ToLower(strings.TrimSpace(userCfg.Builder)) {
		case "dockerfile":
			if userCfg.DockerfilePath != "" {
				if _, err := resolveDockerfilePath(effectiveDir, userCfg.DockerfilePath); err != nil {
					return nil, fmt.Errorf("builder resolver: %w", err)
				}
			} else {
				if _, err := findDockerfile(effectiveDir); err != nil {
					return nil, fmt.Errorf("builder resolver: explicit dockerfile builder requested but no Dockerfile found in %s", effectiveDir)
				}
			}
			return r.dockerfileBuilder, nil
		case "railpack":
			return r.railpackBuilder, nil
		case "nixpacks":
			if r.logger != nil {
				r.logger.Warn("builder: nixpacks is deprecated and will be removed in a future release; configure BUILDER=railpack")
			}
			return r.nixpacksBuilder, nil
		default:
			return nil, fmt.Errorf("builder resolver: unsupported builder engine %q (supported: dockerfile, railpack, nixpacks)", userCfg.Builder)
		}
	}

	// 2. Explicit configured Dockerfile path
	if userCfg != nil && strings.TrimSpace(userCfg.DockerfilePath) != "" {
		if _, err := resolveDockerfilePath(effectiveDir, userCfg.DockerfilePath); err != nil {
			return nil, fmt.Errorf("builder resolver: %w", err)
		}
		return r.dockerfileBuilder, nil
	}

	// 3. Repository Dockerfile when builder is not explicitly selected
	if _, err := findDockerfile(effectiveDir); err == nil {
		return r.dockerfileBuilder, nil
	}

	// 4 & 5. Auto-detection / platform fallback default
	defaultEngine := "railpack"
	if r.cfg != nil && strings.TrimSpace(r.cfg.Builder) != "" {
		defaultEngine = strings.ToLower(strings.TrimSpace(r.cfg.Builder))
	}

	switch defaultEngine {
	case "nixpacks":
		if r.logger != nil {
			r.logger.Warn("builder: nixpacks is deprecated and will be removed in a future release; configure BUILDER=railpack")
		}
		return r.nixpacksBuilder, nil
	case "dockerfile":
		if _, err := findDockerfile(effectiveDir); err != nil {
			return nil, fmt.Errorf("builder resolver: default builder is dockerfile but no Dockerfile found in %s", effectiveDir)
		}
		return r.dockerfileBuilder, nil
	default:
		return r.railpackBuilder, nil
	}
}

