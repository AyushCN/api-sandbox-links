package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// SweepSandboxContainers stops and removes any container still labelled with
// the sandbox ID, touching nothing else — no networks, no volumes. It is the
// second half of the destroy-vs-deploy race fix and must be called AFTER the
// tombstone (destroyed_at) is committed:
//
//   - a deploy creates its container before MarkSandboxActive, and
//     MarkSandboxActive refuses tombstones, so
//   - containers created before the commit are visible to this sweep, and
//   - containers created after the commit make their deploy observe
//     ErrSandboxDestroyed and tear themselves down.
//
// That closes the window where a container born between the destroy's first
// teardown pass and its tombstone write would otherwise outlive a sandbox
// that is already deleted (the expiry reaper never revisits destroyed rows).
// Idempotent: with no containers left it does nothing.
func (o *Orchestrator) SweepSandboxContainers(ctx context.Context, sandboxID string) error {
	containers, err := o.FindContainersBySandbox(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("orchestrator: sweeping sandbox containers: %w", err)
	}
	for _, c := range containers {
		o.StopStatsCollector(sandboxID)
		if err := o.StopContainer(ctx, c.ID); err != nil {
			o.logger.Warn("sweep: stopping container",
				slog.String("container", c.ID), slog.String("err", err.Error()))
		}
		if err := o.RemoveContainer(ctx, c.ID); err != nil {
			o.logger.Warn("sweep: removing container",
				slog.String("container", c.ID), slog.String("err", err.Error()))
		}
	}
	return nil
}

// TeardownSandbox irreversibly removes everything the running sandbox owns:
// the app container, any sidecar database containers, their data volumes, and
// the private network. It is shared by the expiry reaper (lifetime_expired)
// and the manual "Destroy now" action (user_requested) — never by the idle
// reaper, whose hibernate action is reversible and must not touch volumes.
//
// The sandbox metadata row is deliberately NOT deleted here; the caller marks
// it destroyed (tombstone) afterward.
func (o *Orchestrator) TeardownSandbox(ctx context.Context, sandbox *db.Sandbox) error {
	containers, err := o.FindContainersBySandbox(ctx, sandbox.ID)
	if err != nil {
		return fmt.Errorf("orchestrator: listing sandbox containers for teardown: %w", err)
	}
	for _, c := range containers {
		o.StopStatsCollector(sandbox.ID)
		if err := o.StopContainer(ctx, c.ID); err != nil {
			o.logger.Warn("teardown: stopping container",
				slog.String("container", c.ID), slog.String("err", err.Error()))
		}
		if err := o.RemoveContainer(ctx, c.ID); err != nil {
			o.logger.Warn("teardown: removing container",
				slog.String("container", c.ID), slog.String("err", err.Error()))
		}
	}

	// Sidecar database volumes are the only durable per-sandbox storage; remove
	// them so nothing survives the destruction. Labels are the source of truth
	// (a sidecar row may be missing after a partially failed deploy), while the
	// row-derived names are kept as a fallback for volumes created before
	// volumes carried the sandbox label.
	volumes, err := o.FindVolumesBySandbox(ctx, sandbox.ID)
	if err != nil {
		o.logger.Warn("teardown: listing labelled volumes",
			slog.String("sandbox", sandbox.ID), slog.String("err", err.Error()))
	}
	for _, name := range volumes {
		if err := o.RemoveVolume(ctx, name); err != nil {
			o.logger.Warn("teardown: removing labelled volume",
				slog.String("volume", name), slog.String("err", err.Error()))
		}
	}
	dbs, err := o.store.ListSandboxDatabases(sandbox.ID)
	if err != nil {
		return fmt.Errorf("orchestrator: listing sandbox databases for teardown: %w", err)
	}
	for _, dbb := range dbs {
		if err := o.RemoveVolume(ctx, volumeName(dbb.ID)); err != nil {
			o.logger.Warn("teardown: removing db volume",
				slog.String("db", dbb.ID), slog.String("err", err.Error()))
		}
	}
	if _, err := o.store.DeleteSandboxDatabases(sandbox.ID); err != nil {
		o.logger.Warn("teardown: deleting sidecar rows",
			slog.String("sandbox", sandbox.ID), slog.String("err", err.Error()))
	}

	if err := o.RemoveNetwork(ctx, sandbox.ID); err != nil {
		o.logger.Warn("teardown: removing network",
			slog.String("sandbox", sandbox.ID), slog.String("err", err.Error()))
	}

	if ref := sandboxImageRef(sandbox); ref != "" {
		if err := o.RemoveImage(ctx, ref); err != nil {
			o.logger.Warn("teardown: removing image",
				slog.String("image", ref), slog.String("err", err.Error()))
		}
	}

	// Metric history for a destroyed sandbox is unreadable and, because the
	// sandbox row survives as a tombstone, never cascades away on its own — the
	// foreign key only fires if the row itself is deleted, which this platform
	// never does. Without this, every destroyed sandbox leaves up to 7 days of
	// rows behind for the retention reaper to find. Best-effort: a failure here
	// must not block a teardown that has already removed the containers.
	if _, err := o.store.DeleteResourceSnapshotsForSandbox(sandbox.ID); err != nil {
		o.logger.Warn("teardown: deleting resource snapshots",
			slog.String("sandbox", sandbox.ID), slog.String("err", err.Error()))
	}
	return nil
}

// FindVolumesBySandbox lists every Docker volume labelled with the sandbox
// ID. Volume labels are set when a sidecar volume is created, so teardown can
// reclaim a volume even when the sandbox_databases row was never written.
func (o *Orchestrator) FindVolumesBySandbox(ctx context.Context, sandboxID string) ([]string, error) {
	f := filters.NewArgs()
	f.Add("label", SandboxIDLabel+"="+sandboxID)
	list, err := o.docker.VolumeList(ctx, volume.ListOptions{Filters: f})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: listing sandbox volumes: %w", err)
	}
	names := make([]string, 0, len(list.Volumes))
	for _, v := range list.Volumes {
		if v != nil && v.Name != "" {
			names = append(names, v.Name)
		}
	}
	return names, nil
}

// RemoveImage removes a sandbox image. A missing image is success: failure
// cleanup is idempotent, and a build that never produced an image leaves
// nothing to remove.
func (o *Orchestrator) RemoveImage(ctx context.Context, ref string) error {
	if ref == "" {
		return nil
	}
	_, err := o.docker.ImageRemove(ctx, ref, image.RemoveOptions{Force: true, PruneChildren: true})
	if err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("orchestrator: removing image %s: %w", ref, err)
	}
	return nil
}

// sandboxImageRef returns the image this platform built for the sandbox. It
// only trusts the stored tag when it carries our sandbox- prefix, so a test or
// legacy row holding an unrelated tag (for example "latest") can never make
// teardown remove somebody else's image.
func sandboxImageRef(sandbox *db.Sandbox) string {
	if strings.HasPrefix(sandbox.ImageTag, "sandbox-") {
		return sandbox.ImageTag
	}
	if sandbox.Subdomain != "" {
		return "sandbox-" + sandbox.Subdomain + ":latest"
	}
	return ""
}
