package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/api-sandbox-links/backend/internal/ai"
	"github.com/api-sandbox-links/backend/internal/cache"
	"github.com/api-sandbox-links/backend/internal/config"
	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
	"github.com/api-sandbox-links/backend/internal/proxy"
	"github.com/api-sandbox-links/backend/internal/secret"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server bundles every dependency the HTTP layer needs. It is constructed
// once in main and shared by all handlers.
type Server struct {
	cfg      *config.Config
	store    *db.Store
	cache    *cache.Redis
	orch     *orchestrator.Orchestrator
	deployer *orchestrator.Deployer
	box      *secret.Box
	ai       *ai.Client
	logger   *slog.Logger
	gateway  *proxy.Gateway
}

// New assembles the Server with its ready-to-use dependencies.
func New(
	cfg *config.Config,
	store *db.Store,
	cache *cache.Redis,
	orch *orchestrator.Orchestrator,
	deployer *orchestrator.Deployer,
	box *secret.Box,
	aiClient *ai.Client,
	logger *slog.Logger,
) *Server {
	return &Server{
		cfg:      cfg,
		store:    store,
		cache:    cache,
		orch:     orch,
		deployer: deployer,
		box:      box,
		ai:       aiClient,
		logger:   logger,
		gateway:  proxy.NewGateway(store, cache, orch, cfg, logger),
	}
}

// Gateway exposes the sandbox-traffic reverse proxy (Traefik forwards
// *.sandbox.localhost here) for the gateway listener in main.
func (s *Server) Gateway() http.Handler { return s.gateway.Handler() }

// Router builds the complete chi router for the /api surface.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.loggingMiddleware)
	r.Use(s.corsMiddleware)

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"time":     time.Now().UTC().Format(time.RFC3339),
			"dev_auth": s.cfg.DevAuth,
		})
	})

	// Production readiness probe: verifies Postgres, Redis, and Docker connectivity.
	r.Get("/api/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		components := map[string]string{
			"postgres": "ok",
			"redis":    "ok",
			"docker":   "ok",
		}
		var errs []string

		if err := s.store.Ping(ctx); err != nil {
			components["postgres"] = err.Error()
			errs = append(errs, "postgres: "+err.Error())
		}
		if err := s.cache.Ping(ctx); err != nil {
			components["redis"] = err.Error()
			errs = append(errs, "redis: "+err.Error())
		}
		if err := s.orch.Ping(ctx); err != nil {
			components["docker"] = err.Error()
			errs = append(errs, "docker: "+err.Error())
		}

		status := "ok"
		statusCode := http.StatusOK
		if len(errs) > 0 {
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		}

		writeJSON(w, statusCode, map[string]any{
			"status":     status,
			"time":       time.Now().UTC().Format(time.RFC3339),
			"components": components,
		})
	})

	// Public auth endpoints.
	r.Get("/api/auth/github", s.handleAuthStart)
	r.Get("/api/auth/github/callback", s.handleAuthCallback)

	// Logout is deliberately not behind requireUser. Its whole job is to clear
	// the session cookie, so requiring a valid session to reach it means a
	// caller whose session has already gone bad cannot clear the very cookie
	// that is causing it. Clearing a cookie only ever affects the caller's own
	// browser, so there is nothing to authorize here.
	r.Post("/api/auth/logout", s.handleLogout)

	r.Group(func(protected chi.Router) {
		protected.Use(s.requireUser)
		protected.Get("/api/me", s.handleMe)

		protected.Route("/api/sandboxes", func(pr chi.Router) {
			pr.Get("/", s.handleListSandboxes)
			pr.Post("/", s.handleCreateSandbox)
			pr.Get("/{id}", s.handleGetSandbox)
			pr.Post("/{id}/actions/extend", s.handleExtendLifetime)
			pr.Post("/{id}/actions/destroy", s.handleDestroySandbox)
			pr.Post("/{id}/fork", s.handleForkSandbox)
			pr.Get("/{id}/deployments", s.handleListDeployments)
			pr.Post("/{id}/deploy", s.handleRedeploy)
			pr.Get("/{id}/warnings", s.handleSandboxWarnings)
			pr.Get("/{id}/stats/history", s.handleStatsHistory)

			// Phase F/G: in-browser editor + git push.
			pr.Get("/{id}/files", s.handleFileTree)
			pr.Get("/{id}/file", s.handleFileContent)
			pr.Put("/{id}/file", s.handleSaveFile)
			pr.Post("/{id}/commit", s.handleCommitPush)
		})

		// Live channels. Both share authorizeSandboxAccess and the same
		// unavailable/closed lifecycle; see websocket.go.
		protected.Get("/api/sandboxes/{id}/logs/ws", s.handleLogsWS)
		protected.Get("/api/sandboxes/{id}/stats/ws", s.handleStatsWS)
	})

	return r
}

// writeJSON serializes v with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr is the standard error envelope; details are never secrets.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// loggingMiddleware records each request's method, path, duration, and status.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.logger.Info("http",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.Status()),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// corsMiddleware allows the Next.js dev server to call the API.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.FrontendURL)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
