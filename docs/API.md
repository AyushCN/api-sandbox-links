# API Reference — Control Plane & BFF

The platform exposes two coordinated API layers:
1. **Control Plane (`http://localhost:8080`)**: Core Go backend handling sandbox orchestration, Docker lifecycle, database sidecars, and live WebSockets.
2. **Frontend BFF (`http://localhost:3000/api`)**: Next.js App Router handlers providing Workspace Project scoping, team member management, and local Git operations.

---

## 1. Authentication & Session Conventions

- **Session Cookie (`asl_session`)**: Contains an AES-256-GCM encrypted token of the authenticated user's ID (`SECRETS_ENCRYPTION_KEY`). Set via `httpOnly` cookie.
- **Dev Auth Mode (Default)**: If `GITHUB_CLIENT_ID` is unset, all requests are automatically assigned the local `devuser` (ID `1`), allowing complete access without requiring an OAuth roundtrip.
- **Standard Error Format**: Errors return JSON with `{ "error": "<detailed message>" }` and appropriate HTTP status codes.

---

## 2. Core Sandboxes API (`:8080`)

### The Sandbox Object
Returned by all sandbox endpoints (`sandboxJSON`):

```jsonc
{
  "id": "6a90fec3-8aad-4024-b83e-1cb76d867c9f",
  "subdomain": "node-js-sample-7e9fd5",
  "url": "http://node-js-sample-7e9fd5.sandbox.localhost",
  "status": "running",                  // queued | building | running | hibernated | failed | expired | deleted
  "branch": "master",
  "parent_sandbox_id": null,            // UUID if cloned/forked
  "container_id": "8c2f10b...",
  "internal_port": 3000,
  "idle_timeout_seconds": 900,
  "last_request_at": "2026-11-15T06:20:00Z",
  "lifetime_seconds": 86400,
  "activated_at": "2026-11-15T06:24:50Z", // Lifetime countdown begins when status reaches running
  "expires_at": "2026-11-16T06:24:50Z",
  "destroyed_at": null,
  "destruction_reason": null,           // lifetime_expired | user_requested
  "detected_language": "nodejs",
  "build_config": {
    "builder": "railpack",
    "provider": "node",
    "port": 3000,
    "start": "npm start"
  },
  "created_at": "2026-11-15T06:22:00Z",
  "updated_at": "2026-11-15T06:24:50Z"
}
```

### Sandbox Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check: `{"status":"ok","time":"...","dev_auth":true}` (Public) |
| `GET` | `/api/me` | Current authenticated user profile |
| `POST`| `/api/auth/logout` | Clears `asl_session` cookie (Public, idempotent) |
| `GET` | `/api/sandboxes/` | List all sandboxes for the authenticated user |
| `POST`| `/api/sandboxes/` | Create & queue a new sandbox deployment (Body below) |
| `GET` | `/api/sandboxes/{id}` | Retrieve sandbox status and metadata |
| `POST`| `/api/sandboxes/{id}/actions/extend` | Extend sandbox lifetime (`additional_seconds`) |
| `POST`| `/api/sandboxes/{id}/actions/destroy`| Permanently destroy sandbox and tear down sidecars |
| `POST`| `/api/sandboxes/{id}/fork` | Clone sandbox working tree into a new sibling sandbox |
| `POST`| `/api/sandboxes/{id}/deploy` | Trigger a manual redeployment from current working tree |
| `GET` | `/api/sandboxes/{id}/deployments` | List historical deployments and build logs |
| `GET` | `/api/sandboxes/{id}/warnings` | Check remaining lifetime and expiration warnings |
| `GET` | `/api/sandboxes/{id}/stats/history` | Historical metrics (`?range=1h\|6h\|24h\|7d`) |

### Create Sandbox — `POST /api/sandboxes/`
```jsonc
{
  "repo_url": "https://github.com/heroku/node-js-sample", // Required (GitHub repository URL)
  "branch": "master",                                     // Optional (defaults to repo default)
  "lifetime_seconds": 86400,                              // Optional (300 to 604800, default 86400)
  "idle_timeout_seconds": 900,                            // Optional (default 900)
  "database": "auto",                                     // "auto" | "postgres" | "mysql" | "none"
  "env_vars": { "API_KEY": "sandbox_secret_key" }         // Encrypted with AES-256-GCM at rest
}
```

---

## 3. In-Browser Cloud IDE Endpoints (`:8080`)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/sandboxes/{id}/files` | List working-tree file tree (`.git` excluded) |
| `GET` | `/api/sandboxes/{id}/file?path=<rel>` | Retrieve file contents (supports text and base64 binaries) |
| `PUT` | `/api/sandboxes/{id}/file` | Save file content and automatically enqueue a save-redeploy |
| `POST`| `/api/sandboxes/{id}/commit` | Commit working tree and push to a remote GitHub branch |

