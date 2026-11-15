# Architecture & System Design

This document details the architectural principles, container orchestration pipelines, networking models, and data flows powering **API Sandbox Links**.

---

## 1. System Overview & Component Split

The platform is designed around a strict decoupling between the **control plane** (management API and UI) and the **data plane** (sandbox network traffic and dynamic container execution):

```
Browser ── :80 (Traefik Edge)      ──▶ Gateway :8090 (Wake Proxy)     ──▶ App Sandbox Container (:internal_port)
Browser ── :3000 (Next.js Console) ──▶ API :8080 (REST + WebSockets)  ──▶ PostgreSQL (:5434) & Redis (:6380)
                                                                       ├─▶ BuildKit Daemon (:1234)
                                                                       └─▶ Docker Daemon Socket
```

### Core Services

1. **`backend/` (Go 1.25+ Binary)**: Runs two independent HTTP/TCP listeners:
   - **Port `:8080` (Control Plane)**: REST API and WebSocket streams for log tailing, container metrics, file editor operations, and Git push.
   - **Port `:8090` (Gateway Proxy)**: Low-latency reverse proxy that inspects incoming subdomain host headers, wakes hibernated containers on demand, and forwards traffic.
2. **`frontend/` (Next.js 16 App Router & React 19)**:
   - Developer console and in-browser IDE with Monaco Editor, Ash Grey terminal consoles, and zero-drift Git management.
   - Next.js BFF (Backend-For-Frontend) route handlers for Workspace Project scoping and collaborator roles.
3. **`traefik/dynamic.yml` (Edge Proxy)**:
   - Listens on ports `80` (HTTP) and `443` (TLS wildcard `*.sandbox.localhost`).
   - Uses file-provider routing to direct sandbox traffic directly to host gateway `:8090`.
4. **Development Infrastructure (`docker-compose.dev.yml`)**:
   - **PostgreSQL 16**: Port `5434` (persists users, sandboxes, deployments, and encrypted credentials).
   - **Redis 7**: Port `6380` (ephemeral idle TTL keys `lr:<id>` and live metrics buffer).
   - **BuildKit Daemon**: Port `1234` (rootless OCI builder for Railpack).

---

## 2. Ingress & Wake-on-Request Gateway

### Why a Dedicated Gateway Instead of Docker Labels?
Direct Docker-label routing in edge proxies (e.g. Traefik Docker provider) has significant drawbacks:
- It requires exposing the host `/var/run/docker.sock` to the public proxy.
- Container stop/start events trigger noisy routing table re-computations and race conditions.
- It cannot intercept requests to stopped containers to implement zero-latency auto-waking.

In API Sandbox Links, Traefik uses a static wildcard file rule:
```yaml
http:
  routers:
    sandbox-wildcard:
      rule: "HostRegexp(`^[a-z0-9-]+\\.sandbox\\.localhost$`)"
      service: gateway-backend
  services:
    gateway-backend:
      loadBalancer:
        servers:
          - url: "http://host.docker.internal:8090"
```

### The Single-Flight Wake Mechanism

```
Inbound Request (*.sandbox.localhost)
           │
           ▼
    Gateway (:8090)
           │
           ├──▶ Sandbox RUNNING?
           │         │
           │         ├─ YES ──▶ Touch Redis `lr:<id>` TTL (900s) ──▶ Reverse Proxy to Container
           │         │
           │         └─ NO (HIBERNATED)
           │                 │
           │                 ▼
           │           Single-Flight Lock (coalesces duplicate bursts)
           │                 │
           │                 ├─▶ docker start sandbox_<subdomain>
           │                 ├─▶ WaitHealthy (poll internal_port TCP accept)
           │                 ├─▶ Update DB status -> `running`
           │                 └─▶ Forward Request
```

