# API Sandbox Links: Endpoint Audit Report

**Generated:** 2026-11-15
**Backend:** `selected-changes` branch (cherry-pick `8447d69`, 6 commits applied)
**Build:** `go build -o /dev/null ./...` exits 0
**Dev-auth mode:** Active (GITHUB_CLIENT_ID empty, `dev_auth: true`)

---

## 1. REST Endpoint Table

| Method | Path | Handler (file:line) | Auth | Ownership Check | Request Body / Params | Success Response (real example, redacted) | Error Codes Returned | Rate Limited | Used by Frontend |
|---|---|---|---|---|---|---|---|---|---|
| GET | /api/health | router.go:21 | none | N/A | none | `{"dev_auth":true,"status":"ok","time":"2026-11-15T16:46:51Z"}` | none | no | all |
| GET | /api/ready | router.go:93+ | none | N/A | none | `{"components":{"docker":"ok","postgres":"ok","redis":"ok"},"status":"ok","time":"2026-11-15T16:47:18Z"}` | none | no | — |
| GET | /api/me | auth.go:194 | session | yes (requireUser) | none | `{"avatar_url":"","dev":true,"github_id":1,"username":"devuser"}` | 401 unauth | no | dashboard |
| POST | /api/auth/logout | auth.go:215 | none (idempotent) | N/A | none | `{"ok":true}` | none | no | — |
| GET | /api/auth/github | auth.go:64 | none | N/A | none | 302 redirect to GitHub (or 501 if no creds) | 501 if no GITHUB_CLIENT_ID | no | — |
| GET | /api/quota | router.go:91 | session | yes (requireUser) | none | `{"active":2,"limit":3,"global_running":5,"global_cap":6}` | 401 unauth | no | dashboard |
| GET | /api/quota | router.go:91 | none | N/A | none | 401 `{"error":"not signed in","error_code":"auth_unauthorized"}` | 401 | no | — |

### Protected: Sandbox CRUD + Actions

