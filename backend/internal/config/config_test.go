package config

import (
	"encoding/base64"
	"testing"
)

func validEncryptionKey() string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("SECRETS_ENCRYPTION_KEY", validEncryptionKey())
}

func TestConfigDefaults(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected Load to succeed with valid minimal env, got %v", err)
	}

	if cfg.Builder != "nixpacks" {
		t.Errorf("expected default Builder to be 'nixpacks', got %q", cfg.Builder)
	}
	if cfg.NixpacksBin != "nixpacks" {
		t.Errorf("expected default NixpacksBin to be 'nixpacks', got %q", cfg.NixpacksBin)
	}
	if cfg.RailpackBin != "railpack" {
		t.Errorf("expected default RailpackBin to be 'railpack', got %q", cfg.RailpackBin)
	}
	if cfg.BuildKitHost != "unix:///var/run/buildkit/buildkitd.sock" {
		t.Errorf("expected default BuildKitHost to be 'unix:///var/run/buildkit/buildkitd.sock', got %q", cfg.BuildKitHost)
	}
	if cfg.BackendPort != 8080 {
		t.Errorf("expected default BackendPort 8080, got %d", cfg.BackendPort)
	}
	if cfg.GatewayPort != 8090 {
		t.Errorf("expected default GatewayPort 8090, got %d", cfg.GatewayPort)
	}
	if cfg.BuildConcurrency != 2 {
		t.Errorf("expected default BuildConcurrency 2, got %d", cfg.BuildConcurrency)
	}
	if cfg.MaxActiveSandboxesPerUser != 3 {
		t.Errorf("expected default MaxActiveSandboxesPerUser 3, got %d", cfg.MaxActiveSandboxesPerUser)
	}
}

func TestConfigBuilderSelection(t *testing.T) {
	tests := []struct {
		name        string
		builderEnv  string
		expected    string
		expectError bool
	}{
		{
			name:        "explicit nixpacks",
			builderEnv:  "nixpacks",
			expected:    "nixpacks",
			expectError: false,
		},
		{
			name:        "case-insensitive nixpacks",
			builderEnv:  "NIXPACKS",
			expected:    "nixpacks",
			expectError: false,
		},
		{
			name:        "explicit railpack",
			builderEnv:  "railpack",
			expected:    "railpack",
			expectError: false,
		},
		{
			name:        "case-insensitive railpack",
			builderEnv:  "Railpack",
			expected:    "railpack",
			expectError: false,
		},
		{
			name:        "invalid builder",
			builderEnv:  "dockerfile",
			expected:    "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setMinimalEnv(t)
			t.Setenv("BUILDER", tc.builderEnv)

			cfg, err := Load()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for BUILDER=%q, got nil", tc.builderEnv)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for BUILDER=%q: %v", tc.builderEnv, err)
				}
				if cfg.Builder != tc.expected {
					t.Errorf("expected Builder %q, got %q", tc.expected, cfg.Builder)
				}
			}
		})
	}
}

func TestConfigValidationErrors(t *testing.T) {
	t.Run("missing database url", func(t *testing.T) {
		setMinimalEnv(t)
		t.Setenv("DATABASE_URL", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when DATABASE_URL is missing")
		}
	})

	t.Run("missing redis url", func(t *testing.T) {
		setMinimalEnv(t)
		t.Setenv("REDIS_URL", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when REDIS_URL is missing")
		}
	})

	t.Run("missing encryption key", func(t *testing.T) {
		setMinimalEnv(t)
		t.Setenv("SECRETS_ENCRYPTION_KEY", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when SECRETS_ENCRYPTION_KEY is missing")
		}
	})

	t.Run("invalid encryption key length", func(t *testing.T) {
		setMinimalEnv(t)
		shortKey := base64.StdEncoding.EncodeToString([]byte("too-short"))
		t.Setenv("SECRETS_ENCRYPTION_KEY", shortKey)
		if _, err := Load(); err == nil {
			t.Fatal("expected error when SECRETS_ENCRYPTION_KEY has invalid byte length")
		}
	})

	t.Run("invalid concurrency bounds", func(t *testing.T) {
		setMinimalEnv(t)
		t.Setenv("BUILD_CONCURRENCY", "0")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when BUILD_CONCURRENCY is zero")
		}

		t.Setenv("BUILD_CONCURRENCY", "101")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when BUILD_CONCURRENCY exceeds 100")
		}
	})
}
