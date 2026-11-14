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
	"github.com/api-sandbox-links/backend/internal/health"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/api-sandbox-links/backend/internal/reaper"
	"github.com/api-sandbox-links/backend/internal/secret"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(doctor())
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// doctor prints a pass/fail checklist for every external dependency and exits
// non-zero if any required check fails. It is deliberately usable before the
// server ever starts.
func doctor() int {
	fmt.Println("API Sandbox doctor")
	cfg, err := config.Load()
	if err != nil {
		fmt.Println(color(false, "FAIL") + " config: " + err.Error())
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	checks := []health.Check{
		health.Postgres(ctx, cfg.DatabaseURL),
		health.Redis(ctx, cfg.RedisURL),
		health.Docker(ctx, cfg),
	}
	checks = append(checks, health.StaticChecks(ctx, cfg)...)

	failed := false
	for _, c := range checks {
		if !c.OK {
			failed = true
		}
		fmt.Printf("%s %-16s %s\n", color(c.OK, map[bool]string{true: "PASS", false: "FAIL"}[c.OK]), c.Name, c.Detail)
	}
	fmt.Printf("\nbuild mode: %s (explicit, unisolated host Docker is the supported production path)\n", cfg.BuildMode)
	if failed {
		return 1
	}
	return 0
}

func color(ok bool, s string) string {
	if os.Getenv("NO_COLOR") != "" || !isTerminal(os.Stdout) {
		return s
	}
	if ok {
		return "\x1b[32m" + s + "\x1b[0m"
	}
	return "\x1b[31m" + s + "\x1b[0m"
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// apiAddr binds the control-plane API to loopback only. Sandbox containers can
// reach the host's bridge address, so an API on 0.0.0.0 would be directly
// reachable from untrusted sandbox code; Traefik only needs the gateway.
func apiAddr(port int) string { return fmt.Sprintf("127.0.0.1:%d", port) }

// gatewayAddr is intentionally not loopback: Traefik runs in its own container
// and reaches the gateway through host.docker.internal.
func gatewayAddr(port int) string { return fmt.Sprintf(":%d", port) }

// run contains the full bootstrap. Splitting it from main keeps main readable
// and makes the whole boot sequence testable.
func ctxOrBackground() context.Context { return context.Background() }

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Info("configuration loaded",
		slog.String("app_env", cfg.AppEnv),
		slog.String("build_mode", cfg.BuildMode),
		slog.Bool("dev_auth", cfg.DevAuth))
	if cfg.BuildMode == "host-docker" {
		logger.Warn("builds run on the host Docker daemon without isolation; this mode is explicit and supported")
	}
	if v, err := health.NixpacksVersion(ctxOrBackground(), cfg); err != nil {
		logger.Warn("nixpacks is unavailable", slog.String("err", err.Error()))
	} else {
		logger.Info("nixpacks detected", slog.String("version", v))
	}
	if disk := health.Disk(cfg); !disk.OK {
		logger.Warn("low disk space", slog.String("detail", disk.Detail))
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

	aiClient, err := ai.New(ctx, cfg.GeminiAPIKey, "gemini-2.5-flash", logger)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.SandboxWorkDir, 0o755); err != nil {
		return fmt.Errorf("main: creating sandbox work dir: %w", err)
	}

	// Docker and the database can disagree after an unclean shutdown. Reconcile
	// before any worker or request can observe the stale state.
	if err := orch.ReconcileStartup(ctx); err != nil {
		logger.Warn("startup reconciliation failed", slog.String("err", err.Error()))
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
	go orch.RunHygiene(ctx, time.Hour)

	// --- HTTP surfaces ---------------------------------------------------
	server := api.New(cfg, store, redis, orch, deployer, box, aiClient, logger)
	apiHandler := server.Router()
	gatewayHandler := server.Gateway()

	apiSrv := &http.Server{Addr: apiAddr(cfg.BackendPort), Handler: apiHandler}
	gatewaySrv := &http.Server{Addr: gatewayAddr(cfg.GatewayPort), Handler: gatewayHandler}

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
