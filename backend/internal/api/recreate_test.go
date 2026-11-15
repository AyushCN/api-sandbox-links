package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
)

type recordedDeploy struct {
	sandboxID string
	trigger   db.DeploymentTrigger
	database  string
}

type recordingDeployer struct {
	calls chan recordedDeploy
}

func (d *recordingDeployer) DeploySandbox(_ context.Context, sb *db.Sandbox, opts orchestrator.DeployOptions) error {
	d.calls <- recordedDeploy{sandboxID: sb.ID, trigger: opts.Trigger, database: opts.DatabaseOverride}
	return nil
}

func TestRecreateRouteCreatesFreshSandbox(t *testing.T) {
	s, gdb, srv, box := authedServer(t)
	parent := e2eSandbox(t, s, gdb, "recreate-route", 326, "sleep 30")
	cookie := sealedCookie(t, box, 326)

	enc, err := box.Encrypt([]byte("secret-value"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if err := s.store.UpsertEnvVar(parent.ID, "API_KEY", enc, true); err != nil {
		t.Fatalf("UpsertEnvVar: %v", err)
	}

	now := time.Now().UTC()
	if err := gdb.Model(&db.Sandbox{}).Where("id = ?", parent.ID).Updates(map[string]any{
		"status":             string(db.StatusDeleted),
		"destroyed_at":       now,
		"destruction_reason": string(db.DestroyUserRequested),
		"container_id":       "",
	}).Error; err != nil {
		t.Fatalf("marking parent destroyed: %v", err)
	}

	deployer := &recordingDeployer{calls: make(chan recordedDeploy, 1)}
	s.deployer = deployer

	resp, data := authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+parent.ID+"/actions/recreate", `{"lifetime_seconds":7200}`, cookie)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("recreate status = %d, want 202 (body %s)", resp.StatusCode, data)
	}

	var got struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		Branch          string `json:"branch"`
		LifetimeSeconds int64  `json:"lifetime_seconds"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if got.ID == "" || got.ID == parent.ID {
		t.Fatalf("recreated id = %q, want a fresh id", got.ID)
	}
	if got.Status != string(db.StatusQueued) {
		t.Errorf("status = %q, want queued", got.Status)
	}
	if got.Branch != parent.BranchName {
		t.Errorf("branch = %q, want %q", got.Branch, parent.BranchName)
	}
	if got.LifetimeSeconds != 7200 {
		t.Errorf("lifetime_seconds = %d, want 7200", got.LifetimeSeconds)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM sandboxes WHERE id = ?", got.ID) })

	child, err := s.store.GetSandbox(got.ID)
	if err != nil {
		t.Fatalf("GetSandbox(child): %v", err)
	}
	if child.RepositoryID != parent.RepositoryID {
		t.Errorf("repository = %q, want %q", child.RepositoryID, parent.RepositoryID)
	}

	vars, err := s.store.ListEnvVars(child.ID)
	if err != nil {
		t.Fatalf("ListEnvVars: %v", err)
	}
	if len(vars) != 1 || vars[0].Key != "API_KEY" || !vars[0].IsAISuggested {
		t.Fatalf("copied env vars = %+v, want API_KEY with its flag", vars)
	}
	if string(vars[0].Value) != string(enc) {
		t.Errorf("copied env value changed")
	}

	select {
	case call := <-deployer.calls:
		if call.sandboxID != got.ID {
			t.Errorf("deploy sandbox = %q, want %q", call.sandboxID, got.ID)
		}
		if call.trigger != db.TriggerRecreate {
			t.Errorf("deploy trigger = %q, want recreate", call.trigger)
		}
		if call.database != "auto" {
			t.Errorf("database override = %q, want auto", call.database)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recreated sandbox was never handed to the deployer")
	}
}

func TestRecreateRejectsLiveSandbox(t *testing.T) {
	s, gdb, srv, box := authedServer(t)
	parent := e2eSandbox(t, s, gdb, "recreate-live", 327, "sleep 30")
	cookie := sealedCookie(t, box, 327)
	s.deployer = &recordingDeployer{calls: make(chan recordedDeploy, 1)}

	resp, data := authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+parent.ID+"/actions/recreate", "", cookie)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("recreate live status = %d, want 409 (body %s)", resp.StatusCode, data)
	}
}

func TestRecreateWithoutBodyUsesParentLifetime(t *testing.T) {
	s, gdb, srv, box := authedServer(t)
	parent := e2eSandbox(t, s, gdb, "recreate-default", 328, "sleep 30")
	cookie := sealedCookie(t, box, 328)

	now := time.Now().UTC()
	if err := gdb.Model(&db.Sandbox{}).Where("id = ?", parent.ID).Updates(map[string]any{
		"status":             string(db.StatusExpired),
		"destroyed_at":       now,
		"destruction_reason": string(db.DestroyLifetimeExpired),
		"container_id":       "",
	}).Error; err != nil {
		t.Fatalf("marking parent expired: %v", err)
	}
	deployer := &recordingDeployer{calls: make(chan recordedDeploy, 1)}
	s.deployer = deployer

	resp, data := authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+parent.ID+"/actions/recreate", "", cookie)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("recreate status = %d, want 202 (body %s)", resp.StatusCode, data)
	}
	var got struct {
		ID              string `json:"id"`
		LifetimeSeconds int64  `json:"lifetime_seconds"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM sandboxes WHERE id = ?", got.ID) })
	if got.LifetimeSeconds != parent.LifetimeSeconds {
		t.Errorf("lifetime_seconds = %d, want parent lifetime %d", got.LifetimeSeconds, parent.LifetimeSeconds)
	}
	select {
	case <-deployer.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("recreated sandbox was never handed to the deployer")
	}
}