1. **Activity Heartbeat**: Every forwarded HTTP request touches Redis key `lr:<sandbox_id>` with TTL equal to `idle_timeout_seconds` (default 900s / 15 mins).
2. **Idle Reaping**: The background idle reaper periodically sweeps active sandboxes. If `lr:<sandbox_id>` has expired, the container is cleanly stopped and marked `hibernated`.
3. **Transparent Wake**: When traffic reaches a hibernated sandbox, the gateway:
   - Acquires a single-flight mutex for that sandbox (preventing boot storms if 20 concurrent requests arrive).
   - Issues `docker start <container_id>`.
   - Polls the sandbox's `internal_port` until TCP handshakes succeed (`WaitHealthy`).
   - Flips status in Postgres to `running`.
   - Reverse-proxies the original HTTP request seamlessly.

---

## 3. Builder Engine & Precedence Resolution

The platform supports pluggable builders with deterministic precedence:

```
                      Repository Deployment Request
                                    │
                                    ▼
                     Explicit User Config Provided?
                                    │
                     ├── YES ───────┴──────── NO ──┐
                     ▼                             ▼
            Run Explicit Builder          Dockerfile in Repo Root?
         (Dockerfile/Railpack/Nixpacks)            │
                                           ├── YES ┴──────── NO ──┐
                                           ▼                      ▼
                                   Dockerfile Builder      Railpack (Default)
                                  (Native BuildKit OCI)   (BUILDKIT_HOST:1234)
                                                                  │
                                                          Fallback if requested
                                                                  ▼
                                                          Nixpacks (Deprecated)
```

1. **Explicit Builder Selection**: If `.env` or deployment payload specifies `BUILDER=...`, that choice takes precedence.
2. **Native Dockerfile Precedence**: If the repository root contains a `Dockerfile`, the orchestrator automatically builds it directly using BuildKit, guaranteeing 100% standard Docker compatibility.
3. **Railpack (Default Cloud-Native Builder)**:
   - Modern, high-performance builder using Railpack buildpacks.
   - Compiles OCI image layers via BuildKit daemon on `tcp://127.0.0.1:1234`.
   - Provides aggressive layer caching across builds, reducing rebuild times from minutes to seconds.
4. **Nixpacks (Supported Deprecated Fallback)**:
   - Available via `BUILDER=nixpacks` during migration. Emits structured deprecation notices.

### Automatic Port Discovery (`DiscoverPort`)
Application frameworks often ignore the assigned `PORT` environment variable and bind their own hardcoded default (e.g. Next.js on `3000`, Flask on `5000`, Go on `8080`, Rails on `3000`).

To ensure zero false-positive deployment failures:
1. `DiscoverPort` inspects the planned port first.
2. If the planned port does not accept TCP within initial grace periods, it scans candidate ports:
   `[8080, 8000, 3000, 5000, 5001, 4000, 9000, 8888, 80]`
3. Whichever port responds to TCP health checks is dynamically persisted as `internal_port` and used by the gateway.

---

## 4. Sidecar Databases & Zero-Leak Isolation

When an application requires a database, the platform provisions a dedicated, lightweight sidecar container:

```
┌────────────────────────────────────────────────────────┐
│ Bridge Network: sandbox_<id[:8]>                        │
│                                                        │
│  ┌───────────────────────┐    ┌──────────────────────┐ │
│  │ Sandbox App Container │    │ Sidecar Database DB  │ │
│  │ Name: sandbox_<subdomain>│ ──▶│ Host: db_<id[:8]>    │ │
│  │ Port: internal_port   │    │ Port: 5432 or 3306   │ │
│  └───────────────────────┘    └──────────────────────┘ │
└────────────────────────────────────────────────────────┘
```

