# API Sandbox Links — Documentation Index

Turn any public GitHub repository into a disposable, time-limited, remotely-accessed cloud sandbox. This is the master index and guide to all project documentation.

---

## Documentation Catalog

| Document | Purpose & Scope | Key Code References |
|---|---|---|
| **[SETUP.md](SETUP.md)** | Step-by-step local development setup, environment variables, build orders, port configurations, and troubleshooting. | [`backend/internal/config/config.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/config/config.go), [`docker-compose.dev.yml`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/docker-compose.dev.yml), [`.env.example`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/.env.example) |
| **[ARCHITECTURE.md](ARCHITECTURE.md)** | System design, two-listener split (`:8080` API vs `:8090` Gateway), single-flight wake-on-request, Railpack/Dockerfile builders, sidecar databases, and reapers. | [`backend/internal/orchestrator/`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/orchestrator/), [`backend/internal/proxy/`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/proxy/), [`traefik/dynamic.yml`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/traefik/dynamic.yml) |
| **[API.md](API.md)** | Complete REST & WebSocket API reference: Sandboxes, Workspace Projects, Collaborators, Monaco Editor file operations, and Git push routes. | [`backend/internal/api/router.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/api/router.go), [`frontend/app/api/`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/frontend/app/api/) |
| **[DATABASE.md](DATABASE.md)** | PostgreSQL schema, versioned migrations, GORM entity models, activation timestamp mechanics, and snapshot retention. | [`backend/migrations/`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/migrations/), [`backend/internal/db/models.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/db/models.go) |
| **[SECURITY.md](SECURITY.md)** | Security posture, Linux capability dropping (`CapDrop: ALL`), Google gVisor kernel containment (`runsc`), AES-256 token encryption, and threat models. | [`backend/internal/orchestrator/lifecycle.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/orchestrator/lifecycle.go), [`backend/internal/api/auth.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/api/auth.go) |
| **[RUNBOOK.md](RUNBOOK.md)** | Production operations, single-node deployment runbook, automated recovery, systemd unit configurations, and TLS management. | [`docker-compose.prod.yml`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/docker-compose.prod.yml), [`traefik/`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/traefik/) |
| **[PRD.md](PRD.md)** | Product requirements document, feature specifications, performance milestones, and architectural acceptance criteria. | Specifications & feature deliverables |

---

## Architecture Flow Overview

```
Browser ── :3000 (Next.js Console) ──▶ API :8080 (REST + WebSockets)  ──▶ PostgreSQL (:5434) & Redis (:6380)
Browser ── :80 (Traefik Edge)      ──▶ Gateway :8090 (Wake Proxy)     ──▶ App Sandbox Container (:internal_port)
```

1. **Control Plane (`:8080`)**: Handled by the Go `chi` router. Manages sandbox lifecycle, workspace project scoping, user authentication, file tree inspection, and live metric WebSocket streams.
2. **Gateway Proxy (`:8090`)**: Intercepts inbound traffic for all `*.sandbox.localhost` subdomains. If a target container is hibernated, it wakes the container, awaits TCP healthiness, and reverse-proxies the request transparently.
3. **Builder Engine (`:1234`)**: Powered by a rootless BuildKit daemon communicating with Railpack to provide layer caching and fast OCI image builds.

---

## Verifying Stack Health

Before troubleshooting UI symptoms, verify that the backend and edge proxy are listening:

```bash
# Check control plane health
curl -s http://localhost:8080/api/health
# Response: {"status":"ok","time":"...","dev_auth":true}

# Check edge Traefik router
curl -I http://localhost:80
```

A browser reporting WebSocket close code `1006` typically indicates the backend process is stopped or inaccessible rather than a protocol failure. Refer to [SETUP.md](SETUP.md) for diagnostics.
