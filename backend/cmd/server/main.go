// Command server is the entry point for the API Sandbox Links backend. It
// keeps this file thin: load config, open infrastructure, wire dependencies,
// start background workers, serve two listeners (REST/WebSocket API and the
// sandbox traffic gateway), and shut everything down cleanly on SIGINT.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/api-sandbox-links/backend/internal/ai"
	"github.com/api-sandbox-links/backend/internal/api"
	"github.com/api-sandbox-links/backend/internal/cache"
	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/api-sandbox-links/backend/internal/reaper"
	"github.com/api-sandbox-links/backend/internal/secret"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run contains the full bootstrap. Splitting it from main keeps main readable
// and makes the whole boot sequence testable.
func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Root context cancelled by SIGINT/SIGTERM; every worker is cancellable via it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Infrastructure --------------------------------------------------
	dbh, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if err := db.Migrate(dbh); err != nil {
		return err
	}
	store := db.NewStore(dbh)

	redis, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}

	box, err := secret.NewBox(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	// --- Core services ---------------------------------------------------
	orch, err := orchestrator.New(ctx, cfg, store, redis, box, logger)
	if err != nil {
		return err
	}
	defer orch.Close()

	deployer := orchestrator.NewDeployer(cfg, store, orch, box, redis)
	if recovered, err := deployer.RecoverStuckBuilds(ctx); err != nil {
		logger.Warn("deployer: recovering stuck builds from previous run", slog.String("err", err.Error()))
	} else if recovered > 0 {
		logger.Info("deployer: recovered stuck builds from previous run", slog.Int64("count", recovered))
	}

	aiClient, err := ai.New(ctx, cfg.GeminiAPIKey, "gemini-2.5-flash", logger)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.SandboxWorkDir, 0o755); err != nil {
		return fmt.Errorf("main: creating sandbox work dir: %w", err)
	}

	// --- Background workers ----------------------------------------------
	// Three reapers over sandbox rows, kept strictly separate (idle =
	// reversible, expiry = irreversible, warnings = notifications), plus the
	// two that keep resource_snapshots bounded in both directions: one writes
	// them on a slow cadence, one trims them to the retention window.
	go reaper.NewWarningsWorker(store, logger, 60*time.Second).Run(ctx)
	go reaper.NewIdleReaper(store, redis, orch, logger, 30*time.Second).Run(ctx)
	go reaper.NewExpiryReaper(store, orch, cfg, logger, 60*time.Second).Run(ctx)
	go reaper.NewSnapshotReaper(store, logger, reaper.SnapshotSweepInterval, reaper.SnapshotRetention).Run(ctx)
	go orchestrator.NewStatsFlushWorker(store, redis, logger, orchestrator.StatsFlushInterval).Run(ctx)

	// Sweep builds running longer than DeployTimeout (e.g. stalled build commands)
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if timedOut, err := deployer.SweepTimedOutBuilds(ctx); err != nil {
					logger.Warn("deployer: sweeping timed-out builds", slog.String("err", err.Error()))
				} else if timedOut > 0 {
					logger.Warn("deployer: cleaned up timed-out builds", slog.Int64("count", timedOut))
				}
			}
		}
	}()

	// --- HTTP surfaces ---------------------------------------------------
	server := api.New(cfg, store, redis, orch, deployer, box, aiClient, logger)
	apiHandler := server.Router()
	gatewayHandler := server.Gateway()

	apiSrv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.BackendPort), Handler: apiHandler}
	gatewaySrv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.GatewayPort), Handler: gatewayHandler}

	errCh := make(chan error, 2)
	go func() { errCh <- apiSrv.ListenAndServe() }()
	go func() { errCh <- gatewaySrv.ListenAndServe() }()
	logger.Info("backend listening",
		slog.String("api", apiSrv.Addr),
		slog.String("gateway", gatewaySrv.Addr),
		slog.Bool("dev_auth", cfg.DevAuth))

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = apiSrv.Shutdown(shutdownCtx)
	_ = gatewaySrv.Shutdown(shutdownCtx)
	return nil
}