### Save File — `PUT /api/sandboxes/{id}/file`
```jsonc
{
  "path": "server.js",
  "content": "const express = require('express');\nconst app = express();\n...",
  "is_base64": false
}
```

### Commit & Push — `POST /api/sandboxes/{id}/commit`
```jsonc
{
  "message": "Update database connection string",
  "branch": "sandbox-edits-node-js-sample", // Defaults to sandbox-edits-<subdomain>
  "direct_to_original": false,              // Set true to push directly to original branch
  "force": false
}
```

---

## 4. Workspace Projects API (`:3000/api/projects`)

Workspace projects provide team-based organization, repository grouping, and environment scoping.

### The Project Object
```jsonc
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
      "role": "OWNER"                       // OWNER | EDITOR | VIEWER
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
  "sandboxIds": ["0d9a52df-fe54-45dc-8b73-d4b936f2e55b"],
  "createdAt": "2026-11-15T06:00:00.000Z"
}
```

### Project Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/projects` | List all workspace projects |
| `POST`| `/api/projects` | Create a new workspace project |
| `GET` | `/api/projects/{id}` | Get project details, members, and scoped sandboxes |
| `POST`| `/api/projects/{id}/edit` | Update project name and description |
| `GET` | `/api/projects/{id}/members` | List project collaborators and their roles |
| `POST`| `/api/projects/{id}/invite` | Invite a collaborator (`username`, `email`, `role`) |
| `DELETE`| `/api/projects/{id}/collaborators/{userId}` | Remove a collaborator from the project |
| `GET` | `/api/projects/{id}/repositories` | List repositories associated with this project |
| `POST`| `/api/projects/{id}/repositories` | Associate a new repository with this project |
| `GET` | `/api/projects/{id}/workspaces` | List sandboxes scoped exclusively to this project |
| `POST`| `/api/projects/{id}/link` | Link an existing sandbox ID to this project |
| `GET` | `/api/projects/{id}/change-requests` | List change requests in this project |

---

## 5. Environments & Git Management API (`:3000/api/environments`)

Provides project-scoped sandbox filtering and local Git branch controls:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/environments?projectId={id}` | List sandboxes scoped strictly to the specified project |
| `GET` | `/api/environments/{id}` | Get full environment state (subdomain, sidecar DB, ports) |
| `GET` | `/api/environments/{id}/git/branches` | List local and remote Git branches in worktree |
| `POST`| `/api/environments/{id}/git/checkout` | Switch branch (`{ "branch": "feat-ui" }`) |
| `POST`| `/api/environments/{id}/git/branch` | Create new branch (`{ "branch": "new-feature" }`) |
| `POST`| `/api/environments/{id}/git/pull` | Pull latest changes from remote |
| `GET` | `/api/environments/{id}/git/status` | Current modified files and unstaged changes |
| `GET` | `/api/environments/{id}/git/log` | Recent commit history |
| `GET` | `/api/environments/{id}/docker-logs` | Live stream of container runtime logs |

---

## 6. Real-Time WebSocket Streams (`:8080`)

All WebSocket endpoints authenticate via the `asl_session` cookie prior to protocol upgrade.

### Live Container Logs — `GET /api/sandboxes/{id}/logs/ws`
Streams container `stdout` and `stderr` line-by-line using demultiplexed Docker framing:

```jsonc
// Log line frame
{
  "type": "log",
  "stream": "stdout",                           // "stdout" | "stderr"
  "timestamp": "2026-11-15T06:24:50.123Z",
  "line": "[INFO] Listening at: http://0.0.0.0:5000"
}

// Inactive container frame
{
  "type": "unavailable",
  "reason": "Container is hibernated due to inactivity. Click Restart to wake it."
}

// Termination frame
{
  "type": "closed",
  "reason": "Container process exited cleanly."
}
```

### Real-Time Resource Metrics — `GET /api/sandboxes/{id}/stats/ws`
Emits normalized container metrics approximately once per second:

```jsonc
{
  "type": "stats",
  "cpu_percent": 3.42,
  "memory_used_bytes": 48234496,
  "memory_limit_bytes": 536870912,
  "network_rx_bytes": 10240,
  "network_tx_bytes": 8192,
  "recorded_at": "2026-11-15T06:25:01Z"
}
```

### WebSocket Close Codes & Reconnection Contract

| Code | Meaning | Client Behavior |
|---|---|---|
| `1000` | Normal termination with explanatory `closed`/`unavailable` frame. | Closes cleanly; displays server reason. |
| `1008` | Policy violation (unauthorized / not sandbox owner). | Stops retrying; displays authorization alert. |
| `1006` | Abnormal closure (backend unreachable or network drop). | Exponential backoff (1s, 2s, 4s, 8s, up to 15s; max 5 attempts). |
| `1011` | Unexpected internal server error. | Reconnects with exponential backoff. |