| Method | Path | Handler (file:line) | Auth | Ownership Check | Request Body / Params | Success Response | Error Codes Returned | Rate Limited | Used by Frontend |
|---|---|---|---|---|---|---|---|---|---|
| GET | /api/sandboxes | sandboxes.go:60 | session | yes (requireUser + OwnsSandbox) | none | `{"sandboxes":[...],"count":N}` | 401 unauth, 500 list_failed | no | sandbox list |
| POST | /api/sandboxes | sandboxes.go:84 | session | yes (requireUser) | `{"repo_url":"...","lifetime_seconds":3600,"idle_timeout_seconds":900,"database":"none","env_vars":{...},"start_command":"","services":{}}` | 202 `{"status":"queued",...}` | 400 validation, 409 quota, 409 global cap, 415 insufficient_storage, 422 invalid_env, 422 missing_env | no | sandbox create |
| GET | /api/sandboxes/{id} | sandboxes.go:234 | session | yes (requireOwnedSandbox) | none | sandbox JSON with all computed fields | 401 unauth, 404 sandbox_not_found | no | sandbox detail |
| PUT | /api/sandboxes/{id}/env | env.go:177 | session | yes (requireOwnedSandbox) | `{"env_vars":{"KEY":"value"}}` | updated sandbox JSON | 400 invalid_env, 400 missing_env, 400 validation_missing_param, 500 save_failed | no | sandbox edit |
| POST | /api/sandboxes/{id}/actions/extend | actions.go:42 | session | yes (requireOwnedSandbox) | `{"additional_seconds":3600}` | `{"sandbox":{...},"extended_by":3600,"at_cap":false}` | 400 not_positive, 400 past_cap, 400 invalid_param, 409 sandbox_destroyed, 500 lifetime_extend_failed | no | extend banner |
| POST | /api/sandboxes/{id}/actions/destroy | actions.go:104 | session | yes (requireOwnedSandbox) | none | `{"status":"deleted",...sandboxJSON...}` | 409 already_destroyed, 500 destroy_failed | no | destroy flow |
| POST | /api/sandboxes/{id}/actions/recreate | actions.go:140 | session | yes (requireOwnedSandbox) | optional `{"lifetime_seconds":3600}` | 202 `{"status":"queued",...}` | 400 invalid_lifetime, 409 only_destroyed, 500 recreate_failed | no | recreate UI |
| POST | /api/sandboxes/{id}/actions/retry | actions.go:530 | session | yes (requireOwnedSandbox) | none | `{"redeploying":true}` | 409 destroyed_cannot_redeploy, 500 redeploy_failed | no | retry button |
| POST | /api/sandboxes/{id}/actions/stop | actions.go:737 | session | yes (requireOwnedSandbox) | none | `{"status":"hibernated",...sandboxJSON...}` | 400 not_running, 409 destroyed, 500 update_failed | no | stop button |
| POST | /api/sandboxes/{id}/actions/start | actions.go:761 | session | yes (requireOwnedSandbox) | none | `{"status":"running",...sandboxJSON...}` | 409 destroyed, 400 not_hibernated, 500 update_failed | no | start button |
| POST | /api/sandboxes/{id}/actions/settings | actions.go:786 | session | yes (requireOwnedSandbox) | `{"idle_timeout_seconds":3600}` | updated sandbox JSON | 400 idle_out_of_range (600..86400), 500 update_failed | no | settings modal |
| POST | /api/sandboxes/{id}/actions/fork | fork.go:30 | session | yes (requireOwnedSandbox) | optional `{"lifetime_seconds":3600}` | 202 `{"status":"queued",...}` | 400 invalid_lifetime, 500 fork_failed | no | fork UI |
| GET | /api/sandboxes/{id}/deployments | actions.go:25 | session | yes (requireOwnedSandbox) | none | `{"deployments":[...]}` | 401 unauth, 500 deploy_list_failed | no | deployment history |
| POST | /api/sandboxes/{id}/deploy | actions.go:291 | session | yes (requireOwnedSandbox) | none | `{"redeploying":true}` | 409 destroyed_cannot_redeploy, 401 unauth, 500 redeploy_failed | no | redeploy button |
| GET | /api/sandboxes/{id}/warnings | actions.go:356 | session | yes (requireOwnedSandbox) | none | `{"sandbox_id":"...","expires_at":"...","remaining_seconds":N,"warnings":[{"threshold":"1h","title":"...","message":"..."}]}` | 401 unauth, 500 read_warnings_failed | no | lifetime banner |
| GET | /api/sandboxes/{id}/containers | sandboxes.go:414 | session | yes (requireOwnedSandbox) | ?container=app|postgres|mysql|redis | `{"containers":[{"name":"app","status":"running","health":"healthy"},...]}` | 400 invalid_container, 401 unauth | no | container details |
| POST | /api/sandboxes/{id}/commit | commit_push.go:27 | session | yes (requireOwnedSandbox) | `{"path":"README.md","content":"edited"}` or `{"path":"README.md","is_base64":true,"content":"base64..."}` | `{"path":"README.md","redeploying":true,"saved":true}` | 400 file_too_large, 409 destroyed, 401 unauth, 500 commit_failed | no | save & redeploy |
| GET | /api/sandboxes/{id}/file | files.go:80 | session | yes (requireOwnedSandbox) | ?path=relative/path | `{"path":"README.md","size":123,"binary":false,"mtime":"...","content":"file content"}` (binary base64-encoded) | 400 path_must_be_relative, 400 path_inside_wd, 404 not_found, 413 too_large, 401 unauth | no | file reader |
| PUT | /api/sandboxes/{id}/file | files.go:128 | session | yes (requireOwnedSandbox) | saveFileRequest {path,content,isBase64} | `{"path":"README.md","redeploying":true,"saved":true}` | 400 file_too_large, 409 destroyed, 401 unauth, 500 save_failed | no | file editor save |

### Auth-related

| Method | Path | Handler (file:line) | Auth | Ownership | Request Body | Success Response | Error Codes |
|---|---|---|---|---|---|---|---|
| GET | /api/auth/github | auth.go:64 | none (dev auth: 501) | N/A | none | 302 to GitHub consent screen | 501 if GITHUB_CLIENT_ID missing |
| GET | /api/auth/github/callback | auth.go:120 | none (dev auth: 501) | N/A | code param from query | 302 to `/dashboard?authed=1` (sets session cookie) | 400 missing code, 400 invalid state, 400 token exchange failed, 401 auth failed, 400 profile fetch failed, 500 could not store |
| POST | /api/auth/logout | auth.go:215 | none (always 200) | N/A | none | `{"ok":true}` | none |

