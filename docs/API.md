# API — `:8080` control plane

Base URL: `http://localhost:8080`. All routes except `/api/health`, the auth
start/callback, and `POST /api/auth/logout` require the session cookie
`asl_session` (set by GitHub OAuth). In **dev auth mode** (no
`GITHUB_CLIENT_ID`) every `requireUser` request is auto-assigned the seeded
`devuser`, so the whole surface is testable without logging in.

`POST /api/auth/logout` is deliberately **not** behind `requireUser`: its whole
job is to clear the session cookie, so requiring a valid session to reach it
means a caller whose session has already gone bad cannot clear the very cookie
that is causing it. It is idempotent and never touches the database, so it
answers `200 { "ok": true }` whether the cookie was valid, stale, or absent.
Clearing a cookie only affects the caller's own browser, so there is nothing to
authorize.

Conventions:

- JSON in/out. Errors are `{ "error": "<message>", "error_code": "<code>" }`
  with an appropriate status.
- Statuses: `200` ok, `201` created, `202` accepted, `400` validation, `401`
  auth, `404` missing, `409` conflict, `501`/`502` upstream.
- The container's actual listening port is **discovered** at deploy time, so
  `internal_port` may differ from the nixpacks-detected port (apps that hardcode
  a port, e.g. Go apps on 8080). See ARCHITECTURE.md.

## Error codes

The following machine-readable error codes are returned with every JSON error response:

| Code | Meaning |
|---|---|
| `auth_unauthorized` | Not signed in / invalid session |
| `validation_missing_param` | Required parameter missing |
| `validation_invalid_repo_url` | Invalid GitHub repository URL |
| `validation_invalid_services` | Invalid services selection |
| `validation_invalid_range` | Invalid range parameter (must be 1h, 6h, 24h, or 7d) |
| `validation_positive` | Parameter must be positive (e.g. additional_seconds) |
| `validation_range` | Parameter out of valid range (e.g. idle_timeout_seconds 600-86400) |
| `sandbox_destroyed` | Sandbox has been destroyed |
| `sandbox_destroyed_cannot_edit` | Cannot edit a destroyed sandbox |
| `only_destroyed_recreate` | Only destroyed sandboxes can be recreated (try recreate instead) |
| `only_stop_destroyed` | Cannot stop a destroyed sandbox |
| `only_start_destroyed` | Cannot start a destroyed sandbox |
| `only_running_stop` | Only running sandboxes can be stopped |
| `only_hibernated_start` | Only hibernated sandboxes can be started |
| `analysis_failed` | Analysis of the repository failed |
| `deploy_list_failed` | Could not list deployments |
| `lifetime_extend_failed` | Could not extend lifetime |
| `nothing_to_commit` | Nothing to commit (no changes) |
| `no_changes_to_commit` | No changes in the sandbox working directory |
| `load_repository_failed` | Could not load the repository |
| `read_repository_url_failed` | Could not read the repository URL |
| `read_settings_failed` | Could not read the previous sandbox settings |
| `allocate_subdomain_failed` | Could not allocate subdomain |
| `recreate_failed` | Could not recreate sandbox |
| `recreate_settings_failed` | Could not copy settings during recreation |
| `commit_failed` | Could not commit changes |
| `read_github_creds_failed` | Could not read GitHub credentials |
| `list_sandboxes_failed` | Could not list sandboxes |
| `store_repository_failed` | Could not store repository |
| `create_sandbox_failed` | Could not create sandbox |
| `create_sandbox_settings_failed` | Could not save sandbox settings during creation |
| `working_dir_not_found` | Working directory not found (sandbox building or destroyed) |
| `working_dir_building` | Working directory still building |
| `working_dir_not_found_2` | Working directory not found |
| `git_editable` | Git internals are not editable |
| `read_existing_settings_failed` | Could not read existing settings |
| `save_settings_failed` | Could not save settings |
| `load_parent_repo_failed` | Could not load the parent repository |
| `read_parent_repo_url_failed` | Could not read the parent repository URL |
| `create_fork_failed` | Could not create fork |
| `copy_fork_settings_failed` | Could not copy fork settings |
| `sandbox_destroyed_cannot_edit` | This sandbox has been destroyed and cannot be edited |
| `read_warnings_failed` | Could not read warnings |
| `query_history_failed` | Could not query resource history |

