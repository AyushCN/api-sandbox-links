# Backend: Control Plane & Gateway Proxy

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Docker SDK](https://img.shields.io/badge/Docker-Engine%20SDK-2496ED?style=flat&logo=docker)](https://pkg.go.dev/github.com/docker/docker/client)
[![BuildKit](https://img.shields.io/badge/BuildKit-Rootless%20TCP-FF416C?style=flat)](https://github.com/moby/buildkit)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?style=flat&logo=postgresql)](https://www.postgresql.org)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat&logo=redis)](https://redis.io)

The backend service is a compiled Go binary providing both the **REST and WebSocket control plane** (`:8080`) and the **single-flight wake-on-request gateway proxy** (`:8090`).

---

## Key Responsibilities

* 🏛️ **Two Independent Listeners**:
  * **`:8080` (Control Plane)**: Authenticated chi router servicing `/api/sandboxes`, `/api/health`, Monaco IDE file operations, Git commits, and live WebSocket telemetry.
  * **`:8090` (Gateway Proxy)**: Low-latency HTTP reverse proxy resolving `*.sandbox.localhost` subdomains, waking hibernated containers, and forwarding traffic.
* 🚀 **Pluggable Builder Engine**:
  * Prioritizes native repository `Dockerfile` configurations when present.
  * Default cloud-native builder: **Railpack** connected to a rootless BuildKit daemon over TCP (`:1234`) with OCI layer caching.
  * Supported deprecated fallback: **Nixpacks** (`BUILDER=nixpacks`).
* 📦 **Automatic Sidecar Databases**:
  * Scans source code for database dependencies (`pg`, `mysql2`, `psycopg2`, Prisma, GORM).
  * Provisions isolated, pre-configured PostgreSQL or MySQL containers on a shared bridge network (`sandbox_<id[:8]>`).
* ⚡ **Single-Flight Container Wake-on-Request**:
  * Coalesces concurrent HTTP requests to a sleeping sandbox into a single `docker start` operation.
  * Waits for TCP readiness before proxying requests.
* 🛡️ **Container Hardening & Google gVisor**:
  * Drops all Linux capabilities (`CapDrop: ALL`), enforces `no-new-privileges`, supervises processes with `tini` init, and provides opt-in gVisor (`runsc`) user-space kernel virtualization.

---

## Directory Layout

```
backend/
├── cmd/server/               # Application main entrypoint
├── internal/
│   ├── api/                  # chi router, middlewares, WebSocket handlers, auth
│   ├── config/               # Environment configuration and defaults
│   ├── db/                   # GORM models, repository interfaces, queries
│   ├── git/                  # Repository cloning, branch switching, commit & push
│   ├── orchestrator/         # Container lifecycle, Railpack & Dockerfile builders, sidecars
│   └── proxy/                # Ingress reverse proxy & single-flight wake logic
└── migrations/               # Sequential SQL migrations (PostgreSQL)
```

---

## Building and Running

Because `go.mod` resides in `backend/`, compile from this directory:

```bash
cd backend
go build -o server ./cmd/server
```

Always launch the compiled binary from the **repository root** so `godotenv` loads `./.env` correctly:

```bash
cd ..
./backend/server
```

### Running Tests

```bash
# Unit tests with race detector
cd backend && go test -race ./...
```

For comprehensive integration testing with live Docker and PostgreSQL:
```bash
ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' \
  go test -race ./internal/db/ ./internal/api/ ./internal/orchestrator/
```