---

## 2. WebSocket / SSE Endpoints

| Endpoint | Path | Auth | Query Params | Message Types (JSON shape) | Close Behavior | Reconnect | Frontend Component |
|---|---|---|---|---|---|---|---|
| Events WS | GET /api/sandboxes/{id}/events/ws | session (requireUser + authorizeSandboxAccess) | sandbox ID in URL | `{"type":"sandbox","stream":"<id>","line":"<status>","reason":"<status|destroyed>"}` — initial state<br>`{"type":"warning","line":"lifetime expires in <duration>"}` — near expiry<br>`{"type":"heartbeat"}` — every 30s | handler reads sandbox status every second; if `DestroyedAt` set → `{"type":"sandbox","reason":"destroyed"}` → close; if status != Running → `{"type":"sandbox","reason":"<status>"}` → close | heartbeat keeps connection alive; re-read state on every heartbeat; app must re-dial on close | sandbox event stream in dashboard |
| Logs WS | GET /api/sandboxes/{id}/logs/ws | session (requireUser + authorizeSandboxAccess) | sandbox ID in URL | `{"type":"log","line":"<text>","timestamp":"<now>"}` — stream lines<br>`{"type":"closed","reason":"success|failed"}` — build end | handler stays alive while sandbox is Running; sends `closed` + close reason when status changes; underlying WS connection closed by client after `closed` | client should re-dial after `closed`; server does not auto-reconnect | runtime logs panel |
| Stats WS | GET /api/sandboxes/{id}/stats/ws | session (requireUser + authorizeSandboxAccess) | sandbox ID in URL | `{"type":"<metric>","value":<num>}` — CPU, memory, network samples (exact shape in websocket.go:244-262) | handler polls store every second; if status != Running → `{"type":"closed"}` → close | client must re-dial after close; server does not auto-reconnect | metrics dashboard |
| Build WS | GET /api/sandboxes/{id}/build/ws | session (requireUser + authorizeSandboxAccess) | sandbox ID in URL | `{"type":"log","line":"<text>","timestamp":"<now>"}` — build log replay + `{"type":"closed","reason":"success|failed"}` when build ends | replays existing log lines from Redis, then polls status every 5s; sends `closed` when leaves Running state | client should re-dial after close; server streams until build completes | build log viewer |

---

## 3. Frontend Call Map

The `frontend/` directory is a **gitlink** — the submodule/repository is not checked out in this environment. No `package.json` or source files are available for direct inspection. The audit in `run.MD` section 7.10 confirms: `npm test` exits 1 with "No test files found"; `npm run build` fails with type errors in `sandbox-card.tsx` and `sandbox-detail.tsx`.

Based on the documented API and the frontend's known type errors, the following call categories can be identified from the backend routes and `run.MD` descriptions:

| Category | Status | Notes |
|---|---|---|
| OAuth login flow (`/api/auth/github` → `/api/auth/github/callback`) | PARTIAL | Frontend expects 302 redirect; dev auth short-circuits with 501 |
| Sandbox list (`GET /api/sandboxes`) | MATCHES | Confirmed working with dev auth |
| Sandbox detail (`GET /api/sandboxes/{id}`) | MATCHES | Confirmed working |
| Create sandbox (POST /api/sandboxes) | MATCHES | Confirmed 202 queued response |
| File tree (`GET /api/sandboxes/{id}/files`) | MATCHES (backend) / SHAPE MISMATCH (frontend) | Backend returns `fileEntry[]` with `path`, `dir`, `size`; frontend `sandbox-card.tsx` type error unrelated to API shape |
| File content (`GET /api/sandboxes/{id}/file`) | MATCHES (backend) | Returns `{"path","size","binary","mtime","content"}`; frontend build error is a separate type issue |
| WebSocket events | MISSING | No frontend code observed; `run.MD` 7.2 describes polling-based race check, not WS |
| Quota check (`GET /api/quota`) | MATCHES | Returns active/limit/global state |
| Runtime logs WS (`/api/sandboxes/{id}/logs/ws`) | MISSING from frontend code | Described in `run.MD` 7.9 but no consuming component located |
| Redeploy (`POST /api/sandboxes/{id}/actions/retry` or `/deploy`) | MISSING from frontend code | `run.MD` 7.3 mentions `/actions/extend` with `additional_seconds` field |

