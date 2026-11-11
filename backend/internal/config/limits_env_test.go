package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

// setBaseEnv sets the mandatory non-quota configuration so Load() gets past
// boot validation and the quota-specific assertions are what actually run.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example/db")
	t.Setenv("REDIS_URL", "redis://example:6379")
	t.Setenv("SECRETS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

// Abuse-control knobs come from the environment and gate every user's ability
// to create sandboxes, so a typo must fail at boot with a clear message, and
// an unset variable must land on the documented safe defaults — never on a
// zero that would deny (or unlimited that would allow) everything.

func TestAbuseControlDefaults(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MAX_ACTIVE_SANDBOXES_PER_USER", "")
	t.Setenv("MAX_RUNNING_SANDBOXES", "")
	t.Setenv("BUILD_CONCURRENCY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.MaxActiveSandboxesPerUser != 3 {
		t.Errorf("MaxActiveSandboxesPerUser = %d, want default 3", cfg.MaxActiveSandboxesPerUser)
	}
	if cfg.MaxRunningSandboxes != 6 {
		t.Errorf("MaxRunningSandboxes = %d, want default 6", cfg.MaxRunningSandboxes)
	}
	if cfg.BuildConcurrency != 2 {
		t.Errorf("BuildConcurrency = %d, want default 2", cfg.BuildConcurrency)
	}
}

func TestAbuseControlBoundsAreValidated(t *testing.T) {
	cases := []struct {
		key   string
		value string
		want  string
	}{
		{"MAX_ACTIVE_SANDBOXES_PER_USER", "0", "MAX_ACTIVE_SANDBOXES_PER_USER"},
		{"MAX_ACTIVE_SANDBOXES_PER_USER", "-3", "MAX_ACTIVE_SANDBOXES_PER_USER"},
		{"MAX_ACTIVE_SANDBOXES_PER_USER", "101", "MAX_ACTIVE_SANDBOXES_PER_USER"},
		{"MAX_RUNNING_SANDBOXES", "0", "MAX_RUNNING_SANDBOXES"},
		{"MAX_RUNNING_SANDBOXES", "1001", "MAX_RUNNING_SANDBOXES"},
		{"BUILD_CONCURRENCY", "0", "BUILD_CONCURRENCY"},
		{"BUILD_CONCURRENCY", "101", "BUILD_CONCURRENCY"},
	}
	for _, tc := range cases {
		setBaseEnv(t)
		t.Setenv("MAX_ACTIVE_SANDBOXES_PER_USER", "3")
		t.Setenv("MAX_RUNNING_SANDBOXES", "6")
		t.Setenv("BUILD_CONCURRENCY", "2")
		t.Setenv(tc.key, tc.value)

		_, err := Load()
		if err == nil {
			t.Errorf("%s=%s: Load() succeeded, want a validation error", tc.key, tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s=%s: error %q should name %s", tc.key, tc.value, err, tc.want)
		}
	}
}
