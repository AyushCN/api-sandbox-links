package orchestrator

import (
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/docker/docker/api/types/container"
)

func summary(id, sandboxID, service string) container.Summary {
	return container.Summary{
		ID:     id,
		Labels: map[string]string{SandboxIDLabel: sandboxID, "api-sandbox-links.service": service},
	}
}

func runtimeRow(id string, status db.SandboxStatus, containerID string) *db.Sandbox {
	return &db.Sandbox{ID: id, Status: status, ContainerID: containerID}
}

func TestPlanReconcileMatchesRuntimeRowsToContainers(t *testing.T) {
	destroyed := time.Now()
	rows := []*db.Sandbox{
		runtimeRow("running-ok", db.StatusRunning, "c1"),
		runtimeRow("hibernated-ok", db.StatusHibernated, "c2"),
		runtimeRow("stale-id", db.StatusRunning, "old"),
		runtimeRow("missing", db.StatusRunning, "gone"),
		runtimeRow("sidecar-only", db.StatusRunning, ""),
		runtimeRow("queued", db.StatusQueued, ""),
		{ID: "destroyed", Status: db.StatusExpired, DestroyedAt: &destroyed},
		{ID: "failed", Status: db.StatusFailed},
	}
	containers := []container.Summary{
		summary("c1", "running-ok", "app"),
		summary("c2", "hibernated-ok", "app"),
		summary("c3", "stale-id", "app"),
		summary("sidecar", "sidecar-only", "sidecar-db"),
		summary("orphan-sidecar", "missing", "sidecar-db"),
		summary("q1", "queued", "app"),
		summary("d1", "destroyed", "app"),
		summary("f1", "failed", "app"),
		summary("n1", "no-row", "app"),
	}

	plan := planReconcile(rows, containers)

	for _, id := range []string{"running-ok", "hibernated-ok", "stale-id"} {
		if !plan.Live[id] {
			t.Errorf("%s not marked live", id)
		}
	}
	missing := map[string]bool{}
	for _, sb := range plan.Missing {
		missing[sb.ID] = true
	}
	for _, id := range []string{"missing", "sidecar-only"} {
		if !missing[id] {
			t.Errorf("%s not marked missing", id)
		}
	}
	if missing["running-ok"] || missing["hibernated-ok"] || missing["stale-id"] {
		t.Errorf("live sandbox reported missing: %v", missing)
	}
	orphans := map[string]bool{}
	for _, id := range plan.Orphans {
		orphans[id] = true
	}
	for _, id := range []string{"queued", "destroyed", "failed", "no-row"} {
		if !orphans[id] {
			t.Errorf("%s not marked orphaned", id)
		}
	}
	for _, id := range []string{"running-ok", "hibernated-ok", "stale-id", "missing", "sidecar-only"} {
		if orphans[id] {
			t.Errorf("%s incorrectly marked orphaned", id)
		}
	}
}

func TestAppContainerFallsBackToStoredContainerID(t *testing.T) {
	sb := runtimeRow("legacy", db.StatusRunning, "legacy-id")
	containers := []container.Summary{
		{ID: "sidecar", Labels: map[string]string{"api-sandbox-links.service": "sidecar-db"}},
		{ID: "legacy-id", Labels: map[string]string{}},
	}
	if got := appContainerFor(sb, containers); got == nil || got.ID != "legacy-id" {
		t.Fatalf("appContainerFor fallback = %+v, want legacy-id", got)
	}
}