Dynamic errors (those containing `err.Error()`) may not have a static error code.

## Sandbox object

Every endpoint that returns a sandbox uses this shape (keys from `sandboxJSON`):

```jsonc
{
  "id": "6a90fec3-8aad-4024-b83e-1cb76d867c9f",
  "subdomain": "node-js-sample-7e9fd5",
  "url": "http://node-js-sample-7e9fd5.sandbox.localhost",
  "status": "running",                 // queued|building|running|hibernated|failed|expired|deleted
  "branch": "master",
  "parent_sandbox_id": null,           // set on forks
  "container_id": "…",
  "internal_port": 3000,
  "idle_timeout_seconds": 900,
  "last_request_at": "…",
  "lifetime_seconds": 86400,
  "activated_at": null,                 // set when the sandbox first goes live
  "expires_at": "…",
  "destroyed_at": null,                // tombstone when destroyed
  "destruction_reason": null,          // lifetime_expired|user_requested
  "detected_language": "nodejs",
  "build_config": { "provider": "node", "port": 3000, "start": "…" },
  "created_at": "…",
  "updated_at": "…"
}
```

## Sandbox object

Every endpoint that returns a sandbox uses this shape (keys from `sandboxJSON`):

```jsonc
{
  "id": "6a90fec3-8aad-4024-b83e-1cb76d867c9f",
  "subdomain": "node-js-sample-7e9fd5",
  "url": "http://node-js-sample-7e9fd5.sandbox.localhost",
  "status": "running",                 // queued|building|running|hibernated|failed|expired|deleted
  "branch": "master",
  "parent_sandbox_id": null,           // set on forks
  "container_id": "…",
  "internal_port": 3000,
  "idle_timeout_seconds": 900,
  "last_request_at": "…",
  "lifetime_seconds": 86400,
  "activated_at": null,                 // set when the sandbox first goes live
  "expires_at": "…",
  "destroyed_at": null,                // tombstone when destroyed
  "destruction_reason": null,          // lifetime_expired|user_requested
  "detected_language": "nodejs",
  "build_config": { "provider": "node", "port": 3000, "start": "…" },
  "created_at": "…",
  "updated_at": "…"
}
```

## Auth

| Method | Path | Description |
|---|---|---|
| GET | `/api/auth/github` | Redirects to GitHub consent. `501` when OAuth is unconfigured (dev auth). |
| GET | `/api/auth/github/callback` | Exchanges code, upserts user, sets `asl_session` (30d, httpOnly), redirects to `FRONTEND_URL/dashboard?authed=1`. |
| GET | `/api/me` | `{ "github_id": 1, "username": "devuser", "avatar_url": "", "dev": true }`. |
| POST | `/api/auth/logout` | Clears the cookie → `{ "ok": true }`. **Public**, not behind `requireUser`; idempotent. |
| GET | `/api/health` | `{ "status": "ok", "time": "…", "dev_auth": true }` — no auth. |

## Sandboxes

