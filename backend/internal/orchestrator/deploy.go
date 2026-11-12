package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/api-sandbox-links/backend/internal/cache"
	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/git"
	"github.com/api-sandbox-links/backend/internal/secret"
)

// Deployer coordinates one full deploy: clone (first time), nixpacks plan +
// build, container run, sidecar DB wiring, and status persistence. It is the
// shared path for the initial creation, save-and-redeploy, forks, and manual
// redeploys, kept in one place so every trigger behaves identically.

// DeployTimeout bounds one deploy from the moment it starts waiting for a
// build slot through the final activation. A queued build is included: the
// slot wait is part of the caller's deploy budget, not separate from it.
const DeployTimeout = 20 * time.Minute

const defaultBuildConcurrency = 2

type Deployer struct {
	cfg   *config.Config
	store *db.Store
	orch  *Orchestrator
	box   *secret.Box
	cache *cache.Redis

	// buildSem limits how many clones/builds/container starts run at once.
	// Nil means unlimited; only direct Deployer construction in tests omits it.
	buildSem chan struct{}
}

// NewDeployer builds the deploy coordinator.
func NewDeployer(cfg *config.Config, store *db.Store, orch *Orchestrator, box *secret.Box, cache *cache.Redis) *Deployer {
	concurrency := cfg.BuildConcurrency
	if concurrency < 1 {
		concurrency = defaultBuildConcurrency
	}
	return &Deployer{
		cfg: cfg, store: store, orch: orch, box: box, cache: cache,
		buildSem: make(chan struct{}, concurrency),
	}
}