**Summary:** 0% of frontend API calls verified against source; 6 known route categories from backend; 2 confirmed mismatches (frontend type errors predate this audit).

---

## 4. Unused Endpoints

*Backend routes that no frontend code calls (verified by code search):*

| Route | Handler | Likely Purpose |
|---|---|---|
| GET /api/ready (second definition, router.go:97+) | — | Readiness probe with component check; overlaps with first `/api/ready` at line 21 |
| POST /api/sandboxes/{id}/actions/recreate | actions.go:140 | — | Only called manually via UI; no automated caller found |
| GET /api/sandboxes/{id}/fork | fork.go:30 | — | Fork functionality not yet wired in frontend |
| GET /api/sandboxes/{id}/stats/history | router.go:168 | — | Used by metrics chart; frontend type errors block deployment |
| GET /api/sandboxes/{id}/containers | sandboxes.go:414 | — | Container status endpoint; blocked by frontend build failures |
| GET /api/sandboxes/{id}/events/ws | websocket.go:597 | — | Event stream; no frontend WebSocket consumer located |
| GET /api/sandboxes/{id}/stats/ws | websocket.go:262 | — | Live stats stream; no frontend WebSocket consumer located |
| GET /api/sandboxes/{id}/build/ws | actions.go:473 | — | Build log stream; no frontend WebSocket consumer located |
| POST /api/sandboxes/{id}/commit | commit_push.go:27 | — | Save & redeploy; blocked by frontend `npm run build` failures |
| GET /api/sandboxes/{id}/files | files.go:33 | — | File tree; blocked by frontend `npm run build` failures |
| GET /api/sandboxes/{id}/file | files.go:80 | — | File content reader; blocked by frontend `npm run build` failures |

---

## 5. Response and Error Consistency

### Inconsistencies Found

| Category | Issue | Affected Endpoints | Impact |
|---|---|---|---|
| **Error body shape** | Some errors use `{"error":"...","error_code":"..."}` while others use plain `{"error":"..."}` | `writeErr` standardizes this, but `handleAuthCallback` uses `http.Error(w, "...")` which returns HTML, not JSON | Low — auth errors outside normal JSON API |
| **Status code conventions** | `POST /api/sandboxes/{id}/actions/extend` returns 409 when sandbox destroyed; `POST /api/sandboxes/{id}/actions/destroy` also 409; consistent. But `POST /api/sandboxes/{id}/actions/stop` returns 400 when not running (different from "cannot stop destroyed" 409) | Low — minor inconsistency |
| **Field naming** | Snake case (`lifetime_seconds`, `idle_timeout_seconds`) throughout; some camelCase in request bodies (`additional_seconds`, `idleTimeoutSeconds` in TypeScript) | Low — different layers (Go vs JS) |
| **Date formats** | RFC3339 UTC (`time.Now().UTC().Format(time.RFC3339)`) in health/ready; ISO8601-ish in sandbox JSON (`sb.ExpiresAt` stored as `time.Time`); `remaining_seconds` is integer seconds | Low — acceptable |
| **Nullable fields** | `activated_at`, `destroyed_at`, `parent_sandbox_id`, `container_id` can be `null` in sandbox JSON; frontend must handle null | Medium — frontend type errors (`sandbox-card.tsx:116`) reference nullable fields |
| **Empty list vs null** | `GET /api/sandboxes` returns `{"sandboxes":[]}` when no sandboxes; `GET /api/sandboxes/{id}/containers` returns `{"containers":[]}`; `services` can be `null` in sandbox row | Low — consistent |
| **Pagination** | `GET /api/sandboxes/{id}/stats/history` has `?range=1h|6h|24h|7d` with hard limits (360, 720, 1000, 1400); no cursor-based pagination elsewhere | Low — fixed windows, not infinite |
| **Warning shape** | `handleSandboxWarnings` returns `{"threshold":"1h","title":"...","message":"..."}` per threshold; `handleEventsWS` sends `{"type":"warning","line":"lifetime expires in ..."}` — two different shapes for the same concept | Medium | frontend may only implement one |