| Method | Path | Description |
|---|---|---|
| GET | `/api/sandboxes/` | `{ "sandboxes": [<sandbox>, …] }`, newest first; tombstones included. |
| POST | `/api/sandboxes/` | **Create.** Body below. `202` with the queued sandbox; deploys async. |
| GET | `/api/sandboxes/{id}` | One sandbox. |
| POST | `/api/sandboxes/{id}/actions/extend` | **Extend lifetime.** Body below. Returns `{ sandbox, extended_by, at_cap }`. |
| POST | `/api/sandboxes/{id}/actions/destroy` | Permanent teardown → sandbox with `status: deleted`. |
| POST | `/api/sandboxes/{id}/fork` | Clone into a sibling container under a new owner. Returns the new sandbox. |
| GET | `/api/sandboxes/{id}/deployments` | `{ "deployments": [<deployment>, …] }`. |
| POST | `/api/sandboxes/{id}/deploy` | Manual redeploy from the current working tree → `202 { "redeploying": true }`. |
| GET | `/api/sandboxes/{id}/warnings` | Lifetime-warning state. Shape below. |
| GET | `/api/sandboxes/{id}/stats/history?range=1h` | Persisted metrics for a historical range. Shape below. |
| GET | `/api/sandboxes/{id}/logs/ws` | **Live logs** (WebSocket). See "WebSockets" below. |
| GET | `/api/sandboxes/{id}/stats/ws` | **Live metrics** (WebSocket). See "WebSockets" below. |

### Create — `POST /api/sandboxes/`

```jsonc
{
  "repo_url": "https://github.com/heroku/node-js-sample",   // required, GitHub only
  "branch": "master",                                        // optional, defaults to repo default
  "lifetime_seconds": 86400,                                 // optional, default 86400; min 300, max 7d
  "idle_timeout_seconds": 900,                               // optional, default 900; <60 → 900
  "database": "auto",                                        // auto|postgres|mysql|none
  "env_vars": { "KEY": "value" }                             // encrypted at rest, injected into container
}
```

Validation: `repo_url` is required and must parse as a GitHub repo;
`database` must be one of the four values above; `lifetime_seconds` is the total
allowance, and the countdown only starts once the sandbox reaches `running` (see
`activated_at`), so build time is not billed against it.

`database` unknown value → 400; lifetime out of `[300, MAX_LIFETIME]` → 400 with
the valid range in the message.

### Extend — `POST /api/sandboxes/{id}/actions/extend`

Body **requires** `additional_seconds` (positive integer):

```jsonc
{ "additional_seconds": 86400 }
```

`expires_at` is pushed forward but never past `MAX_LIFETIME` (default 7 days)
measured from the instant the lifetime clock started — `activated_at` where the
sandbox has gone live, `created_at` otherwise. Anchoring on activation is what
stops a slow build from eating into the extension allowance. Response:

```jsonc
{ "sandbox": { … }, "extended_by": 86400, "at_cap": false }
```

Destroyed sandboxes → 409. Missing/`<=0` body → 400.

### Warnings — `GET /api/sandboxes/{id}/warnings`

```jsonc
{
  "sandbox_id": "…",
  "expires_at": "…",
  "remaining_seconds": 84600,
  "warnings": [
    { "threshold": "24h", "title": "…", "message": "…" }
  ]
}
```

### Stats history — `GET /api/sandboxes/{id}/stats/history`

`?range=` selects the window and is validated against a fixed allowlist — `1h`
(default), `6h`, `24h`, `7d`. Anything else is `400`. Arbitrary durations are
rejected rather than parsed because an unvalidated window becomes an unbounded
scan of a table that only ever grows; each allowed window also carries its own
row cap so a 7-day request does not try to draw 60k points.

The cap takes the **newest** rows in the window and returns them oldest-first.
A 24h window holds ~8,600 rows against a 1,000-row cap, so capping the oldest
rows instead would render a stale two-hour slice from the start of the window
and hide everything since.

```jsonc
{
  "range": "1h",
  "since": "2026-09-26T09:00:00Z",
  "until": "2026-09-26T10:00:00Z",
  "snapshots": [
    { "id": 1, "sandbox_id": "…", "cpu_percent": 12.4, "memory_used_bytes": 8125000,
      "memory_limit_bytes": 268435456, "network_rx_bytes": 102400,
      "network_tx_bytes": 51200, "recorded_at": "…" }
  ]
}
```