// acquireBuildSlot waits for a build slot or for the deploy context to end.
// It returns a release function that is safe to call exactly once per success.
func (d *Deployer) acquireBuildSlot(ctx context.Context) (func(), error) {
	if d.buildSem == nil {
		return func() {}, nil
	}
	select {
	case d.buildSem <- struct{}{}:
		return func() { <-d.buildSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RecoverStuckBuilds sweeps any deployments left in pending or building state
// from a previous process run, and marks them failed with an explanatory message.
func (d *Deployer) RecoverStuckBuilds(ctx context.Context) (int64, error) {
	if d == nil || d.store == nil {
		return 0, nil
	}
	return d.store.RecoverStuckDeployments(0, "build aborted: worker process restarted while build was in progress")
}

// SweepTimedOutBuilds marks any builds that have been running longer than DeployTimeout
// as failed, preventing stuck processes or wedged tools from holding up queue slots forever.
func (d *Deployer) SweepTimedOutBuilds(ctx context.Context) (int64, error) {
	if d == nil || d.store == nil {
		return 0, nil
	}
	return d.store.RecoverStuckDeployments(DeployTimeout, "build aborted: build exceeded maximum execution timeout of 20 minutes")
}

// sandboxDir returns the on-disk clone directory for a sandbox.
func (d *Deployer) sandboxDir(subdomain string) string {
	return filepath.Join(d.cfg.SandboxWorkDir, subdomain)
}

// DeployOptions carries per-invocation choices for a deploy.
type DeployOptions struct {
	// Trigger explains what caused this deploy (initial, save_redeploy, ...).
	Trigger db.DeploymentTrigger
	// DatabaseOverride forces a sidecar engine ("postgres"/"mysql"/"none") or
	// leaves "auto" to repository detection.
	DatabaseOverride string
}

// DeploySandbox deploys (or redeploys) the given sandbox and records progress
// in the deployments history table. It is synchronous: callers run it in a
// background goroutine so the HTTP request returns immediately with a
// 'queued/building' status.
//
// The deploy does six things:
//  1. clone (only if the working copy is missing),
//  2. detect + provision a sidecar database if the repo needs one,
//  3. ask nixpacks what the app is and which port it uses,
//  4. nixpacks build into a Sandbox-specific image,
//  5. turn off any previous container for this sandbox,
//  6. run the new container on the private network with limits + labels.
func (d *Deployer) DeploySandbox(ctx context.Context, sandbox *db.Sandbox, opts DeployOptions) error {
	// Reload sandbox instead of creating new one, if it existed before
	fresh, err := d.store.GetSandbox(sandbox.ID)
	if err != nil {
		return fmt.Errorf("deploy: reloading sandbox: %w", err)
	}
	if fresh.DestroyedAt != nil {
		return fmt.Errorf("deploy: sandbox %s was destroyed; refusing to deploy", sandbox.ID)
	}

	deploy, err := d.store.CreateDeployment(&db.Deployment{
		SandboxID: sandbox.ID,
		Trigger:   string(opts.Trigger),
		Status:    string(db.DeployPending),
	})
	if err != nil {
		return fmt.Errorf("deploy: recording deployment: %w", err)
	}

	markFailed := func(cause error, logs ...string) error {
		now := deploymentFinished()
		logExcerpt := cause.Error()
		if len(logs) > 0 && logs[0] != "" {
			// Include build output if available
			combined := logs[0]
			if combined != "" {
				logExcerpt = truncate(combined+"\n"+cause.Error(), 4000)
			}
		}
		_ = d.store.UpdateDeployment(deploy.ID, map[string]any{
			"status":      string(db.DeployFailed),
			"log_excerpt": logExcerpt,
			"finished_at": now,
		})
		// A failed deploy must not leave any runtime behind. Tear down by the
		// sandbox-id label (not the stored container ID, which may still be
		// empty) so orphan app containers, sidecars, volumes, the network, and
		// the image are all reclaimed before the row is marked failed.
		if d.orch != nil && d.orch.docker != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := d.orch.TeardownSandbox(cleanupCtx, sandbox); err != nil {
				d.orch.logger.Warn("deploy: tearing down failed deploy",
					slog.String("sandbox", sandbox.ID), slog.String("err", err.Error()))
			}
			cancel()
		}
		// LIVE sandbox keeps its directory on failure: it may hold user edits from the file manager,
		// and it is reclaimed at expires_at by the reaper, so nothing is
		// abandoned indefinitely.
		if d.cleanupWorkDirIfDestroyed(sandbox.ID, sandbox.Subdomain) {
			d.orch.logger.Info("deploy: failed build on destroyed sandbox; removed work dir",
				slog.String("sandbox", sandbox.ID))
		}
		return d.store.UpdateSandbox(sandbox.ID, map[string]any{
			"status": string(db.StatusFailed),
		})
	}

	// Queue behind other builds before doing any expensive work. The row is
	// 'queued' while waiting and only becomes 'building' after a slot is held,
	// so the UI can distinguish "waiting for capacity" from "compiling".
	if err := d.store.UpdateSandbox(sandbox.ID, map[string]any{"status": string(db.StatusQueued)}); err != nil {
		return err
	}
	release, err := d.acquireBuildSlot(ctx)
	if err != nil {
		return markFailed(fmt.Errorf("deploy: waiting for build slot: %w", err))
	}
	defer release()

	if err := d.store.UpdateDeployment(deploy.ID, map[string]any{"status": string(db.DeployBuilding)}); err != nil {
		return markFailed(fmt.Errorf("deploy: marking deployment building: %w", err))
	}
	if err := d.store.UpdateSandbox(sandbox.ID, map[string]any{"status": string(db.StatusBuilding)}); err != nil {
		return markFailed(fmt.Errorf("deploy: marking sandbox building: %w", err))
	}

	workDir := d.sandboxDir(sandbox.Subdomain)
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(workDir), 0o755); err != nil {
			return markFailed(fmt.Errorf("deploy: creating work dir: %w", err))
		}
		repo, err := d.store.GetRepository(sandbox.RepositoryID)
		if err != nil {
			return markFailed(fmt.Errorf("deploy: loading repository: %w", err))
		}
		if _, err := git.Clone(ctx, repo.GithubURL, sandbox.BranchName, workDir); err != nil {
			// remove partial clone
			if rmErr := RemoveSandboxWorkDir(d.cfg.SandboxWorkDir, sandbox.Subdomain); rmErr != nil {
				d.orch.logger.Warn("deploy: removing partial clone after failure",
					slog.String("sandbox", sandbox.ID), slog.String("err", rmErr.Error()))
			}
			return markFailed(err)
		}
		// cleanup
		if d.cleanupWorkDirIfDestroyed(sandbox.ID, sandbox.Subdomain) {
			return markFailed(fmt.Errorf("deploy: sandbox was destroyed while cloning"))
		}
	}

	// Phase H: sidecar database. Detection reads the freshly-cloned files; an
	// explicit override wins over auto-detection.
	if err := d.maybeProvisionDatabase(ctx, sandbox, opts.DatabaseOverride, workDir); err != nil {
		return markFailed(fmt.Errorf("deploy: provisioning sidecar database: %w", err))
	}

	buildLog := &bytes.Buffer{}
	plan, err := d.orch.PlanAnalyze(ctx, workDir)
	if err != nil {
		return markFailed(err)
	}
	port := plan.InternalPort

	imageTag, err := d.orch.BuildImage(ctx, workDir, "sandbox-"+sandbox.Subdomain, buildLog)
	if err != nil {
		return markFailed(err, buildLog.String())
	}

	// Shut down and remove any previous container so the new image takes over.
	// Sidecar DB containers are untouched — only the app container is replaced.
	if sandbox.ContainerID != "" {
		if err := d.replaceAppContainer(ctx, sandbox.ID, sandbox.ContainerID); err != nil {
			return markFailed(fmt.Errorf("deploy: replacing old container: %w", err))
		}
	}

	envs, err := d.buildEnv(ctx, sandbox, port)
	if err != nil {
		return markFailed(err)
	}

	runOpts := RunOptions{
		SandboxID:    sandbox.ID,
		ImageTag:     imageTag,
		Subdomain:    sandbox.Subdomain,
		InternalPort: port,
		Env:          envs,
		NetworkName:  netName(sandbox.ID),
		Labels:       map[string]string{"api-sandbox-links.service": "app"},
	}
	result, err := d.orch.RunSandbox(ctx, runOpts)
	if err != nil {
		return markFailed(err)
	}

	// The nixpacks-detected port can be wrong when an app hardcodes its own
	// port instead of honoring PORT. Discover the real listening port so the
	// gateway routes to the right container port.
	discovered, err := d.orch.DiscoverPort(ctx, result.IPAddress, result.Port, 90*time.Second)
	if err != nil {
		return markFailed(fmt.Errorf("deploy: app not reachable on any known port: %w", err))
	}
	if discovered != result.Port {
		d.orch.logger.Info("deploy: overriding detected port",
			slog.Int("planned", result.Port), slog.Int("actual", discovered))
		result.Port = discovered
	}

	now := time.Now()
	// This is where the lifetime clock starts. On the first activation
	// MarkSandboxActive rebases expires_at to now + lifetime_seconds, so the time
	// spent building is not spent out of the user's sandbox. A redeploy returns
	// false and leaves the deadline alone.
	_, err = d.store.MarkSandboxActive(sandbox.ID, now, map[string]any{
		"status":            string(db.StatusRunning),
		"container_id":      result.ContainerID,
		"image_tag":         imageTag,
		"internal_port":     result.Port,
		"detected_language": plan.DetectedLanguage,
	})
	switch {
	case errors.Is(err, db.ErrSandboxDestroyed):
		// The sandbox was destroyed or expired while it was building. The
		// container that just came up belongs to a tombstone, so take it back
		// down rather than reporting a successful deploy of something the user
		// already deleted.
		d.orch.logger.Warn("deploy: sandbox was destroyed while building; discarding the new container",
			slog.String("sandbox", sandbox.ID), slog.String("container", result.ContainerID))
		_ = d.orch.TeardownSandbox(ctx, sandbox)
		// The deploy may have re-created the work directory after the destroy
		// removed it (clone-then-destroy race); destroy it again so a deleted
		// sandbox never leaves files behind. RemoveSandboxWorkDir validates
		// the path and is idempotent, so this is safe even when the destroy
		// already removed it.
		if rmErr := RemoveSandboxWorkDir(d.cfg.SandboxWorkDir, sandbox.Subdomain); rmErr != nil {
			d.orch.logger.Warn("deploy: removing work dir of destroyed sandbox",
				slog.String("sandbox", sandbox.ID), slog.String("err", rmErr.Error()))
		}
		return d.store.UpdateDeployment(deploy.ID, map[string]any{
			"status":      string(db.DeployFailed),
			"log_excerpt": "sandbox was destroyed while this deployment was still building",
			"finished_at": now,
		})
	case err != nil:
		return markFailed(fmt.Errorf("deploy: persisting run state: %w", err))
	}
	d.orch.EnsureStatsCollector(sandbox.ID, result.ContainerID)

	// Start the idle-timer clock at deploy time: the sandbox is considered
	// "recently used" the moment it becomes reachable, so a brand-new sandbox
	// gets its full idle grace period even if nobody has hit it yet.
	touchCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = d.cache.Touch(touchCtx, sandbox.ID, sandbox.IdleTimeoutSeconds)

	if err := d.store.UpdateDeployment(deploy.ID, map[string]any{
		"status":      string(db.DeploySuccess),
		"log_excerpt": truncate(buildLog.String(), 4000),
		"finished_at": now,
	}); err != nil {
		d.orch.logger.Warn("deploy: finalizing deployment record", slog.String("err", err.Error()))
	}
	// Capture build metadata on the sandbox row for the UI/debugging.
	builderName := plan.Builder
	if builderName == "" && d.orch != nil && d.orch.Builder() != nil {
		builderName = d.orch.Builder().Name()
	}
	buildMeta, _ := json.Marshal(map[string]any{
		"builder":  builderName,
		"provider": plan.Provider,
		"port":     plan.InternalPort,
		"start":    plan.StartCmd,
	})
	_ = d.store.UpdateSandbox(sandbox.ID, map[string]any{"build_config": string(buildMeta)})

	return nil
}

