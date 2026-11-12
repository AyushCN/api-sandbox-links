// Package orchestrator owns everything that touches Docker directly: building
// images with nixpacks, running/stopping sandbox containers, provisioning
// sidecar databases, streaming logs and stats, and tearing things down. The
// rest of the backend talks to this package, never to the Docker SDK.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/api-sandbox-links/backend/internal/cache"
	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/runtime"
	"github.com/api-sandbox-links/backend/internal/runtime/docker"
	"github.com/api-sandbox-links/backend/internal/secret"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
)

// Orchestrator is the single entry point for Docker lifecycle operations.
// It owns a Docker client plus the platform services it needs to wire a
// sandbox's container into storage, cache, and label-based routing.
//
// ctx is the process-lifetime context from main. Background streams started here
// (the per-sandbox stats collectors) derive from it, so shutting the server
// down tears them down instead of leaking a Docker connection each.
type Orchestrator struct {
	ctx        context.Context
	docker     *client.Client
	runner     runtime.ContainerRunner
	cfg        *config.Config
	store      *db.Store
	cache      *cache.Redis
	secret     *secret.Box
	logger     *slog.Logger
	builder    Builder
	statsMu    sync.Mutex // guards collectors
	collectors map[string]*StatsCollector
}

// New connects a Docker client and returns a ready Orchestrator. It fails
// fast if the daemon is unreachable so misconfiguration is obvious.
func New(ctx context.Context, cfg *config.Config, store *db.Store, cache *cache.Redis, box *secret.Box, logger *slog.Logger) (*Orchestrator, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithHost(cfg.DockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: creating docker client: %w", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("orchestrator: cannot reach docker daemon (%s): %w", cfg.DockerHost, err)
	}
	builder, err := NewBuilder(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: initializing builder: %w", err)
	}
	return &Orchestrator{
		ctx:        ctx,
		docker:     cli,
		runner:     docker.NewDockerRunner(cli),
		cfg:        cfg,
		store:      store,
		cache:      cache,
		secret:     box,
		logger:     logger,
		builder:    builder,
		collectors: map[string]*StatsCollector{},
	}, nil
}

// Close releases the orchestrator's Docker client connection.
func (o *Orchestrator) Close() error {
	return o.docker.Close()
}

// Ping verifies connectivity to the Docker daemon.
func (o *Orchestrator) Ping(ctx context.Context) error {
	if o == nil || o.docker == nil {
		return fmt.Errorf("orchestrator: docker client not initialized")
	}
	_, err := o.docker.Ping(ctx)
	return err
}

// Runner returns the active container runner abstraction.
func (o *Orchestrator) Runner() runtime.ContainerRunner {
	return o.runner
}

// SetRunner overrides the container runner (useful for testing or alternative runtime backends).
func (o *Orchestrator) SetRunner(r runtime.ContainerRunner) {
	o.runner = r
}

// ImageSizeBytes inspects the given image tag and returns its virtual size in bytes.
// If the image does not exist or inspection fails, an error is returned.
func (o *Orchestrator) ImageSizeBytes(ctx context.Context, imageTag string) (int64, error) {
	if o == nil || o.docker == nil {
		return 0, fmt.Errorf("orchestrator: docker client not initialized")
	}
	inspect, _, err := o.docker.ImageInspectWithRaw(ctx, imageTag)
	if err != nil {
		return 0, fmt.Errorf("orchestrator: inspecting image %s: %w", imageTag, err)
	}
	return inspect.Size, nil
}

// Logger exposes the slog logger for use by helpers in this package.
func (o *Orchestrator) Logger() *slog.Logger { return o.logger }

// volumeCreate builds the options for a named, labelled Docker volume. The
// sandbox-id label is what lets teardown reclaim volumes even when the
// sandbox_databases row was never written (for example a failed first deploy).
func volumeCreate(name, sandboxID string) volume.CreateOptions {
	labels := map[string]string{"api-sandbox-links.purpose": "sandbox-data"}
	if sandboxID != "" {
		labels[SandboxIDLabel] = sandboxID
	}
	return volume.CreateOptions{Name: name, Labels: labels}
}

// newID returns a UUID string for rows the DB model expects populated client-side.
func newID() string {
	return uuid.NewString()
}
