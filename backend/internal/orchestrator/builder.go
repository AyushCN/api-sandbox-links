package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/api-sandbox-links/backend/internal/config"
)

// BuildPlan is the parsed, normalized output of a repository build plan analysis.
// The orchestrator relies on these fields to run, wire, and route the resulting container.
type BuildPlan struct {
	Builder          string            `json:"builder"`                     // "nixpacks" or "railpack"
	Provider         string            `json:"provider"`                    // e.g. "node", "python", "go"
	StartCmd         string            `json:"start_cmd"`                   // start command for container execution
	InternalPort     int               `json:"internal_port"`               // resolved app listening port
	DetectedLanguage string            `json:"detected_language"`           // primary language detected
	Variables        map[string]string `json:"variables,omitempty"`         // environment variables discovered during planning
}

// Builder defines the interface for analyzing source code and producing runnable container images.
type Builder interface {
	// Name returns the canonical name of the builder (e.g. "nixpacks", "railpack").
	Name() string

	// PlanAnalyze inspects the checked-out source directory and produces a normalized BuildPlan.
	PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error)

	// BuildImage compiles the source code in dir into an image named `name`, streaming output to progress.
	BuildImage(ctx context.Context, dir, name string, progress io.Writer) (string, error)
}

// NewBuilder creates a new Builder instance corresponding to the configured engine.
func NewBuilder(cfg *config.Config, logger *slog.Logger) (Builder, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Builder))
	if name == "" {
		name = "nixpacks"
	}

	switch name {
	case "nixpacks":
		return NewNixpacksBuilder(cfg.NixpacksBin, logger), nil
	case "railpack":
		return NewRailpackBuilder(cfg.RailpackBin, cfg.BuildKitHost, logger), nil
	default:
		return nil, fmt.Errorf("orchestrator: unsupported builder engine %q (supported: nixpacks, railpack)", name)
	}
}

