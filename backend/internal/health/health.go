// Package health implements the checks shared by GET /api/ready and the
// `server doctor` subcommand. Each check returns a plain result so both
// surfaces report exactly the same truth.
package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/api-sandbox-links/backend/internal/cache"
	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/docker/docker/client"
)

// Check is one pass/fail line in the checklist.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// NixpacksVersion returns the installed nixpacks version string.
func NixpacksVersion(ctx context.Context, cfg *config.Config) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, cfg.NixpacksBin, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w (%s)", cfg.NixpacksBin, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// BuildxVersion returns the Docker buildx version string.
func BuildxVersion(ctx context.Context, cfg *config.Config) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", "buildx", "version")
	cmd.Env = dockerEnv(cfg.DockerHost)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker buildx version: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

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

// Disk checks free space on the sandbox work filesystem.
func Disk(cfg *config.Config) Check {
	c := Check{Name: "disk"}
	path := cfg.SandboxWorkDir
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			path = "/"
			break
		}
		path = parent
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		c.Detail = fmt.Sprintf("cannot stat %s: %v", path, err)
		return c
	}
	freeGB := float64(st.Bavail*uint64(st.Bsize)) / (1024 * 1024 * 1024)
	c.OK = int64(freeGB) >= int64(cfg.DiskMinFreeGB)
	c.Detail = fmt.Sprintf("%.1f GB free on %s (minimum %d GB)", freeGB, path, cfg.DiskMinFreeGB)
	return c
}

// Domain checks the wildcard-domain configuration.
func Domain(cfg *config.Config) Check {
	c := Check{Name: "wildcard domain"}
	d := strings.TrimSpace(cfg.SandboxBaseDomain)
	if d == "" || !strings.Contains(d, ".") {
		c.Detail = "SANDBOX_BASE_DOMAIN must be a real domain, e.g. sandbox.example.com"
		return c
	}
	if d == "sandbox.localhost" || strings.HasSuffix(d, ".localhost") {
		c.OK = cfg.AppEnv != "production"
		if c.OK {
			c.Detail = d + " (local development only)"
		} else {
			c.Detail = d + " is not a public wildcard domain; set SANDBOX_BASE_DOMAIN=sandbox.example.com"
		}
		return c
	}
	c.OK = true
	c.Detail = "*." + d + " (certificate must cover the wildcard and the app domain)"
	return c
}

// Nixpacks checks the pinned builder CLI.
func Nixpacks(ctx context.Context, cfg *config.Config) Check {
	c := Check{Name: "nixpacks"}
	v, err := NixpacksVersion(ctx, cfg)
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	c.OK = true
	c.Detail = v
	return c
}

// Buildx checks the Docker buildx plugin.
func Buildx(ctx context.Context, cfg *config.Config) Check {
	c := Check{Name: "docker buildx"}
	v, err := BuildxVersion(ctx, cfg)
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	c.OK = true
	c.Detail = v
	return c
}

// StaticChecks are the checks that need no already-open connection. Readiness
// adds the live Postgres/Redis/Docker pings to this list.
func StaticChecks(ctx context.Context, cfg *config.Config) []Check {
	return []Check{
		Nixpacks(ctx, cfg),
		Buildx(ctx, cfg),
		Disk(cfg),
		Domain(cfg),
	}
}

// Postgres opens a fresh connection and pings it. Used by the doctor; the
// running server reuses its store instead.
func Postgres(ctx context.Context, dsn string) Check {
	c := Check{Name: "postgres"}
	if dsn == "" {
		c.Detail = "DATABASE_URL is empty"
		return c
	}
	gdb, err := db.Open(dsn)
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	defer sqlDB.Close()
	if err := sqlDB.PingContext(ctx); err != nil {
		c.Detail = err.Error()
		return c
	}
	var version string
	_ = sqlDB.QueryRowContext(ctx, "select version()").Scan(&version)
	c.OK = true
	c.Detail = strings.TrimSpace(version)
	if c.Detail == "" {
		c.Detail = "connected"
	}
	return c
}

// Redis opens a fresh connection and pings it.
func Redis(ctx context.Context, url string) Check {
	c := Check{Name: "redis"}
	if url == "" {
		c.Detail = "REDIS_URL is empty"
		return c
	}
	r, err := cache.New(ctx, url)
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	if err := r.Ping(ctx); err != nil {
		c.Detail = err.Error()
		return c
	}
	c.OK = true
	c.Detail = "connected"
	return c
}

// Docker pings the daemon configured for the platform.
func Docker(ctx context.Context, cfg *config.Config) Check {
	c := Check{Name: "docker"}
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if cfg.DockerHost != "" {
		opts = append(opts, client.WithHost(cfg.DockerHost))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	defer cli.Close()
	ping, err := cli.Ping(ctx)
	if err != nil {
		c.Detail = err.Error()
		return c
	}
	c.OK = true
	c.Detail = "API " + ping.APIVersion
	return c
}
