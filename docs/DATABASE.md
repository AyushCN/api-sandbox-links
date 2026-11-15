# Database & Persistence Architecture

The platform uses a layered persistence architecture:
1. **PostgreSQL 16**: Relational storage for users, sandboxes, deployments, sidecar databases, and encrypted secrets.
2. **Redis 7**: Ephemeral hot-state store for idle activity TTLs and live resource snapshot buffers.
3. **Workspace Projects Store**: Fast, file-backed JSON store (`/tmp/api-sandbox-links/projects.json`) for collaborative multi-tenancy and project scoping.

---

## 1. PostgreSQL Schema & Migrations

PostgreSQL is accessed via GORM in the Go backend. Database migrations are strictly managed through sequential SQL files located in [`backend/migrations/`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/migrations):

- `0001_init.up.sql`: Initial schema (users, repositories, sandboxes, env vars, databases, deployments, snapshots, audit log).
- `0002_snapshot_dedup.up.sql`: Unique constraint on `resource_snapshots(sandbox_id, recorded_at)` ensuring idempotent 10s metric flushes.
- `0003_lifetime_starts_on_activation.up.sql`: Adds `sandboxes.activated_at` to ensure sandbox lifetime begins only when status transitions to `running`.

### Local Development Connection
The Postgres container runs on host port **`5434`** (mapped to container port `5432` in `docker-compose.dev.yml` to prevent local port conflicts):
```
postgres://user:pass@localhost:5434/sandbox_platform?sslmode=disable
```

---

## 2. Relational Table Schema Reference

### Table Overviews

| Table | Primary Purpose | Key Columns & Indexes |
|---|---|---|
| `users` | Authenticated developers | `id uuid pk`, `github_id bigint unique`, `username`, `email`, `github_token bytea` (AES-256-GCM encrypted) |
| `repositories` | Registered GitHub repositories | `id uuid pk`, `user_id fk`, `github_url`, `default_branch`, `unique(user_id, github_url)` |
| `sandboxes` | Container environments | `id uuid pk`, `subdomain unique`, `status`, `container_id`, `internal_port`, `idle_timeout_seconds`, `activated_at`, `expires_at`, `build_config jsonb` |
| `sandbox_env_vars` | Decrypted runtime variables | `id uuid pk`, `sandbox_id fk`, `key`, `value bytea` (encrypted), `unique(sandbox_id, key)` |
| `sandbox_databases` | Per-sandbox sidecar DBs | `id uuid pk`, `sandbox_id fk`, `engine` (`postgres\|mysql`), `container_id`, `connection_url bytea` (encrypted) |
| `sandbox_lifetime_warnings` | Deduplicated alert thresholds | `id uuid pk`, `sandbox_id fk`, `threshold` (`24h\|1h\|5m`), `sent_at`, `unique(sandbox_id, threshold)` |
| `deployments` | Build & deploy history | `id uuid pk`, `sandbox_id fk`, `trigger` (`initial\|save_redeploy\|github_push`), `status`, `log_excerpt` |
| `resource_snapshots` | 10s container metric samples | `id bigint pk`, `sandbox_id fk`, `cpu_percent`, `memory_used_bytes`, `recorded_at`, `unique(sandbox_id, recorded_at)` |
| `file_edits` | In-browser IDE change audits | `id uuid pk`, `sandbox_id fk`, `file_path`, `edited_by fk`, `edited_at`, `pushed_to_github` |

### GORM Table-Name Mappings
GORM pluralizes Go structs unless explicitly overridden via `TableName()`. The following mappings are configured in [`backend/internal/db/models.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/db/models.go):

| Go Model Struct | Physical Database Table |
|---|---|
| `EnvVar` | `sandbox_env_vars` |
| `LifetimeWarning` | `sandbox_lifetime_warnings` |
| `Repository` | `repositories` |
| `SandboxDatabase` | `sandbox_databases` |

---

## 3. Lifetime Accounting (`activated_at`)

A sandbox's expiration deadline is a guarantee of usable uptime. Build times—which can take 1 to 3 minutes for large repositories—must **never** consume the user's allocated lifetime.

1. **Creation**: When a sandbox is initialized, `expires_at` is provisionally set to `created_at + lifetime_seconds` as a fallback timeout in case the build hangs.
2. **Activation**: The moment the sandbox successfully builds and its container reaches `running`, the orchestrator executes an atomic conditional update:
   ```sql
   UPDATE sandboxes
      SET status = 'running',
          activated_at = NOW(),
          expires_at = NOW() + (lifetime_seconds || ' seconds')::interval
    WHERE id = $1 AND activated_at IS NULL;
   ```
3. **Idempotency**: The `activated_at IS NULL` condition ensures that subsequent redeployments or container restarts do not improperly reset the original lifetime expiration.

---

## 4. Redis Hot-State Storage

Redis operates strictly as an ephemeral cache and signaling bus; no permanent data relies on Redis:

| Key Pattern | Data Type | TTL | Purpose |
|---|---|---|---|
| `lr:<sandbox_id>` | String timestamp | `idle_timeout_seconds` (default 900s) | Refreshed on every HTTP request by the Gateway `:8090`. Expiration triggers the idle reaper to hibernate the container. |
| `stats:latest:<sandbox_id>` | JSON object | `10s` | Buffer written by the real-time `docker stats` collector. Flushed to Postgres `resource_snapshots` every 10s by `StatsFlushWorker`. |

---

## 5. Workspace Projects Data Store

Workspace Projects, collaborative member roles, and project-to-sandbox associations are managed through a high-performance JSON store at `/tmp/api-sandbox-links/projects.json`.

### Project Entity Structure
```json
{
  "id": "proj-microservices",
  "name": "Payment Microservices",
  "description": "Internal payment APIs and webhook testing",
  "owner": "AyushCN",
  "collaborators": [
    {
      "id": "collab-1",
      "userId": "AyushCN",
      "username": "AyushCN",
      "email": "ayush@example.com",
      "role": "OWNER"
    }
  ],
  "repositories": [
    {
      "id": "repo-1",
      "name": "payment-api",
      "url": "https://github.com/org/payment-api",
      "defaultBranch": "main"
    }
  ],
  "sandboxIds": [
    "0d9a52df-fe54-45dc-8b73-d4b936f2e55b"
  ],
  "createdAt": "2026-11-15T06:00:00.000Z"
}
```

### Multi-Tenancy Scoping (`filterSandboxesForProject`)
1. **The Default Project (`default-project`)**: Captures all sandboxes not explicitly bound to any custom workspace project.
2. **Custom Projects**: Only surface sandboxes whose IDs are explicitly recorded in `project.sandboxIds`.
3. **Automatic Persistence**: Changes made via the UI (creating projects, inviting members, linking sandboxes) immediately sync to disk with atomic write locks.