Snapshots are recorded every 10 seconds while a sandbox runs and are pruned
after 7 days, so a range wider than a sandbox's uptime simply returns fewer
rows. The frontend shows "Not enough data yet" for a range with fewer than two
rows rather than rendering an empty axis.

## File editor

| Method | Path | Description |
|---|---|---|
| GET | `/api/sandboxes/{id}/files` | Working-tree file tree (`.git` excluded). `{ "files": [ { "path": "…", "dir": false } ] }`. |
| GET | `/api/sandboxes/{id}/file?path=<rel>` | `{ "path": "…", "content": "…", "binary": false, "encoding": "" }`. Paths are `safeJoin`-validated; traversal → 400, empty tree/volume → 404. |
| PUT | `/api/sandboxes/{id}/file` | Body `{ "path": "…", "content": "…", "is_base64": false }`. Writes into the volume and **enqueues a save-redeploy**. `{ "saved": true, "redeploying": true }`. |

## Commit & push

| Method | Path | Description |
|---|---|---|
| POST | `/api/sandboxes/{id}/commit` | Body `{ "message": "…", "branch": "…", "direct_to_original": false, "force": false }`. Commits the whole working tree and pushes. Default branch `sandbox-edits-<subdomain>` (never touches your real branch); `direct_to_original + force` explicitly overrides. Returns `{ "commit": "…", "branch": "…", "direct": false, "pushed_at": "…" }`. |

Requires a real GitHub token (OAuth sign-in). `409` when there is nothing to
commit (`git.ErrNoChanges`); `502` if the stored token doesn't authenticate.

## WebSockets (authed, text frames with JSON)

Both live sockets share one shape: a `type` discriminator plus that type's
payload, where `type` is one of `log`, `stats`, `unavailable`, `closed`. The last
two are how the server reports a stream it is deliberately *not* going to
continue — a hibernated sandbox, a closed container — so the UI can show a real
state instead of a panel that silently stops. Neither endpoint retries anything: reconnection is the client's job, and the
close code is how the two halves agree on what happened.

**Authorization** is checked before the upgrade, and a failure is reported as
WebSocket close code **1008 (policy violation)** rather than a refused
handshake, so the client can distinguish "you may not watch this sandbox" from
"the network is down" and stop retrying. The same check backs every REST
route via `requireOwnedSandbox` → `authorizeSandboxAccess`
(`api/sandboxes.go`); neither leaks whether someone else's sandbox ID is real.

### Close codes

The codes are not interchangeable, and the difference is the whole diagnosis
when a panel misbehaves:

| Code | Meaning | Client behaviour |
|---|---|---|
| `1000` | Normal close, preceded by a `closed`/`unavailable` frame carrying the reason. | Stops; shows the reason. |
| `1008` | Refused on policy grounds (not the owner). | Stops; shows "no access". |
| `1006` | **Abnormal closure — no close frame was received.** | Retries with backoff. |
| `1011` | Server ended the stream without delivering a terminal frame. | Retries with backoff. |

`1006` is the ambiguous one, and it is ambiguous in a way that has cost real
debugging time. A browser reports **all** of these as `1006`:

- the TCP connection vanishing mid-stream;
- a **failed handshake** — including a `401` from `requireUser`, a server that is
  simply not running, and a `NEXT_PUBLIC_API_BASE` the browser treats as
  cross-site so the session cookie is never attached to the upgrade.

A browser also reports a refused handshake as `1006` even though the server sent
a perfectly good `401`, because the handshake never completed. So `1006` alone
cannot distinguish "the network dropped" from "the server refused" from "nothing
is listening".

The client resolves it with a second signal rather than the code. `openReconnectingSocket`
(`frontend/src/lib/ws.ts`) tracks whether a socket ever fired `onopen`:

- **closed without ever opening** → the handshake failed, not the network. If
  `probeSession` (a `GET /api/me` call) then reports a bad session, the panel
  stops immediately and says *"Your session has expired. Sign in again to watch
  this sandbox live."* A session that checks out fine means the server is
  unreachable, which **is** worth retrying, so the normal backoff applies. This
  split is what stops an expired cookie from burning all five attempts in
  silence.