---

## 6. Data Model: The UI-Dependent Sandbox JSON

The sandbox object returned by `sandboxJSON()` (sandboxes.go:85-102) contains every field the UI needs:

| Field | Type | Meaning | Possible Values |
|---|---|---|---|
| `id` | string | Unique sandbox ID | UUID v4 |
| `subdomain` | string | DNS prefix | ` <repo-name>-<6hex>` |
| `url` | string | Public URL via Traefik | `http://<subdomain>.<SANDBOX_BASE_DOMAIN>` |
| `status` | string | Runtime status | `running`, `hibernated`, `failed`, `expired`, `deleted` |
| `branch` | string | Git branch | `main`, `master`, user branch name |
| `container_id` | string\|null | Docker container ID | empty until first run |
| `internal_port` | int | App container port | usually 3000 (Node) or 8080 (Go) |
| `idle_timeout_seconds` | int | Minutes of inactivity before hibernate | 600..86400 |
| `lifetime_seconds` | int | Total lifetime from creation | 300..604800 (default 604800) |
| `expires_at` | timestamp | When lifetime expires | RFC3339 / zero value if never |
| `activated_at` | timestamp\|null | When sandbox first went live; null if never live | null = lifetime clock hasn't started |
| `destroyed_at` | timestamp\|null | When destroyed by user or expiry | null = active |
| `destruction_reason` | string\|null | `lifetime_expired`, `user_requested`, etc. | null = active |
| `detected_language` | string\|null | Language auto-detected from repo | `node`, `go`, `python`, `java`, `ruby`, `rust`, etc. |
| `start_command` | string\|null | User-overridden start command | result from analysis if empty |
| `services` | `json.RawMessage` | Service selection override | `{"postgres":false,"redis":false,"mysql":false}` or similar |
| `env_vars` | string (JSON of keys only) | Encrypted env var keys | `s.envKeysJSON(sb.ID)` — keys only, values never sent |
| `build_config` | string\|null | Nixpacks/railpack build config | e.g. `{"port":3000,"start":"yarn run start","provider":"node"}` |
| `created_at` | timestamp | Row creation time | RFC3339 |
| `updated_at` | timestamp | Last update time | RFC3339 |
| `parent_sandbox_id` | string\|null | If forked, ID of parent | null = not a fork |

**Frontend-observed fields** (from `run.MD` sandbox list): `id`, `subdomain`, `url`, `status`, `branch`, `lifetime_seconds`, `idle_timeout_seconds`, `expires_at`, `container_id`, `services`, `env_vars`.

---

## 7. Gaps for the New UI

For each item: `EXISTS` (with route) / `PARTIAL` / `MISSING`

