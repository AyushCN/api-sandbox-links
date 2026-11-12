package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
)

// reconcilePlan is the pure decision half of startup reconciliation. Keeping it
// separate from Docker calls makes the missing/orphan classification testable
// without a daemon.
type reconcilePlan struct {
	// Missing are running/hibernated rows with no app container.
	Missing []*db.Sandbox
	// Orphans are sandbox IDs that own containers but are not live rows.
	Orphans []string
	// Live is the set of runtime rows whose app container exists.
	Live map[string]bool
}

func planReconcile(rows []*db.Sandbox, containers []container.Summary) reconcilePlan {
	bySandbox := make(map[string][]container.Summary)
	for _, c := range containers {
		if sid := c.Labels[SandboxIDLabel]; sid != "" {
			bySandbox[sid] = append(bySandbox[sid], c)
		}
	}

	plan := reconcilePlan{Live: map[string]bool{}}
	runtimeRows := map[string]*db.Sandbox{}
	for _, sb := range rows {
		if sb.DestroyedAt != nil || (sb.Status != db.StatusRunning && sb.Status != db.StatusHibernated) {
			continue
		}
		runtimeRows[sb.ID] = sb
		if appContainerFor(sb, bySandbox[sb.ID]) == nil {
			plan.Missing = append(plan.Missing, sb)
			continue
		}
		plan.Live[sb.ID] = true
	}

	for sid := range bySandbox {
		if plan.Live[sid] {
			continue
		}
		if _, missing := runtimeRows[sid]; missing {
			// The missing-row teardown below already owns this sandbox's
			// residual containers; do not schedule a second teardown.
			continue
		}
		plan.Orphans = append(plan.Orphans, sid)
	}
	return plan
}

// appContainerFor identifies the app container among a sandbox's containers.
// ContainerID is a fallback for rows/containers created before the service
// label was added.
func appContainerFor(sb *db.Sandbox, containers []container.Summary) *container.Summary {
	for i := range containers {
		c := &containers[i]
		if c.Labels["api-sandbox-links.service"] == "app" || (sb.ContainerID != "" && c.ID == sb.ContainerID) {
			return c
		}
	}
	return nil
}

// ReconcileStartup makes the database agree with Docker after a crash or a
// restart: rows that claim to be running/hibernated but have no app container
// are marked failed and torn down, and containers/volumes/networks left over
// from terminal or deleted sandboxes are removed.
func (o *Orchestrator) ReconcileStartup(ctx context.Context) error {
	rows, err := o.store.ListAllSandboxes()
	if err != nil {
		return err
	}
	containers, err := o.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", SandboxIDLabel)),
	})
	if err != nil {
		return fmt.Errorf("orchestrator: listing sandbox containers for reconciliation: %w", err)
	}

	plan := planReconcile(rows, containers)
	rowsByID := make(map[string]*db.Sandbox, len(rows))
	for _, sb := range rows {
		rowsByID[sb.ID] = sb
	}
	handled := map[string]bool{}

	teardown := func(sb *db.Sandbox) {
		if handled[sb.ID] {
			return
		}
		handled[sb.ID] = true
		if err := o.TeardownSandbox(ctx, sb); err != nil {
			o.logger.Warn("reconcile: tearing down sandbox runtime",
				"sandbox", sb.ID, "err", err.Error())
		}
	}

	for _, sb := range plan.Missing {
		o.logger.Warn("reconcile: runtime row has no app container; marking failed", "sandbox", sb.ID)
		if err := o.store.UpdateSandbox(sb.ID, map[string]any{
			"status":       string(db.StatusFailed),
			"container_id": "",
		}); err != nil {
			o.logger.Warn("reconcile: marking missing sandbox failed", "sandbox", sb.ID, "err", err.Error())
		}
		teardown(sb)
	}

	for _, sid := range plan.Orphans {
		sb := rowsByID[sid]
		if sb == nil {
			sb = &db.Sandbox{ID: sid}
		}
		o.logger.Warn("reconcile: removing orphaned sandbox runtime", "sandbox", sid)
		teardown(sb)
	}

	// Volumes can outlive their containers (for example a partially failed
	// first deploy). Remove those whose sandbox is not live.
	vols, err := o.docker.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", SandboxIDLabel)),
	})
	if err != nil {
		o.logger.Warn("reconcile: listing sandbox volumes", "err", err.Error())
	} else {
		for _, v := range vols.Volumes {
			sid := v.Labels[SandboxIDLabel]
			if sid == "" || plan.Live[sid] || handled[sid] {
				continue
			}
			sb := rowsByID[sid]
			if sb == nil {
				sb = &db.Sandbox{ID: sid}
			}
			teardown(sb)
		}
	}

	// Private networks do not carry the sandbox ID label, but their name does.
	// Remove any whose 8-character id is not a live runtime row.
	nets, err := o.docker.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "api-sandbox-links.purpose=private-sandbox")),
	})
	if err != nil {
		o.logger.Warn("reconcile: listing sandbox networks", "err", err.Error())
	} else {
		for _, n := range nets {
			if !strings.HasPrefix(n.Name, "sandbox_") {
				continue
			}
			short := strings.TrimPrefix(n.Name, "sandbox_")
			live := false
			for sid := range plan.Live {
				if strings.HasPrefix(sid, short) {
					live = true
					break
				}
			}
			if live {
				continue
			}
			if err := o.docker.NetworkRemove(ctx, n.ID); err != nil {
				o.logger.Warn("reconcile: removing orphaned network", "network", n.Name, "err", err.Error())
			}
		}
	}
	return nil
}
