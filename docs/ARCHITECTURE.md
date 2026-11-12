# Architecture

## Components

`api-sandbox-links/` contains two deployables plus local infra:

- **`backend/`** — a Go 1.26 binary with **two listeners**:
  - `:8080` — control plane: REST + WebSocket (chi router, see API.md).
  - `:8090` — gateway: reverse proxy with wake-on-request (see below).
- **`frontend/`** — Next.js 16 (App Router, Tailwind v4) dashboard & sandbox console.
- **`traefik/dynamic.yml`** — file-provider edge router for sandbox traffic.
- **`docker-compose.dev.yml`** — Postgres, Redis, Traefik (dev only; sandbox
  containers and per-sandbox DB sidecars are created by the backend at runtime).

## Sandbox traffic does not touch the API

```
Browser ── :80 (Traefik, file provider) ──> gateway :8090 ──> sandbox container :<internal_port>
Browser ── :3000 (Next.js) ──────────────> API :8080 (REST + WS)   <─ control plane
Browser ── ws :8080 /api/sandboxes/{id}/logs/ws | /stats/ws
```

## Why a gateway instead of per-container Traefik labels

The Docker provider is unreliable against modern daemons (Traefik 3.1 pins API
`1.24`; Docker 29 requires `>= 1.44`), so all sandbox routing is driven by the
**file provider**:

```yaml
rule: HostRegexp(`^[a-z0-9-]+\.sandbox\.localhost$`)
service: host.docker.internal:8090   # the backend's gateway listener
```

- Per-sandbox Traefik labels are still written as a **fallback** but are not
  relied on (`--providers.docker=false` in compose).
- Traefik reaches the host's `:8090` via `extra_hosts: host.docker.internal:host-gateway`.
- The gateway resolves the subdomain → sandbox, **wakes it if hibernating**, then
  reverse-proxies to `<container-ip>:<internal_port>` on the sandbox's private
  network.

## Wake-on-request (hibernation)

- Every request the gateway forwards for a sandbox refreshes a Redis TTL key
  `lr:<sandbox_id>` (TTL = `idle_timeout_seconds`, default 900).
- An **idle reaper** walks sandboxes; expired key → the container is stopped and
  the sandbox marked `hibernated`. State survives in the volume, so waking is fast.
- The gateway intercepts a request to a `hibernated` sandbox, **restarts the
  container**, polls TCP health on `internal_port` (`WaitHealthy`), flips status
  back to `running`, then forwards. Boot logs stream to the same WS frames
  channel, so the terminal shows them even for the waking request.

## Build pipeline

`orchestrator.DeploySandbox` (deploy.go), per deploy:

1. **Clone** — shallow `git clone --depth=1` of `repo_url@branch` into
   `SANDBOX_WORK_DIR/<sandbox-id>/`. On redeploy the volume is reused; a missing
   worktree is re-cloned from the repo.
2. **Sidecar database** — `maybeProvisionDatabase`: an explicit
   `database: auto|postgres|mysql|none` override wins; `auto` runs
   `DetectDatabaseNeed` over the cloned files (Prisma `schema.prisma`, Sequelize
   `config/config.json`, a `postgres://`/`mysql://` URL in `.env.example`, or driver
   deps in `package.json`/`go.mod`/`requirements.txt`: `pg`, `psycopg`,
   `gorm.io/driver/postgres`, `lib/pq`, `jackc/pgx`, `mysql2`,
   `go-sql-driver/mysql`). Sidecars persist across redeploys and are reused by
   container name `db_<sandbox-id[:8]>`.
3. **Plan & build** — pluggable `Builder` abstraction (`BUILDER=nixpacks|railpack`,
   defaults to `nixpacks`). The orchestrator runs `Builder.PlanAnalyze` followed by
   `Builder.BuildImage --name sandbox-<subdomain>`.
   `detectPort` reads `PORT`/`NIXPACKS_PORT` from the plan, else a provider
   default (node 3000, python 8080, go 8080, ruby 4567, php/rust 8000, static 80,
   fallback 3000).
4. **Run** — the app container starts on the private network `sandbox_<id[:8]>`
   with env: `PORT=<detected>`, the sandbox's decrypted env vars, and (when a
   sidecar exists) the expanded DB connection vars.
5. **Port discovery** — the builder-detected port can be wrong when an app
   hardcodes its own listener (e.g. TaskForge binds `:8080` instead of honoring
   `PORT`). `DiscoverPort` probes the planned port plus
   `[8080, 8000, 3000, 5000, 5001, 4000, 9000, 8888, 80]` until one accepts TCP
   (up to 90s), and **overrides `internal_port`** with whatever actually binds.
   Deploy fails outright only if no candidate port answers.
