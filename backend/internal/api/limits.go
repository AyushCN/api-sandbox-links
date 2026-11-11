package api

// Abuse controls: the number of concurrently live sandboxes one user may keep
// and the platform-wide cap on live sandboxes. Both are enforced inside the
// INSERT itself (db.CreateSandboxWithinLimits) so racing requests cannot
// overshoot either limit, and both are checked before any expensive work
// (clone, nixpacks build, container start) begins.

import (
	"fmt"
	"net/http"
)

// Safe defaults applied at the point of use. config.Load() enforces the same
// bounds for environment-driven values; these guard programmatically built
// Config values (test harnesses) so an unconfigured zero can never mean
// "quota 0, deny everyone".
const defaultMaxActiveSandboxes = 3

func (s *Server) maxActiveSandboxes() int {
	if s.cfg.MaxActiveSandboxesPerUser >= 1 {
		return s.cfg.MaxActiveSandboxesPerUser
	}
	return defaultMaxActiveSandboxes
}

// maxRunningSandboxes is the platform-wide cap. A zero means "disabled" for
// programmatically built Configs (test harnesses); config.Load always sets a
// positive default (6) and rejects an explicit zero, so production is capped.
func (s *Server) maxRunningSandboxes() int {
	return s.cfg.MaxRunningSandboxes
}

// writeSandboxQuotaExceeded reports quota exhaustion. 409 Conflict: the
// request was valid and authenticated, but the account state (too many live
// sandboxes) refuses it; destroying a sandbox or waiting for expiry clears
// it. The response carries the numeric limit — configuration, not a secret.
func (s *Server) writeSandboxQuotaExceeded(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": fmt.Sprintf("active sandbox quota reached (%d); destroy a sandbox or wait for one to expire", s.maxActiveSandboxes()),
		"limit": s.maxActiveSandboxes(),
	})
}

// writeGlobalCapacityExceeded reports that the platform, not the caller's
// account, is at capacity. 503: this is a transient server-side limit and the
// request is valid; retrying after a sandbox expires is the remedy.
func (s *Server) writeGlobalCapacityExceeded(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": "the platform is at its live sandbox capacity; try again after a sandbox expires",
	})
}