// maybeProvisionDatabase starts a sidecar DB for the sandbox when the repo
// needs one (or the user insists via override). Auto-detection reads the
// cloned working directory; an explicit "none" opts out.
func (d *Deployer) maybeProvisionDatabase(ctx context.Context, sandbox *db.Sandbox, override, workDir string) error {
	engine := strings.TrimSpace(override)
	if engine == "" || engine == "auto" {
		detected, needed := DetectDatabaseNeed(workDir)
		if !needed {
			return nil
		}
		engine = detected
	}
	if engine == "none" {
		return nil
	}
	switch engine {
	case "postgres", "mysql":
	default:
		return fmt.Errorf("unsupported database engine %q (want postgres, mysql, or none)", engine)
	}
	// Sidecar databases persist across redeploys; only provision when the
	// sandbox does not already own one (re-provisioning would collide on the
	// predictable db_<id> container name).
	existing, err := d.store.ListSandboxDatabases(sandbox.ID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		d.orch.logger.Info("deploy: reusing existing sidecar database",
			slog.String("sandbox", sandbox.ID), slog.String("engine", engine))
		return nil
	}
	_, connURL, err := d.orch.ProvisionDatabase(ctx, sandbox, engine)
	if err != nil {
		return err
	}
	d.orch.logger.Info("deploy: sidecar database provisioned",
		slog.String("sandbox", sandbox.ID), slog.String("engine", engine))
	_ = connURL
	return nil
}