6. **Persist** — status `running`, `container_id`, `internal_port`,
   `detected_language`, and a `build_config` JSON (`{builder, provider, port, start}`).
   A `deployments` row goes `building → success` with a `log_excerpt` tail.
7. **Idle clock starts** — the gateway touches `lr:<id>` so a fresh sandbox gets
   its full idle grace period even with no traffic yet.

## Sidecar databases

- One container per sandbox on the **same private network**
  (`postgres:16-alpine` or `mysql:8`), named `db_<sandbox-id[:8]>` — that name is
  its DNS hostname inside the network. Image/volume removed if deploy fails.
- Connection URL: `postgres://sandbox:<pass>@db_<id>:5432/sandboxdb` (or
  `mysql://` on 3306), stored encrypted in `sandbox_databases`.
- At run time the app container gets **every spelling drivers might read**
  (`dbEnvFromURL`): `DATABASE_URL` plus generic `HOST/DBPORT/DBUSER/DBPASSWORD/
  DBNAME` and libpq `PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE`. This stops a
  repo's own `localhost`/`.env` defaults from silently pointing at a nonexistent
  DB (container env beats godotenv's file load).

## Containers

| Property | Value |
|---|---|
| Image | `sandbox-<subdomain>:latest` (nixpacks output) |
| Network | named bridge `sandbox_<id-first-8>` (created lazily by `ensureNetwork`) |
| Volume | `sandbox_data_<id>` mounted at `/app` — persists across redeploys; holds the working tree the editor and git see |
| Ports | none published — reachable only via the gateway over the private network |
| Limits | CPU 0.5, memory 256 MiB (defaults), `PidsLimit`; restart never configured (idle/hibernate is reaper-driven) |

## Reapers (worker goroutines) & collectors

- **Lifetime reaper** — permanently destroys sandboxes past `expires_at`
  (recorded as `status=expired`, `destruction_reason=lifetime_expired`).
- **Idle reaper** — hibernates sandboxes whose Redis `lr:` key lapsed.
- **Warnings reaper** — writes lifetime-warning rows for the 24h/1h/5m ladders.
- **Snapshot reaper** — hourly, deletes `resource_snapshots` rows older than the
  7-day retention window. The table is append-only, so without this it is the
  only part of the system that grows without bound.
- **Stats collector** — per-running-sandbox `docker stats` stream; see below.
- **Stats flush worker** — every `orchestrator.StatsFlushInterval` (10s), copies
  the newest live sample for each sandbox from Redis into `resource_snapshots`.

## Live log & metrics pipeline

Two live channels feed the sandbox detail page. They share their authorization,
their "is this sandbox actually running" check, and their end-of-stream
reporting, but their data paths are different — one is a straight pipe from
Docker, the other is a fan-out with a persistence side-channel.

**Logs — two straight pipes, one per fd.** `docker logs --follow --timestamps`
against the sandbox container, opened once for stdout and once for stderr, and
demuxed by `stdcopy.StdCopy` (Docker interleaves both fds behind an 8-byte header
whenever a container has no TTY, and hand-parsing that framing is the usual
source of garbled output). One pump goroutine per connection, each bound to the
socket's context, so closing the panel closes both Docker streams. The backend
never retries; the frontend reconnects with backoff, up to a manual button.

Reconnection has two distinct stop conditions, and conflating them is what makes
this channel look broken when it is not. A socket that **never opened** failed its
handshake — a bad session, or a server that is not running — and a bad session is
probed once and reported as *"sign in again"* rather than retried. A socket that
opened and then dropped is a real network or server fault and gets the normal
5-attempt backoff. See the close-code table in API.md.

The two connections use different tails, which is deliberate. Docker applies
`--tail` to the *combined* log before filtering by fd, and orders that backfill
stderr-first, so a tail smaller than the whole log discards **all** of stderr
while trimming stdout normally. Measured against Docker v28 on a container with
400 stdout lines and 9 stderr lines:

| request | backfill |
|---|---|
| stdout only, `--tail 200` | the last 200 stdout lines — correct |
| stderr only, `--tail 200` | nothing — the cut lands past every stderr line |
| stderr only, `--tail all` | all 9 stderr lines — correct |
| both fds, one connection, `--tail 200` | 200 stdout lines, no stderr |

The last row is the trap the obvious single-connection implementation walks into:
a chatty app hides its one error line from exactly the user who opened the log
panel to find it. Hence stdout (high volume, ordered last) is capped at
`logTail`, and stderr (low volume, ordered first) is replayed in full. The
unbounded stderr replay is bounded in practice by back-pressure — the pump
writes into a fixed-size pipe, so a large stderr history parks the pump rather
than growing memory. Regression-tested in
`internal/orchestrator/logs_integration_test.go`
(`TestStreamContainerLogsBackfillsStderrDespiteChattyStdout`).

**Metrics — fan-out with a persistence side-channel.**

```
container ──docker stats stream──▶ StatsCollector
                                     ├─▶ WebSocket subscribers (~1/s)  ──▶ browser
                                     └─▶ Redis  stats:latest:{id}  (TTL 10s)
                                              │
                                    every 10s (StatsFlushWorker)
                                              ▼
                                      Postgres resource_snapshots
                                              │
                                    hourly prune (SnapshotReaper, 7d)
                                              ▼
                                  GET /stats/history?range=1h|6h|24h|7d
```

The 1s feed exists to drive a live readout; only its newest value is worth
persisting, so Redis holds a single short-lived key per sandbox rather than a
growing buffer, and the flush worker reads that key on its own slower cadence.
The 10s TTL is the safety property: if a container's stats stream dies, the key
expires rather than serving a frozen reading as though it were live. Postgres
therefore never sits on the hot path of the live feed.

See `docs/diagrams/sequence-live-stats.mmd` for the full sequence.

> **The CPU% gotcha.** Docker never reports a CPU percentage.
> `cpu_usage.total_usage` and `system_cpu_usage` are monotonic nanosecond
> counters, so only the *difference* between two samples is a rate. Reading the
> absolute totals yields lifetime-average utilisation — a plausible number that
> is wrong by orders of magnitude right after a container starts. Every sample
> carries the previous one in `PreCPUStats`, which is the pair
> `computeCPUPercent` consumes. Do not "simplify" it into a direct ratio.

Two smaller normalization decisions worth knowing before reading the numbers:

- **Memory** is `usage` minus reclaimable page cache. cgroup v1 reports that as
  `stats.cache`; cgroup v2 reports `stats.inactive_file`. Only `inactive_file` is
  subtracted on v2, never `total_inactive_file` (which includes resident
  `active_file`). Raw `usage` overstates what the app is holding.
- **Network** is summed across every interface Docker reports, because it makes
  no promise that any one of them is carrying traffic. The stored counters are
  per-sample deltas, not lifetime totals.

## Auth & security posture

- Session cookie `asl_session` = AES-256-GCM ciphertext of the GitHub numeric ID
  (key from `SECRETS_ENCRYPTION_KEY`); decryption doubles as authenticity.
  `requireUser` middleware decrypts it and looks the user up; ownership is
  enforced per-route through `authorizeSandboxAccess` (wrapped by
  `requireOwnedSandbox` for REST, and by the WebSocket upgrade for the live
  sockets, which report a refusal as close code 1008). **Dev auth**
  (`GITHUB_CLIENT_ID` empty) fabricates the seeded `devuser` instead.
- User GitHub tokens and sandbox env vars are encrypted at rest with the same box;
  tokens decrypt only to push commits.
- Fork copies the parent's working tree into a new volume (`cp --reflink` when
  available, else plain copy), reassigns ownership, and records `parent_sandbox_id`.
  The parent's deployments/commits stay untouched.
- `TeardownSandbox` cascades: container → sidecar DB → volume → network, stops
  that sandbox's stats collector, and deletes its `resource_snapshots`. The
  metadata row is kept as a tombstone, and so are its `deployments`; a destroyed
  sandbox's page still renders, but its charts come back empty because the
  samples are gone with the runtime. The snapshot delete is not redundant with
  the `ON DELETE CASCADE` on `resource_snapshots.sandbox_id`: that foreign key
  only fires when the `sandboxes` row itself is deleted, which this platform
  never does. Without an explicit delete every destroyed sandbox would leave up
  to 7 days of samples for the snapshot reaper to find, and the graph would keep
  implying the sandbox had been alive the whole time.
- Editor edits commit ONLY onto a new `sandbox-edits-<subdomain>` branch unless
  the user opts into `direct_to_original` + `force`.

## Tech stack

- Go 1.26, `chi` router, GORM (Postgres, hand-written migrations),
  `redis/go-redis/v9`, `go-git` (clone/push), docker Go SDK, `nhooyr/websocket`.
- Next.js 16 App Router, Tailwind v4, `@monaco-editor/react`, `@xterm/xterm`,
  `recharts`, TanStack Query.