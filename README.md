# API Sandbox Links

Turn any public GitHub repository into a **disposable, time-limited, fully-remotable sandbox** in a few seconds:

- Paste a repo URL → the platform **clones** it, **builds** it with Nixpacks (auto-detected stack), and **runs** the app in an isolated container with **isolated networking** (`<subdomain>.localhost`).
- Every sandbox gets a **public URL** (`https://`-style, works over `:80`) routed through Traefik → a gateway proxy that performs **wake-on-request** for hibernating sandboxes.
- **Live logs** (xterm over WebSocket), **live CPU/memory metrics** (WebSocket + history chart), an in-browser **code editor** (Monaco) whose saves trigger a **save-redeploy**, and a one-click **commit & push** back to GitHub.
- Sandboxes are **ephemeral**: a configurable lifetime (default 24h, max 7d) auto-destroys them; an **idle timeout** (default 15m) hibernates them; **fork** duplicates a live sandbox into a sibling container.

> **Dev auth mode**: when `GITHUB_CLIENT_ID` is empty the backend auto-authenticates as a seeded `devuser`, so the whole system is testable without a GitHub OAuth app.

## Quick start

There's no Makefile — these are the exact commands (see `docs/SETUP.md`):

```bash
# 1. Environment + infrastructure
cp .env.example .env                     # fill in DATABASE_URL/REDIS/SECRETS (see SETUP.md)
docker compose -f docker-compose.dev.yml up -d     # postgres :5432, redis :6380, traefik :80/:8081

# 2. Backend — build from backend/ (that's where go.mod lives), run from the repo root
cd backend && go build -o server ./cmd/server && cd ..
./backend/server &                       # :8080 API + :8090 gateway

# 3. Frontend
cd frontend && npm i && npm run dev      # http://localhost:3000
```

Paste `https://github.com/heroku/node-js-sample` (branch `master`) in the dashboard's create form to try it end-to-end.

## Architecture at a glance

```
Browser ── :3000 (Next.js) ──> API :8080 (chi, PostgreSQL, Redis) ──> Docker (railpack/nixpacks images, per-sandbox containers)
Browser ── :80 (Traefik file provider) ──> gateway :8090 ──> sandbox container (wake-on-request before forwarding)
Browser ── ws :8080 ──> live logs (xterm) + live metrics (recharts)
```

The routing detail that matters: sandbox HTTP traffic does **not** hit the API on `:8080`. Traefik's file provider routes every `*.sandbox.localhost` host to the backend's **gateway listener on `:8090`**, which resolves the sandbox by subdomain, wakes it if hibernating, and reverse-proxies. See `docs/ARCHITECTURE.md`.

## Repo layout

```
backend/   Go 1.25 service (chi) — auth, orchestrator (Docker), git, deployer, gateway, WS
frontend/  Next.js 16 (App Router, Tailwind v4) dashboard & sandbox console
traefik/   dynamic file-provider config (wildcard edge router)
docs/      architecture, API, DB, and setup walkthroughs
docker-compose.dev.yml   local infra (postgres, redis, traefik) + volume
```

## Documentation

- `docs/README.md` — documentation index
- `docs/SETUP.md` — environment, prerequisites, build order, OAuth, troubleshooting
- `docs/ARCHITECTURE.md` — system design, routing, wake-on-request, port discovery, reapers
- `docs/API.md` — every endpoint, request/response shapes, WebSocket protocols
- `docs/DATABASE.md` — schema, migrations, GORM table-name caveats

All docs are cross-referenced to the code that defines them (config keys, schema,
routes), so they can be verified quickly when behaviour changes.

## Status

Phase A–G implemented and verified end-to-end in dev: create → clone → build (Railpack default) → container running → public URL responds → hibernate/wake → file edit → save-redeploy → commit/push wired (push requires a real GitHub token from OAuth sign-in).