func TestRecreateInvalidLifetime(t *testing.T) {
	s, gdb, srv, box := authedServer(t)
	parent := e2eSandbox(t, s, gdb, "recreate-lifetime", 329, "sleep 30")
	cookie := sealedCookie(t, box, 329)

	// Mark parent as destroyed so we can recreate
	now := time.Now().UTC()
	if err := gdb.Model(&db.Sandbox{}).Where("id = ?", parent.ID).Updates(map[string]any{
		"status":             string(db.StatusExpired),
		"destroyed_at":       now,
		"destruction_reason": string(db.DestroyLifetimeExpired),
		"container_id":       "",
	}).Error; err != nil {
		t.Fatalf("marking parent expired: %v", err)
	}

	// Test with lifetime too low (below MinLifetimeSeconds = 3600)
	resp, data := authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+parent.ID+"/actions/recreate", `{"lifetime_seconds":100}`, cookie)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("recreate low lifetime status = %d, want 400 (body %s)", resp.StatusCode, data)
	}
	errMsg := string(data)
	if !strings.Contains(errMsg, "lifetime_seconds must be between") {
		t.Fatalf("recreate low lifetime body %q does not contain expected error", errMsg)
	}

	// Test with lifetime too high (above maxLifetime)
	resp, data = authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+parent.ID+"/actions/recreate", `{"lifetime_seconds":999999}`, cookie)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("recreate high lifetime status = %d, want 400 (body %s)", resp.StatusCode, data)
	}
	errMsg = string(data)
	if !strings.Contains(errMsg, "lifetime_seconds must be between") {
		t.Fatalf("recreate high lifetime body %q does not contain expected error", errMsg)
	}

	// Test with no body (should use parent's lifetime)
	resp, data = authedRequest(t, srv, http.MethodPost,
		"/api/sandboxes/"+parent.ID+"/actions/recreate", "", cookie)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("recreate no body status = %d, want 202 (body %s)", resp.StatusCode, data)
	}
	var got struct {
		ID              string `json:"id"`
		LifetimeSeconds int64  `json:"lifetime_seconds"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if got.LifetimeSeconds != parent.LifetimeSeconds {
		t.Errorf("lifetime_seconds = %d, want parent lifetime %d", got.LifetimeSeconds, parent.LifetimeSeconds)
	}
	t.Cleanup(func() { gdb.Exec("DELETE FROM sandboxes WHERE id = ?", got.ID) })
}
