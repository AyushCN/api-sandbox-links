package api

import (
	"context"
	"net/http"
	"time"

	"github.com/api-sandbox-links/backend/internal/health"
)

// handleReady reports whether this instance can accept deploys. Liveness
// (/api/health) only proves the process is up; readiness checks every external
// dependency and returns 503 so a load balancer stops sending traffic.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := []health.Check{}
	if s.store != nil {
		c := health.Check{Name: "postgres"}
		if err := s.store.Ping(ctx); err != nil {
			c.Detail = err.Error()
		} else {
			c.OK = true
			c.Detail = "connected"
		}
		checks = append(checks, c)
	}
	if s.cache != nil {
		c := health.Check{Name: "redis"}
		if err := s.cache.Ping(ctx); err != nil {
			c.Detail = err.Error()
		} else {
			c.OK = true
			c.Detail = "connected"
		}
		checks = append(checks, c)
	}
	if s.orch != nil {
		c := health.Check{Name: "docker"}
		if err := s.orch.Ping(ctx); err != nil {
			c.Detail = err.Error()
		} else {
			c.OK = true
			c.Detail = "daemon reachable"
		}
		checks = append(checks, c)
	}
	checks = append(checks, health.StaticChecks(ctx, s.cfg)...)

	status := http.StatusOK
	overall := "ok"
	for _, c := range checks {
		if !c.OK {
			status = http.StatusServiceUnavailable
			overall = "degraded"
			break
		}
	}
	writeJSON(w, status, map[string]any{
		"status": overall,
		"time":   time.Now().UTC().Format(time.RFC3339),
		"checks": checks,
	})
}
