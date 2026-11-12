package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
)

func TestDetectCacheHit(t *testing.T) {
	tests := []struct {
		name     string
		builder  string
		logs     string
		expected *bool
	}{
		{
			name:     "empty logs return nil",
			builder:  "railpack",
			logs:     "",
			expected: nil,
		},
		{
			name:     "whitespace only returns nil",
			builder:  "railpack",
			logs:     "   \n\t  ",
			expected: nil,
		},
		{
			name:     "railpack confirmed cache hit",
			builder:  "railpack",
			logs:     "=> [internal] load build definition from Dockerfile\n=> CACHED [2/5] RUN npm install\n=> exporting to image",
			expected: boolPtr(true),
		},
		{
			name:     "railpack cached bracket marker",
			builder:  "railpack",
			logs:     "Step 3/6 : RUN mise install\n ---> [cached] 8a9f23",
			expected: boolPtr(true),
		},
		{
			name:     "railpack completed steps with no cache hit",
			builder:  "railpack",
			logs:     "=> [1/4] FROM docker.io/library/node:20\n=> [2/4] COPY package.json .\n=> [3/4] RUN npm install\n=> [4/4] COPY . .\n=> exporting to image\n=> DONE",
			expected: boolPtr(false),
		},
		{
			name:     "nixpacks cache hit confirmed",
			builder:  "nixpacks",
			logs:     "[nixpacks] Setting up nix environment\n[nixpacks] Cache hit on /root/.npm\n[nixpacks] Building image...",
			expected: boolPtr(true),
		},
		{
			name:     "nixpacks using cache confirmed",
			builder:  "nixpacks",
			logs:     "Using cache for layer 4a123f\nSuccessfully built image",
			expected: boolPtr(true),
		},
		{
			name:     "nixpacks full build without cache",
			builder:  "nixpacks",
			logs:     "Generating nix plan...\nBuilding application with node provider...\nWriting image...",
			expected: boolPtr(false),
		},
		{
			name:     "ambiguous logs return nil",
			builder:  "custom",
			logs:     "Running random script with no known markers",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectCacheHit(tc.builder, tc.logs)
			if tc.expected == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", *got)
				}
			} else {
				if got == nil {
					t.Fatalf("expected %v, got nil", *tc.expected)
				}
				if *got != *tc.expected {
					t.Errorf("expected %v, got %v", *tc.expected, *got)
				}
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestTelemetryLifecycleIntegration(t *testing.T) {
	store, gdb := lifecycleEnv(t)
	sb := lifecycleSandbox(t, store, gdb, "telemetry-test", 880010)

	// 1. Verify T-01 & T-03: Builder and duration recorded on success
	d, _ := lifecycleDeployer(t, store, lifecycleNixpacks(t, "sleep 0.1\nexit 0"))
	lifecycleSeedWorkDir(t, d.sandboxDir(sb.Subdomain), sb, "package.json", `{"scripts":{"start":"node index.js"}}`)

	deployStart := time.Now()
	err := d.DeploySandbox(context.Background(), sb, DeployOptions{Trigger: db.TriggerInitial})
	if err != nil {
		t.Fatalf("DeploySandbox failed: %v", err)
	}

	deps, err := store.ListDeployments(sb.ID)
	if err != nil || len(deps) == 0 {
		t.Fatalf("ListDeployments failed: %v", err)
	}
	latest := deps[0]
	if latest.Builder != "nixpacks" {
		t.Errorf("expected Builder 'nixpacks', got %q", latest.Builder)
	}
	if latest.BuildDurationMs == nil || *latest.BuildDurationMs <= 0 {
		t.Errorf("expected positive BuildDurationMs, got %v", latest.BuildDurationMs)
	}
	if latest.Status != string(db.DeploySuccess) {
		t.Errorf("expected DeploySuccess, got %s", latest.Status)
	}
	if latest.FinishedAt == nil || latest.FinishedAt.Before(deployStart) {
		t.Errorf("expected FinishedAt after deployStart, got %v", latest.FinishedAt)
	}

	// 2. Verify T-02 & T-03: Builder and duration recorded on failed build, and image_size_bytes is nil
	sbFailed := lifecycleSandbox(t, store, gdb, "telemetry-fail", 880011)
	dFail, _ := lifecycleDeployer(t, store, lifecycleNixpacks(t, "sleep 0.05\necho intentional error >&2\nexit 3"))
	lifecycleSeedWorkDir(t, dFail.sandboxDir(sbFailed.Subdomain), sbFailed, "package.json", `{"scripts":{"start":"node index.js"}}`)

	err = dFail.DeploySandbox(context.Background(), sbFailed, DeployOptions{Trigger: db.TriggerInitial})
	if err == nil {
		t.Fatal("expected deploy error on exit 3, got nil")
	}

	failDeps, err := store.ListDeployments(sbFailed.ID)
	if err != nil || len(failDeps) == 0 {
		t.Fatalf("ListDeployments for failed build: %v", err)
	}
	failedDep := failDeps[0]
	if failedDep.Status != string(db.DeployFailed) {
		t.Errorf("expected DeployFailed, got %s", failedDep.Status)
	}
	if failedDep.Builder != "nixpacks" {
		t.Errorf("expected Builder 'nixpacks' on failure, got %q", failedDep.Builder)
	}
	if failedDep.BuildDurationMs == nil || *failedDep.BuildDurationMs <= 0 {
		t.Errorf("expected positive BuildDurationMs on failure, got %v", failedDep.BuildDurationMs)
	}
	if failedDep.ImageSizeBytes != nil {
		t.Errorf("expected nil ImageSizeBytes on failure, got %v", *failedDep.ImageSizeBytes)
	}
	if !strings.Contains(failedDep.LogExcerpt, "intentional error") {
		t.Errorf("expected failure excerpt to include error log, got %q", failedDep.LogExcerpt)
	}
}