// buildEnv assembles the container env: PORT plus the sandbox's decrypted env
// vars. Sidecar DB connection strings are injected here too (Phase H) so app
// code never has to hardcode a database URL.
func (d *Deployer) buildEnv(ctx context.Context, sandbox *db.Sandbox, port int) ([]string, error) {
	envs := []string{fmt.Sprintf("PORT=%d", port)}

	vars, err := d.store.ListEnvVars(sandbox.ID)
	if err != nil {
		return nil, fmt.Errorf("deploy: loading env vars: %w", err)
	}
	for _, v := range vars {
		plain, err := d.box.Decrypt(v.Value)
		if err != nil {
			return nil, fmt.Errorf("deploy: decrypting env var %q: %w", v.Key, err)
		}
		envs = append(envs, v.Key+"="+string(plain))
	}

	// Sidecar database connection strings are stored encrypted; inject them so
	// the app can connect without any further configuration.
	dbs, err := d.store.ListSandboxDatabases(sandbox.ID)
	if err != nil {
		return nil, fmt.Errorf("deploy: loading sidecar databases: %w", err)
	}
	for _, dbb := range dbs {
		plain, err := d.box.Decrypt(dbb.ConnectionURL)
		if err != nil {
			continue
		}
		envs = append(envs, "DATABASE_URL="+string(plain))
		// Many apps read a DB via per-vendor env vars instead of DATABASE_URL
		// (e.g. Go apps with godotenv using HOST/DBPORT/DBUSER/DBPASSWORD/DBNAME,
		// or libpq's PGHOST/PGPORT/...). Inject all of them so a repo's own
		// localhost defaults can't silently point at a nonexistent DB.
		for key, value := range dbEnvFromURL(string(plain)) {
			envs = append(envs, key+"="+value)
		}
	}
	return envs, nil
}

// dbEnvFromURL expands a sidecar connection URL into the individual host/port/
// user/password/db vars that common drivers and frameworks read, in both the
// generic (HOST/DBPORT/...) and libpq (PGHOST/PGPORT/...) spellings.
func dbEnvFromURL(u string) map[string]string {
	p, err := url.Parse(u)
	if err != nil {
		return nil
	}
	host := p.Hostname()
	port := p.Port()
	user := p.User.Username()
	pass, _ := p.User.Password()
	dbName := strings.TrimPrefix(p.Path, "/")
	if i := strings.IndexByte(dbName, '?'); i != -1 {
		dbName = dbName[:i]
	}
	generic := map[string]string{
		"HOST":       host,
		"DBPORT":     port,
		"DBUSER":     user,
		"DBPASSWORD": pass,
		"DBNAME":     dbName,
		// libpq / standard driver names (PGPASSWORD etc.) are honoured by many
		// database libraries without any application code changes.
		"PGHOST":     host,
		"PGPORT":     port,
		"PGUSER":     user,
		"PGPASSWORD": pass,
		"PGDATABASE": dbName,
	}
	for k, v := range generic {
		if v == "" {
			delete(generic, k)
		}
	}
	return generic
}

// replaceAppContainer removes the previous app container for a sandbox so a
// fresh image can take its place; other containers (e.g. the DB sidecar) are
// left running.
func (d *Deployer) replaceAppContainer(ctx context.Context, sandboxID, containerID string) error {
	d.orch.StopStatsCollector(sandboxID)
	if err := d.orch.RemoveContainer(ctx, containerID); err != nil {
		return err
	}
	return nil
}

func deploymentFinished() *time.Time {
	t := time.Now()
	return &t
}

// truncate limits a string to max runes for storage in a text column.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
