package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/filters"
)

// RunHygiene periodically removes dangling images and expired build cache.
// Without it every deploy leaves a layer set behind and the host disk fills up
// on a timescale measured in days. Old sandbox containers are handled by the
// expiry reaper; this worker only owns build artefacts.
func (o *Orchestrator) RunHygiene(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.pruneOnce(ctx)
		}
	}
}

func (o *Orchestrator) pruneOnce(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	imgFilters := filters.NewArgs(filters.Arg("dangling", "true"))
	report, err := o.docker.ImagesPrune(cctx, imgFilters)
	if err != nil {
		o.logger.Warn("image prune failed", slog.String("err", err.Error()))
	} else {
		o.logger.Info("dangling images pruned",
			slog.Int("deleted", len(report.ImagesDeleted)),
			slog.Uint64("reclaimed_bytes", report.SpaceReclaimed))
	}

	until := time.Duration(o.cfg.ImagePruneHours) * time.Hour
	cacheReport, err := o.docker.BuildCachePrune(cctx, build.CachePruneOptions{
		Filters: filters.NewArgs(filters.Arg("until", until.String())),
	})
	if err != nil {
		o.logger.Warn("build cache prune failed", slog.String("err", err.Error()))
		return
	}
	o.logger.Info("build cache pruned",
		slog.Int("records", len(cacheReport.CachesDeleted)),
		slog.Uint64("reclaimed_bytes", cacheReport.SpaceReclaimed))
}
