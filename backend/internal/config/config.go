// Package config loads and validates all server configuration from
// environment variables. It deliberately does not depend on any other
// internal package so it can be imported first during bootstrap.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config is the typed, validated view of every setting the backend needs.
// A single zero-value Config is never used: call Load at startup.
type Config struct {
	// DatabaseURL is the Postgres DSN for platform metadata.
	DatabaseURL string
	// RedisURL is the Redis connection string for hot state.
	RedisURL string

	// GitHub OAuth credentials used for sign-in and repo push permission.
	GitHubClientID         string
	GitHubClientSecret     string
	GitHubOAuthCallbackURL string

	// GeminiAPIKey is optional; AI features degrade gracefully when empty.
	GeminiAPIKey string

	// EncryptionKey is a 32-byte AES-256 key (base64-encoded in the env var)
	// used to encrypt tokens and secrets at rest.
	EncryptionKey []byte

	// DockerHost is the daemon socket, e.g. unix:///var/run/docker.sock.
	DockerHost string
	// SandboxWorkDir is the host directory where repos are cloned and built.
	SandboxWorkDir string

	// BackendPort is the REST/WebSocket API port.
	BackendPort int
	// GatewayPort is the internal port Traefik forwards sandbox subdomain
	// traffic to; the wake middleware + reverse proxy run there.
	GatewayPort int

	// FrontendURL is where the Next.js app is served (for OAuth + CORS).
	FrontendURL string
	// SandboxBaseDomain is e.g. "sandbox.localhost"; sandboxes get their own
	// <subdomain>.sandbox.localhost.
	SandboxBaseDomain string

	// NixpacksBin is the path to the nixpacks CLI binary.
	NixpacksBin string

	// AppEnv is "development" (default) or "production". Production requires
	// real OAuth credentials and disables DevAuth.
	AppEnv string

	// DevAuth, when true AND AppEnv is not production, lets requests
	// authenticate with a fake GitHub identity so the deploy loop is testable
	// without OAuth round-trips. It is opt-in; being unset means off.
	DevAuth bool

	// MaxLifetimeSeconds caps how far expires_at may be extended from original
	// creation (default 7 days).
	MaxLifetimeSeconds int64

	// MaxActiveSandboxesPerUser caps how many sandboxes one user may keep
	// concurrently alive (rows with destroyed_at NULL). Creation past the cap
	// is refused with 409 before any build starts; destroying or expiring a
	// sandbox releases its slot. Enforced atomically in the database (see
	// Store.CreateSandboxWithinQuota), so racing requests cannot overshoot.
	MaxActiveSandboxesPerUser int
	// MaxRunningSandboxes caps how many sandboxes may be live platform-wide,
	// across every user. Default 6. It is enforced in the same atomic insert as
	// the per-user quota (Store.CreateSandboxWithinLimits). A programmatically
	// built Config with zero disables the cap (test harnesses); config.Load
	// always sets a positive value.
	MaxRunningSandboxes int
	// BuildConcurrency caps how many deploys may clone/build/start a container
	// at the same time. Extra deploys wait in the queue (sandbox status
	// 'queued') until a slot frees. Default 2.
	BuildConcurrency int

	// SandboxScheme is the public URL scheme for sandbox links. Production
	// defaults to https because wildcard TLS terminates at the edge.
	SandboxScheme string

	// BuildMode is how images are built. "host-docker" runs nixpacks/docker on
	// the host; this is the only supported production mode and it is explicitly
	// allowed (with a startup warning) because the backend already trusts the
	// host Docker daemon for every sandbox. "disabled" is useful for a control
	// plane that should not build.
	BuildMode string
	// BuildCPUQuota / BuildMemoryMB bound the build container. They are advisory
	// for the Docker CLI and are always paired with the process-group timeout.
	BuildCPUQuota float64
	BuildMemoryMB int
	// DiskMinFreeGB refuses new deploys when the sandbox work filesystem has
	// less free space. The friendly error is easier to act on than a half-built
	// image filling the disk.
	DiskMinFreeGB int
	// ImagePruneHours is the age at which the hygiene worker prunes dangling
	// images and build cache.
	ImagePruneHours int
}