- **Automatic Dependency Detection**: `DetectDatabaseNeed` scans repository manifests (`package.json`, `go.mod`, `requirements.txt`, `Gemfile`, `schema.prisma`) for drivers (`pg`, `mysql2`, `psycopg2`, Prisma, GORM, Sequelize).
- **Network Isolation**: The sidecar runs on the sandbox's dedicated bridge network (`sandbox_<id[:8]>`). It has **no host port published** and is completely unreachable from other sandboxes or external traffic.
- **Environment Injections (`dbEnvFromURL`)**: Injects all standard database variable variants so apps connect out-of-the-box:
  - `DATABASE_URL`
  - `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`
  - `MYSQL_HOST`, `MYSQL_PORT`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_DATABASE`

---

## 5. Workspace Projects & Multi-Tenancy Scoping

Engineering teams organize disposable sandboxes into collaborative **Workspace Projects**:

```
                       Workspace Projects Store
                   (/tmp/api-sandbox-links/projects.json)
                                     │
           ┌─────────────────────────┴─────────────────────────┐
           ▼                                                   ▼
     Default Workspace                               Custom Workspace Project
   (id: "default-project")                             (id: "proj-microservices")
           │                                                   │
  Holds unassigned sandboxes                          Holds explicitly linked sandboxes
  and legacy environments                             via `sandboxIds: [...]`
```

### Scoping Rules (`filterSandboxesForProject`)
- Every sandbox created outside a specific workspace project belongs to the `default-project` ("Default Workspace").
- When viewing a custom workspace project, the BFF route filters environments strictly: only sandboxes whose IDs exist in `project.sandboxIds` are returned.
- Repositories and workspaces are isolated per-project, preventing cross-tenant leakage in multi-project setups.
- **Collaborator RBAC**:
  - `OWNER`: Full administrative rights (invite members, delete project, transfer ownership).
  - `EDITOR`: Deploy sandboxes, trigger redeployments, edit files in IDE, push Git commits.
  - `VIEWER`: Read-only access to preview URLs, build logs, and application metrics.

---

## 6. Live Log & Telemetry Pipeline

### Dual-Pipe WebSocket Logging
Docker interleaves `stdout` and `stderr` behind an 8-byte multiplex header when TTY is disabled. The backend uses `stdcopy.StdCopy` to maintain two independent streams:

| Stream | Backfill Policy | Rationale |
|---|---|---|
| **`stdout`** | Last 200 lines | High volume; replaying the entire history would delay connection without providing debugging value. |
| **`stderr`** | Entire history (`--tail all`) | Low volume; critical diagnostic errors emitted at container start must never be clobbered by high-volume stdout logs. |

### Real-Time Metrics Pipeline
```
Container (docker stats) ──▶ StatsCollector (1s tick)
                                 ├─▶ Live WebSocket Stream (:8080/stats/ws) ──▶ In-Browser Chart
                                 └─▶ Redis Buffer `stats:latest:<id>` (10s TTL)
                                          │
                                 Every 10s (StatsFlushWorker)
                                          ▼
                                 Postgres `resource_snapshots`
                                          │
                                 Pruned after 7 days (SnapshotReaper)
```

1. **Normalized CPU Utilization**: Docker's `cpu_usage.total_usage` is a monotonic counter. The backend computes rates between successive ticks (`computeCPUPercent`), ensuring accurate utilization percentages.
2. **True Memory Usage**: Reclaimable page cache (`inactive_file`) is subtracted from raw memory usage to reflect true application memory pressure.

---

## 7. Container Hardening & Google gVisor

Every sandbox container is deployed with hardened Linux containment:

| Control | Flag / Setting | Purpose |
|---|---|---|
| **Capability Dropping** | `CapDrop: ["ALL"]` | Strips all Linux capabilities by default. |
| **Privilege Escalation** | `SecurityOpt: ["no-new-privileges:true"]` | Prevents setuid binaries inside the container from acquiring elevated privileges. |
| **Process Supervision** | `Init: true` (tini) | Reaps zombie processes and handles POSIX signal forwarding. |
| **Google gVisor (Opt-in)** | `SANDBOX_RUNTIME="runsc"` | Replaces standard `runc` with gVisor's user-space kernel (`runsc`), virtualizing all syscalls and preventing container breakouts. |
| **Read-Only Rootfs** | `SANDBOX_HARDENED=true` | Mounts root filesystem read-only with a 64MB ephemeral `tmpfs` at `/tmp`. |
