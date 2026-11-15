# API Sandbox Links

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Next.js](https://img.shields.io/badge/Next.js-16.2-black?style=flat&logo=next.js)](https://nextjs.org)
[![Docker](https://img.shields.io/badge/Docker-28%2B-2496ED?style=flat&logo=docker)](https://www.docker.com)
[![Builder](https://img.shields.io/badge/Builder-Railpack%20%2B%20BuildKit-FF416C?style=flat)](https://railpack.com)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> The operating system for modern ephemeral cloud environments. Turn any GitHub repository into an isolated, time-limited, live-previewable developer sandbox in seconds.

---

## Highlights

* 🚀 **Deterministic Builder Engine**: Prioritizes native `Dockerfile` configurations, intelligent **Railpack** buildpack plans with **BuildKit** layer caching over TCP (`:1234`), and supported Nixpacks fallback.
* 📦 **Zero-Leak Sidecar Databases**: Automatic dependency scanning detects database drivers (`pg`, `mysql2`, `psycopg2`, Prisma, GORM) and provisions isolated, pre-seeded PostgreSQL or MySQL containers on a shared bridge network.
* ⚡ **Single-Flight Wake-on-Request**: Idle containers automatically hibernate after inactivity. Inbound HTTP gateway requests (`:8090`) trigger instantaneous single-flight container wakeups with live boot log streaming.
* 👥 **Workspace Projects & Multi-Tenancy**: Organize sandboxes into microservice project groups. Manage collaborator roles (`OWNER`, `EDITOR`, `VIEWER`), assign repositories, and isolate environments without cross-project leakage.
* 💻 **In-Browser IDE & Live Telemetry**: Live stdout/stderr logs over WebSockets, xterm terminal console, Monaco code editor with save-redeploy loops, and one-click Git commits pushed to remote branches.
* 🛡️ **Container Hardening & gVisor Isolation**: Capability dropping (`CapDrop: ALL`), `no-new-privileges`, rootless BuildKit builds, and optional Google **gVisor** (`runsc`) user-space kernel virtualization.
* 🎨 **Refined Ash & Coral Design System**: Clean, modern UI with theme-synchronized light/dark modes, custom ash grey terminals, and subtle contrast controls.

---

## Architecture at a Glance

```
                              Internet / Developer
                                       │
                                       ▼
                     ┌───────────────────────────────────┐
                     │   Traefik Edge Reverse Proxy      │  :80 / :443 (Wildcard *.sandbox.localhost)
                     └─────────────────┬─────────────────┘
                                       │
                    ┌──────────────────┴──────────────────┐
                    │ (Sandbox HTTP)                      │ (Console & Control Plane)
                    ▼                                     ▼
        ┌───────────────────────┐             ┌───────────────────────┐
        │  Wake Gateway (:8090) │             │   API Server (:8080)  │  chi router, WebSockets
        └───────────┬───────────┘             └───────────┬───────────┘
                    │                                     │
                    ▼                                     ├─▶ PostgreSQL (:5434 / state)
        ┌───────────────────────┐                         ├─▶ Redis (:6380 / idle TTLs)
        │ Live App Sandboxes    │                         ├─▶ BuildKit Daemon (:1234)
        │ sandbox_<id>          │                         └─▶ Docker Daemon Socket
        │ Sidecars (Postgres/DB)│
        └───────────────────────┘
```

### Port Allocation Reference

| Service | Port | Host Binding | Purpose |
|---|---|---|---|
| **Frontend Console** | `3000` | `127.0.0.1` | Next.js 16 App Router UI & Monaco IDE |
| **API Control Plane** | `8080` | `127.0.0.1` | REST endpoints & live WebSocket telemetry |
| **Gateway Proxy** | `8090` | `127.0.0.1` | Reverse proxy with single-flight wake-on-request |
| **Edge Router (Traefik)** | `80`, `8081` | `0.0.0.0` | `*.sandbox.localhost` edge routing & Traefik dashboard (`:8081`) |
| **PostgreSQL** | `5434` | `127.0.0.1` | Primary persistent state & audit logs (`user:pass`, db `sandbox_platform`) |
| **Redis** | `6380` | `127.0.0.1` | Inactivity TTL markers (`lr:<id>`) & live metric caches |
| **BuildKit Daemon** | `1234` | `127.0.0.1` | TCP build daemon for Railpack image builds |

---

## Quick Start (Local Development)

### 1. Prerequisites
- **Docker Engine** & **Docker Compose v2** installed and running.
- **Go 1.25+** (tested against Go 1.25 / 1.26).
- **Node.js 20+** (LTS or Node 24).
- **Railpack CLI** (optional for host builds; runs via BuildKit):
  ```bash
  curl -sSL https://railpack.com/install.sh | sh
  ```

### 2. Infrastructure Setup
```bash
# Clone the repository
git clone https://github.com/AyushCN/api-sandbox-links.git
cd api-sandbox-links

# Configure environment variables
cp .env.example .env

# Spin up Postgres (5434), Redis (6380), Traefik (80/8081), and BuildKit (1234)
docker compose -f docker-compose.dev.yml up -d
```

### 3. Backend Service
```bash
# Build binary from backend/ (where go.mod resides)
cd backend && go build -o server ./cmd/server && cd ..

# Launch backend from the repository root (reads root .env)
./backend/server
```
*Note: In dev mode (`GITHUB_CLIENT_ID` empty in `.env`), the backend auto-authenticates requests as a local developer user.*

### 4. Frontend Application
```bash
cd frontend
npm install
npm run dev
```
Open **[http://localhost:3000](http://localhost:3000)** in your browser to access the dashboard.

---

## Repository Structure

```
api-sandbox-links/
├── backend/                  # Go service (Chi, GORM, Docker Engine SDK, BuildKit)
│   ├── cmd/server/           # Application entrypoint
│   ├── internal/api/         # REST routes, WebSockets, auth, and gateway proxy
│   ├── internal/orchestrator/# Container lifecycle, Railpack/Dockerfile builders, sidecars
│   ├── internal/db/          # Database queries and GORM schemas
│   └── migrations/           # Versioned SQL migrations
├── frontend/                 # Next.js 16 (React 19, Tailwind CSS v4, Monaco Editor)
│   ├── app/                  # App Router pages (Landing, Dashboard, Projects, IDE)
│   ├── components/           # UI components, terminal consoles, theme toggle
│   └── lib/                  # Auth state, theme provider, persistent projects store
├── docs/                     # Detailed architectural specifications & guides
│   ├── ARCHITECTURE.md       # Deep dive into routing, wake-on-request & builders
│   ├── SETUP.md              # Complete dev environment runbook & troubleshooting
│   ├── API.md                # Comprehensive REST & WebSocket endpoint catalog
│   ├── DATABASE.md           # Schema definitions and data lifecycle models
│   ├── SECURITY.md           # Threat models, gVisor containment & auth tokens
│   └── RUNBOOK.md            # Production deployment and operations guide
├── traefik/                  # Traefik dynamic file configuration
└── docker-compose.dev.yml    # Development infrastructure stack
```

---

## Documentation Index

Explore our comprehensive technical documentation:

- 📖 **[docs/README.md](docs/README.md)** — Master documentation index and reading order.
- ⚙️ **[backend/README.md](backend/README.md)** — Go backend architecture, two listeners (`:8080` & `:8090`), builders, and tests.
- 🎨 **[frontend/README.md](frontend/README.md)** — Next.js 16 Web IDE, ash grey terminals, workspace projects, and theme tokens.
- 🛠️ **[docs/SETUP.md](docs/SETUP.md)** — Step-by-step development setup, port mapping (`:5434`, `:6380`, `:1234`), and debugging guide.
- 🏛️ **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** — System architecture, gateway proxy, wake-on-request, and container lifecycles.
- 📡 **[docs/API.md](docs/API.md)** — Complete API specifications for Sandboxes, Workspace Projects, and WebSockets.
- 🗄️ **[docs/DATABASE.md](docs/DATABASE.md)** — PostgreSQL schema models, GORM definitions, and retention rules.
- 🔒 **[docs/SECURITY.md](docs/SECURITY.md)** — Threat model, capability dropping, AES-256 token encryption, and gVisor sandboxing.
- 🚀 **[docs/RUNBOOK.md](docs/RUNBOOK.md)** — Production operations, systemd unit definitions, automated recovery, and TLS.

---

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
