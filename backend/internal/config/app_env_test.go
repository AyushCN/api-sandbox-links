package config

import (
	"strings"
	"testing"
)

func TestAppEnvDefaultsToDevelopment(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("DEV_AUTH", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Errorf("AppEnv = %q, want development", cfg.AppEnv)
	}
	if cfg.DevAuth {
		t.Error("DevAuth enabled by default; it must be opt-in")
	}
}

func TestProductionRequiresOAuthCredentials(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_CLIENT_ID") {
		t.Fatalf("Load in production = %v, want error naming GITHUB_CLIENT_ID", err)
	}
}

func TestDevAuthIsOptInAndNeverProduction(t *testing.T) {
	cases := []struct {
		name    string
		appEnv  string
		devAuth string
		want    bool
	}{
		{"development opt-in", "development", "true", true},
		{"development off", "development", "false", false},
		{"staging opt-in", "staging", "true", true},
		{"production ignored", "production", "true", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("DEV_AUTH", tc.devAuth)
			t.Setenv("GITHUB_CLIENT_ID", "id")
			t.Setenv("GITHUB_CLIENT_SECRET", "secret")
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.DevAuth != tc.want {
				t.Errorf("DevAuth = %v, want %v", cfg.DevAuth, tc.want)
			}
		})
	}
}

func TestInvalidDevAuthFailsAtBoot(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("DEV_AUTH", "yes")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DEV_AUTH") {
		t.Fatalf("Load(DEV_AUTH=yes) = %v, want error naming DEV_AUTH", err)
	}
}