// Load reads .env (if present) and the process environment, then parses it
// into a Config. It returns a helpful error for any required value that is
// missing or malformed. Call this once, in main.
func Load() (*Config, error) {
	// Load .env only for local dev; it is a no-op when the file is absent.
	_ = godotenv.Load()

	appEnv := envDefault("APP_ENV", "development")
	devAuth, err := envBool("DEV_AUTH", false)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		AppEnv:                    appEnv,
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		RedisURL:                  os.Getenv("REDIS_URL"),
		GitHubClientID:            os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret:        os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubOAuthCallbackURL:    os.Getenv("GITHUB_OAUTH_CALLBACK_URL"),
		GeminiAPIKey:              os.Getenv("GEMINI_API_KEY"),
		DockerHost:                envDefault("DOCKER_HOST", "unix:///var/run/docker.sock"),
		SandboxWorkDir:            envDefault("SANDBOX_WORK_DIR", "/tmp/api-sandbox-links/work"),
		BackendPort:               envIntDefault("BACKEND_PORT", 8080),
		GatewayPort:               envIntDefault("GATEWAY_PORT", 8090),
		FrontendURL:               envDefault("FRONTEND_URL", "http://localhost:3000"),
		SandboxBaseDomain:         envDefault("SANDBOX_BASE_DOMAIN", "sandbox.localhost"),
		NixpacksBin:               envDefault("NIXPACKS_BIN", "nixpacks"),
		MaxLifetimeSeconds:        envInt64Default("MAX_LIFETIME_SECONDS", 7*24*3600),
		MaxActiveSandboxesPerUser: envIntDefault("MAX_ACTIVE_SANDBOXES_PER_USER", 3),
		MaxRunningSandboxes:       envIntDefault("MAX_RUNNING_SANDBOXES", 6),
		BuildConcurrency:          envIntDefault("BUILD_CONCURRENCY", 2),
		SandboxScheme:             envDefault("SANDBOX_SCHEME", defaultSandboxScheme()),
		BuildMode:                 envDefault("BUILD_MODE", "host-docker"),
		BuildCPUQuota:             envFloatDefault("BUILD_CPU_QUOTA", 1.5),
		BuildMemoryMB:             envIntDefault("BUILD_MEMORY_MB", 2048),
		DiskMinFreeGB:             envIntDefault("DISK_MIN_FREE_GB", 5),
		ImagePruneHours:           envIntDefault("IMAGE_PRUNE_HOURS", 24),
		// DevAuth is opt-in and can never be enabled in production, even if the
		// flag is set. Otherwise a forgotten DEV_AUTH=true would ship a bypass.
		DevAuth: devAuth && appEnv != "production",
	}

	// Collect every problem before returning. A startup that lists all missing
	// settings is far quicker to fix than one that reveals them one restart at
	// a time.
	var problems []string
	if err := cfg.decryptKey(); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	if cfg.RedisURL == "" {
		problems = append(problems, "REDIS_URL is required")
	}
	if len(cfg.EncryptionKey) == 0 && os.Getenv("SECRETS_ENCRYPTION_KEY") == "" {
		problems = append(problems, "SECRETS_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	if cfg.AppEnv == "production" {
		if cfg.GitHubClientID == "" {
			problems = append(problems, "APP_ENV=production requires GITHUB_CLIENT_ID")
		}
		if cfg.GitHubClientSecret == "" {
			problems = append(problems, "APP_ENV=production requires GITHUB_CLIENT_SECRET")
		}
	}
	if cfg.SandboxScheme != "http" && cfg.SandboxScheme != "https" {
		problems = append(problems, fmt.Sprintf("SANDBOX_SCHEME must be http or https, got %q", cfg.SandboxScheme))
	}
	if cfg.AppEnv == "production" && cfg.SandboxScheme != "https" {
		problems = append(problems, "APP_ENV=production requires SANDBOX_SCHEME=https")
	}
	if cfg.BuildMode != "host-docker" && cfg.BuildMode != "disabled" {
		problems = append(problems, fmt.Sprintf("BUILD_MODE must be host-docker or disabled, got %q", cfg.BuildMode))
	}
	if cfg.BuildCPUQuota <= 0 {
		problems = append(problems, "BUILD_CPU_QUOTA must be greater than 0")
	}
	if cfg.BuildMemoryMB < 256 {
		problems = append(problems, "BUILD_MEMORY_MB must be at least 256")
	}
	if cfg.DiskMinFreeGB < 0 {
		problems = append(problems, "DISK_MIN_FREE_GB cannot be negative")
	}
	if cfg.ImagePruneHours < 1 {
		problems = append(problems, "IMAGE_PRUNE_HOURS must be at least 1")
	}
	// Abuse-control bounds: a zero or negative limit would either lock every
	// user out (0) or disable the control entirely, so refuse to boot.
	if cfg.MaxActiveSandboxesPerUser < 1 || cfg.MaxActiveSandboxesPerUser > 100 {
		problems = append(problems, fmt.Sprintf("MAX_ACTIVE_SANDBOXES_PER_USER must be between 1 and 100, got %d", cfg.MaxActiveSandboxesPerUser))
	}
	if cfg.MaxRunningSandboxes < 1 || cfg.MaxRunningSandboxes > 1000 {
		problems = append(problems, fmt.Sprintf("MAX_RUNNING_SANDBOXES must be between 1 and 1000, got %d", cfg.MaxRunningSandboxes))
	}
	if cfg.BuildConcurrency < 1 || cfg.BuildConcurrency > 100 {
		problems = append(problems, fmt.Sprintf("BUILD_CONCURRENCY must be between 1 and 100, got %d", cfg.BuildConcurrency))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("config: invalid configuration:\n  - %s\n(see .env.production.example)", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// decryptKey validates and decodes SECRETS_ENCRYPTION_KEY.
func (c *Config) decryptKey() error {
	raw := os.Getenv("SECRETS_ENCRYPTION_KEY")
	if raw == "" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("config: SECRETS_ENCRYPTION_KEY is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return fmt.Errorf("config: SECRETS_ENCRYPTION_KEY must decode to exactly 32 bytes, got %d", len(key))
	}
	c.EncryptionKey = key
	return nil
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("config: %s must be true or false, got %q", key, v)
	}
	return b, nil
}

func envIntDefault(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func defaultSandboxScheme() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return "https"
	}
	return "http"
}

func envFloatDefault(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envInt64Default(key string, fallback int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