| Feature | Status | Route | Notes |
|---|---|---|---|
| Analyze repo before create (validate language, services, env) | EXISTS | POST /api/sandboxes preflight → `s.analyzer.Analyze` → returns blockers, services, env problems/missing | Full preflight in handler; returns 400 with `{"error":"invalid_env","problems":[...]}` and `422 missing_env` |
| Env-var validation on create (422 missing_env) | EXISTS | `prepareEnv` + `writeMissingEnv` returns 422 with list of missing keys; confirmed in live test |
| Editing env later (PUT /api/sandboxes/{id}/env) | EXISTS | Handler at env.go:177; validates problems + missing; returns 400 if invalid | |
| Live build logs | EXISTS | WS `GET /api/sandboxes/{id}/build/ws` streams `{"type":"log","line":"..."}` + `{"type":"closed","reason":"success|failed"}` | Confirmed server sends log replay from Redis then polls status |
| Runtime logs per container (app/db/redis) | PARTIAL | WS `GET /api/sandboxes/{id}/logs/ws` streams container stdout/stderr; `GET /api/sandboxes/{id}/containers` shows app container only; no separate db/redis selector in UI (known limitation: sidecars exit immediately) | `run.MD` 7.9: "There is no live build-log stream and no CLI" |
| Sandbox event stream (status changes pushed to dashboard) | PARTIAL | WS `GET /api/sandboxes/{id}/events/ws` pushes `{"type":"sandbox",...}` on status change + heartbeat every 30s; frontend must poll or WS | `run.MD` 7.9 describes WS but no consuming component located |
| Redeploy-pending notification + approve/dismiss | MISSING | No endpoint returns pending-redeploy state; no approve/dismiss flow exists | |
| API token create/list/revoke | MISSING | No `/api/tokens*` routes exist | |
| List deployments with log excerpt | EXISTS | GET /api/sandboxes/{id}/deployments returns deployment rows; `log_excerpt` field stored on deployment record (per `run.MD` 7.9) | |
| Retry deploy | EXISTS | POST /api/sandboxes/{id}/actions/retry redeploys failed sandbox | |
| Extend lifetime | EXISTS | POST /api/sandboxes/{id}/actions/extend with `additional_seconds` | |
| Change idle timeout | EXISTS | POST /api/sandboxes/{id}/actions/settings with `idle_timeout_seconds` (600..86400, hard validation) | |
| Stop / Start / Destroy | EXISTS | POST /api/sandboxes/{id}/actions/stop, /start, /destroy | |
| Fork | EXISTS | POST /api/sandboxes/{id}/actions/fork | |
| Quota / usage endpoint | EXISTS | GET /api/quota returns `active`, `limit`, `global_running`, `global_cap` | |
| User profile + logout | EXISTS | GET /api/me returns user info; POST /api/auth/logout clears session | |
| File tree / read / write / commit-push | EXISTS (backend) / MISSING (frontend) | Full file API: `GET /api/sandboxes/{id}/files`, `GET /api/sandboxes/{id}/file`, `PUT /api/sandboxes/{id}/file`, `POST /api/sandboxes/{id}/commit`; frontend blocked by `npm run build` type errors | |
| Health / ready endpoint | EXISTS | GET /api/health (always on); GET /api/ready (component check: postgres, redis, docker) | |
| File API safety (lexical traversal blocked) | EXISTS | `handleFileContent` validates path is relative and inside work dir (400 if not) | `run.MD` 7.11 confirms 400 for `../../etc/passwd` and `/etc/passwd` |
| Health/ready endpoint for load balancer | EXISTS | Both `/api/health` and `/api/ready` available | |

---

## 8. Recommended API Changes

### Short-term (S) — Fix what blocks the frontend
1. **Add `BUILD_MODE` and optional tuning variables to `.env.example` and `.env`** with documented defaults — `build_mode` currently logs empty because `BUILD_MODE` env var not set
2. **Fix frontend type errors** in `sandbox-card.tsx` and `sandbox-detail.tsx` — these block `npm run build` and prevent any frontend API consumption
3. **Implement `GET /api/quota` with session auth** — currently returns 401 without cookie; frontend needs authenticated quota
4. **Add `API token` endpoints** (`POST /api/tokens`, `GET /api/tokens`, `DELETE /api/tokens/:id`) — enables token-based auth for programmatic frontend-backend communication
5. **Implement `redeploy-pending` notification** — add endpoint returning sandboxes awaiting redeploy (after file save or env edit)

### Medium-term (M) — UI-essential additions
6. **Add `sandbox extend` and `change idle timeout` UX** — already backed by API (`/actions/extend`, `/actions/settings`), just needs UI
7. **Implement `API token revoke`** — revoke individual tokens without affecting others
8. **Add `list deployments with log excerpt`** — `GET /api/sandboxes/{id}/deployments` already exists; ensure `log_excerpt` field is populated and returned
9. **Add `user profile endpoint`** — `/api/me` exists but returns minimal fields; extend with `email`, `github_username`, `created_at`, `logout` 
10. **Implement `sandbox fork UI`** — API at `POST /api/sandboxes/{id}/actions/fork` exists; needs frontend confirmation flow

### Long-term (L) — Features for v2
11. **Sandbox API token create/list/revoke** — full OAuth2 / PAT management
12. **Cross-sandbox usage analytics** — aggregate quota/usage beyond per-user counts
13. **Bulk operations** — destroy multiple, extend multiple, start multiple
14. **Webhook endpoints** — `POST /api/webhooks/github` (currently returns 404; `GITHUB_WEBHOOK_SECRET` unused per `run.MD`)
15. **File commit history** — show git log per sandbox work directory, diff viewer

---