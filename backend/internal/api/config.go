package api

import "net/http"

// handleConfig serves GET /api/config. It is deliberately public: the frontend
// needs the limits and feature flags before a user signs in, and none of the
// values here are secrets — they are the same numbers the API enforces.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"base_domain":          s.cfg.SandboxBaseDomain,
		"min_lifetime_seconds": MinLifetimeSeconds,
		"max_lifetime_seconds": s.maxLifetime(),
		"lifetime_presets": map[string]int64{
			"1h":  3600,
			"6h":  21600,
			"24h": 86400,
			"7d":  604800,
		},
		"idle_min":     MinIdleTimeoutSeconds,
		"idle_max":     MaxIdleTimeoutSeconds,
		"max_per_user": s.maxActiveSandboxes(),
		"features": map[string]any{
			"webhooks":           s.cfg.GitHubWebhookSecret != "",
			"api_tokens":         true,
			"mysql":              true,
			"redis":              true,
			"per_container_logs": true,
		},
	})
}