- **closed after opening** → a genuine drop; backoff as usual.

Both panels print the code (`── connection lost (code 1006), retrying (1) ──`).
Reconnection gives up after **5** attempts (1s, 2s, 4s, 8s, capped at 15s) and
leaves a manual **Reconnect** button. The attempt counter deliberately survives
a successful open: a server that accepts and then immediately drops the socket
would otherwise reset the counter on every attempt and loop forever.

> The log panel prints `── connected: streaming container logs ──` on a real
> `onopen`, **not** on mount. It used to print on mount, which made a socket that
> never opened indistinguishable from one that connected and then dropped.

While a stream is open, each handler re-reads the sandbox row every 5s. If it
has left `running` the server sends `closed` and shuts the stream down — Docker
would otherwise keep streaming (or silently stall) against a stopped container.

### Logs — `GET /api/sandboxes/{id}/logs/ws`

Docker's multiplexed stdout/stderr frames are demuxed with
`docker/pkg/stdcopy` and streamed line by line. The last **200** lines are
replayed on connect, so a panel opened an hour into a container's life is not
blank. `timestamp` is the container's own write time (Docker prefixes each line
because the request sets `Timestamps: true`), which is what makes the replayed
history honest.

```jsonc
{ "type": "log", "stream": "stdout", "timestamp": "2026-09-26T10:15:30.123Z", "line": "listening on :3000" }
{ "type": "unavailable", "reason": "sandbox is hibernated — live output resumes when it wakes" }
{ "type": "closed", "reason": "log stream ended" }
```

Only `running` sandboxes with a container produce `log` frames. A single line is
capped at 1 MiB: anything longer arrives truncated and marked `…[line truncated]`
rather than being dropped, because one oversized write (a minified bundle, a
base64 blob) must not end the stream and take the rest of the container's output
with it. A dropped client closes its Docker stream immediately, so repeatedly
opening and closing the panel does not accumulate connections.

The 200-line backfill applies to **stdout only**. Docker applies `--tail` to the
*combined* log before filtering by fd, and orders that backfill stderr-first, so
a tail smaller than the whole log discards all of stderr while trimming stdout
normally. The stderr connection therefore uses `--tail all`; the reasoning and
the measurements behind that are in ARCHITECTURE.md.

If a client stops reading, the server's per-frame write eventually fails, the log
pump returns, and the socket is closed. That used to be completely silent — no
log line anywhere — so it was indistinguishable from a network drop. It is now a
`warn` naming the sandbox, the fd, and the error:

```
logs: dropping client that stopped reading  sandbox=… stream=stdout err=…
```

### Stats — `GET /api/sandboxes/{id}/stats/ws`

One flattened sample per frame, ~1/second from the collector, replayed from the
last known sample every 2s so an idle container's chart still moves. Field names
and meanings match the history rows above.

```jsonc
{ "type": "stats", "cpu_percent": 12.4, "memory_used_bytes": 83886080,
  "memory_limit_bytes": 536870912, "network_rx_bytes": 102400,
  "network_tx_bytes": 51200, "recorded_at": "2026-09-26T10:15:32Z" }
```

All six fields are always present, zeros included — zero is the common value for
an idle container, and a metric that vanishes when it reaches zero is a metric
the chart cannot draw. `cpu_percent` is computed from counter deltas, not from
Docker's raw totals — see ARCHITECTURE.md. `memory_used_bytes` has reclaimable
page cache subtracted, and the network counters are summed across every
interface. Both are per-sample deltas, not lifetime totals.

## Gateway (not REST)

`:8090` is a reverse proxy, not part of this API surface. It exposes `GET /` for
health probes and a `*.sandbox.localhost` host path that resolves the sandbox by
subdomain, wakes it if hibernating, then proxies to
`<container-ip>:<internal_port>`. See ARCHITECTURE.md.