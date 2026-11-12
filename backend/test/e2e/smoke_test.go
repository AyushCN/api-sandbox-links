//go:build smoke

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/cache"
	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/api-sandbox-links/backend/internal/proxy"
	"github.com/api-sandbox-links/backend/internal/secret"
	"github.com/docker/docker/client"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Smoke test configuration defaults.
const (
	defaultSampleRepo   = "https://github.com/heroku/node-js-sample.git"
	defaultSampleBranch = "master" // heroku/node-js-sample uses master branch
	defaultDevPostgres  = "postgres://user:pass@127.0.0.1:5434/sandbox_platform?sslmode=disable"
	defaultDevRedis     = "redis://127.0.0.1:6380"
)

// smokeEnv validates availability of real Docker, Postgres, Redis, and Builder CLI.
// If any prerequisite is missing, the test skips gracefully with actionable setup instructions.
type smokeEnv struct {
	cfg        *config.Config
	store      *db.Store
	gdb        *gorm.DB
	cache      *cache.Redis
	secretBox  *secret.Box
	orch       *orchestrator.Orchestrator
	deployer   *orchestrator.Deployer
	gateway    *proxy.Gateway
	docker     *client.Client
	builderBin string
}

func setupSmokeEnv(t *testing.T) *smokeEnv {
	t.Helper()

	// 1. Docker daemon check
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithHost(dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Skipf("skipping e2e smoke test: failed to initialize Docker client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("skipping e2e smoke test: Docker daemon is unreachable (%v). Run dockerd or mount /var/run/docker.sock.", err)
	}

	// 2. Builder CLI check
	builderName := strings.ToLower(os.Getenv("ASL_SMOKE_BUILDER"))
	if builderName == "" {
		builderName = "nixpacks"
	}
	builderBin := os.Getenv("NIXPACKS_BIN")
	if builderBin == "" {
		builderBin = "nixpacks"
	}
	if builderName == "railpack" {
		if rb := os.Getenv("RAILPACK_BIN"); rb != "" {
			builderBin = rb
		} else {
			builderBin = "railpack"
		}
	}
	if _, err := exec.LookPath(builderBin); err != nil {
		t.Skipf("skipping e2e smoke test: builder binary %q not found on PATH. Install %s or set %s_BIN.", builderBin, builderName, strings.ToUpper(builderName))
	}

	// 2b. BuildKit connectivity check (for Railpack builder)
	if builderName == "railpack" {
		bkHost := os.Getenv("BUILDKIT_HOST")
		if bkHost == "" {
			bkHost = "tcp://127.0.0.1:1234"
		}
		ready := false
		var probeErr error
		for attempt := 1; attempt <= 10; attempt++ {
			if buildctlBin, err := exec.LookPath("buildctl"); err == nil {
				chkCtx, chkCancel := context.WithTimeout(context.Background(), 3*time.Second)
				out, chkErr := exec.CommandContext(chkCtx, buildctlBin, "--addr", bkHost, "debug", "workers").CombinedOutput()
				chkCancel()
				if chkErr == nil {
					ready = true
					break
				}
				probeErr = fmt.Errorf("%v (output: %s)", chkErr, strings.TrimSpace(string(out)))
			} else {
				addr := strings.TrimPrefix(bkHost, "tcp://")
				if conn, err := net.DialTimeout("tcp", addr, 1*time.Second); err == nil {
					_ = conn.Close()
					ready = true
					break
				} else {
					probeErr = err
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !ready {
			t.Skipf("skipping e2e smoke test: BuildKit daemon unreachable at %s (%v). Start docker-compose.dev.yml.", bkHost, probeErr)
		}
	}

	// 3. PostgreSQL database check
	dsn := os.Getenv("ASL_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultDevPostgres
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("skipping e2e smoke test: PostgreSQL is unreachable (%v). Set ASL_TEST_DATABASE_URL or start compose db.", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil || sqlDB.Ping() != nil {
		t.Skipf("skipping e2e smoke test: PostgreSQL ping failed at %s. Start docker-compose.dev.yml.", dsn)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("database migration failed: %v", err)
	}
	store := db.NewStore(gdb)

	// 4. Redis cache check
	redisURL := os.Getenv("ASL_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = defaultDevRedis
	}
	redisClient, err := cache.New(context.Background(), redisURL)
	if err != nil {
		t.Skipf("skipping e2e smoke test: Redis is unreachable (%v). Start docker-compose.dev.yml.", err)
	}

	// 5. Encryption key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("failed to create secret box: %v", err)
	}

	workDir := filepath.Join(t.TempDir(), "smoke-work")
	_ = os.MkdirAll(workDir, 0o755)

	cfg := &config.Config{
		Builder:                   builderName,
		NixpacksBin:               builderBin,
		RailpackBin:               builderBin,
		BuildKitHost:              os.Getenv("BUILDKIT_HOST"),
		SandboxWorkDir:            workDir,
		DockerHost:                dockerHost,
		SandboxBaseDomain:         "sandbox.localhost",
		BuildConcurrency:          2,
		MaxActiveSandboxesPerUser: 10,
		MaxRunningSandboxes:       20,
	}

	slogLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch, err := orchestrator.New(context.Background(), cfg, store, redisClient, box, slogLogger)
	if err != nil {
		t.Fatalf("failed to initialize orchestrator: %v", err)
	}
	t.Cleanup(func() { _ = orch.Close() })

	deployer := orchestrator.NewDeployer(cfg, store, orch, box, redisClient)
	gateway := proxy.NewGateway(store, redisClient, orch, cfg, slogLogger)

	return &smokeEnv{
		cfg:        cfg,
		store:      store,
		gdb:        gdb,
		cache:      redisClient,
		secretBox:  box,
		orch:       orch,
		deployer:   deployer,
		gateway:    gateway,
		docker:     cli,
		builderBin: builderBin,
	}
}

// TestSmokeDeployNodeJSSample runs a full end-to-end smoke test:
// 1. Clones heroku/node-js-sample (shallow).
// 2. Analyzes build plan with configured builder (default nixpacks).
// 3. Compiles container image and runs container on private bridge network.
// 4. Probes and discovers listening port (overriding defaults if needed).
// 5. Validates HTTP response over gateway proxy.
// 6. Validates container hibernation and on-demand wake via Gateway.
// 7. Tears down all Docker containers, networks, volumes, and workdirs.
func TestSmokeDeployNodeJSSample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow smoke test in -short mode")
	}

	env := setupSmokeEnv(t)

	tag := int64(990001)
	purge := func() {
		_ = env.gdb.Exec("DELETE FROM users WHERE github_id = ?", tag).Error
	}
	purge()
	t.Cleanup(purge)

	encryptedToken, err := env.secretBox.Encrypt([]byte("smoke-test-token"))
	if err != nil {
		t.Fatalf("failed to encrypt smoke token: %v", err)
	}

	user := &db.User{
		GithubID:    tag,
		Username:    "smoke-tester",
		Email:       "smoke@example.test",
		GithubToken: encryptedToken,
	}
	if err := env.store.UpsertUser(user); err != nil {
		t.Fatalf("failed to seed smoke user: %v", err)
	}

	repoURL := os.Getenv("ASL_SMOKE_REPO")
	if repoURL == "" {
		repoURL = defaultSampleRepo
	}
	branch := os.Getenv("ASL_SMOKE_BRANCH")
	if branch == "" {
		branch = defaultSampleBranch
	}

	repo, err := env.store.FindOrCreateRepository(user.ID, repoURL, branch)
	if err != nil {
		t.Fatalf("failed to create test repository record: %v", err)
	}

	subdomain := fmt.Sprintf("smoke-%d", time.Now().Unix()%100000)
	now := time.Now().UTC()
	sandbox := &db.Sandbox{
		RepositoryID:       repo.ID,
		BranchName:         branch,
		Subdomain:          subdomain,
		Status:             db.StatusQueued,
		LifetimeSeconds:    3600,
		IdleTimeoutSeconds: 300,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := env.store.CreateSandbox(sandbox); err != nil {
		t.Fatalf("failed to create sandbox record: %v", err)
	}

	// Always ensure teardown on test finish
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = env.orch.TeardownSandbox(cleanupCtx, sandbox)
		_ = orchestrator.RemoveSandboxWorkDir(env.cfg.SandboxWorkDir, sandbox.Subdomain)
	})

	t.Logf("starting smoke deploy for %s@%s (subdomain: %s)", repoURL, branch, subdomain)

	// Step 1: Execute deployment
	deployCtx, deployCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer deployCancel()

	err = env.deployer.DeploySandbox(deployCtx, sandbox, orchestrator.DeployOptions{
		Trigger: db.TriggerInitial,
	})
	if err != nil {
		excerpt := ""
		if deps, listErr := env.store.ListDeployments(sandbox.ID); listErr == nil && len(deps) > 0 {
			excerpt = deps[0].LogExcerpt
		}
		t.Fatalf("DeploySandbox failed: %v\nDeployment Log Excerpt:\n%s", err, excerpt)
	}

	// Step 2: Validate persisted state
	fresh, err := env.store.GetSandbox(sandbox.ID)
	if err != nil {
		t.Fatalf("failed to reload sandbox: %v", err)
	}
	if fresh.Status != db.StatusRunning {
		excerpt := ""
		if deps, listErr := env.store.ListDeployments(sandbox.ID); listErr == nil && len(deps) > 0 {
			excerpt = deps[0].LogExcerpt
		}
		t.Fatalf("expected sandbox status running, got %q (deployment log excerpt: %s)", fresh.Status, excerpt)
	}
	if fresh.ContainerID == "" {
		t.Fatal("expected non-empty ContainerID on running sandbox")
	}
	if fresh.InternalPort <= 0 {
		t.Fatalf("expected positive InternalPort, got %d", fresh.InternalPort)
	}

	// Verify build_config JSON recorded builder
	var buildConfig map[string]any
	if err := json.Unmarshal([]byte(fresh.BuildConfig), &buildConfig); err != nil {
		t.Errorf("failed to parse build_config JSON (%q): %v", fresh.BuildConfig, err)
	} else {
		if buildConfig["builder"] != env.cfg.Builder {
			t.Errorf("expected build_config.builder = %q, got %v", env.cfg.Builder, buildConfig["builder"])
		}
	}

	// Verify structured deployment telemetry (T-01, T-03, T-05)
	deps, err := env.store.ListDeployments(sandbox.ID)
	if err != nil || len(deps) == 0 {
		t.Fatalf("failed to query deployments: %v", err)
	}
	dep := deps[0]
	if dep.Builder != env.cfg.Builder {
		t.Errorf("expected deployment.builder = %q, got %q", env.cfg.Builder, dep.Builder)
	}
	if dep.BuildDurationMs == nil || *dep.BuildDurationMs <= 0 {
		t.Errorf("expected positive BuildDurationMs in deployment, got %v", dep.BuildDurationMs)
	}
	if dep.ImageSizeBytes == nil || *dep.ImageSizeBytes <= 0 {
		t.Errorf("expected positive ImageSizeBytes in deployment, got %v", dep.ImageSizeBytes)
	} else {
		t.Logf("telemetry verified: builder=%s, duration=%dms, image_size=%d bytes",
			dep.Builder, *dep.BuildDurationMs, *dep.ImageSizeBytes)
	}

	// Step 3: Validate HTTP response through Gateway Reverse Proxy
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = fmt.Sprintf("%s.%s", subdomain, env.cfg.SandboxBaseDomain)

	env.gateway.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gateway request failed with status %d (body: %s)", rec.Code, rec.Body.String())
	}
	t.Logf("gateway responded HTTP %d with %d bytes", rec.Code, rec.Body.Len())

	// Step 4: Validate Hibernation & On-Demand Wake
	t.Log("testing hibernation and wake-on-request...")
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()

	if err := env.orch.StopContainer(stopCtx, fresh.ContainerID); err != nil {
		t.Fatalf("failed to stop container for hibernation test: %v", err)
	}
	if err := env.store.UpdateSandbox(fresh.ID, map[string]any{"status": string(db.StatusHibernated)}); err != nil {
		t.Fatalf("failed to mark sandbox hibernated: %v", err)
	}

	// Make another request through the Gateway — it must trigger wake and return 200 OK
	wakeRec := httptest.NewRecorder()
	wakeReq := httptest.NewRequest("GET", "/", nil)
	wakeReq.Host = fmt.Sprintf("%s.%s", subdomain, env.cfg.SandboxBaseDomain)

	env.gateway.Handler().ServeHTTP(wakeRec, wakeReq)
	if wakeRec.Code != http.StatusOK {
		t.Fatalf("wake request failed with status %d (body: %s)", wakeRec.Code, wakeRec.Body.String())
	}

	// Confirm status transitioned back to running
	woken, err := env.store.GetSandbox(sandbox.ID)
	if err != nil || woken.Status != db.StatusRunning {
		t.Fatalf("expected sandbox to be running after wake, got status %q (err: %v)", woken.Status, err)
	}

	t.Log("smoke test completed successfully!")
}

// TestTCPPortConnectivity verifies TCP health checking against an open listener.
func TestTCPPortConnectivity(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind loopback port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to local TCP port: %v", err)
	}
	_ = conn.Close()
}

