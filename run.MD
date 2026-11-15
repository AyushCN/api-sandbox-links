# API Sandbox Links: Run Guide

Every command in this file was executed against this checkout on 2026-11-12. Commands that do not work are called out explicitly. This guide documents the repository as it is; it does not describe features that are absent.

Configuration used for the live run: `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` empty (dev auth), `SANDBOX_BASE_DOMAIN=sandbox.localhost`, `SANDBOX_WORK_DIR=/tmp/api-sandbox-links/work`, platform Postgres on `127.0.0.1:5434`, Redis on `127.0.0.1:6380`.

## 0. Prerequisites

```bash
docker --version                # tested with Docker 29.1.3
docker compose version          # tested with Compose 2.40.3 (use `docker compose`, not `docker-compose`)
go version                      # tested with go1.26.0
node --version && npm --version # tested with Node 24.15.0 / npm 12.0.2
git --version
nixpacks --version              # tested with nixpacks 1.41.0
```

Install nixpacks if missing:

```bash
curl -sSL https://nixpacks.com/install.sh | bash
```

Confirm the binary the backend will use:

```bash
which nixpacks
nixpacks --version
```

## 1. Environment

The **root** `.env.example` is currently ignored by `.gitignore` and is absent from a fresh clone. The tracked template is `backend/.env.example`; copy it to the repo root:

```bash
cp backend/.env.example .env
```

Edit `.env`. Never commit it. The backend loads `.env` from its **working directory**, so run the binary from the repository root.

Values that matter for a local run:

| Variable | Purpose | Local value |
|---|---|---|
| `DATABASE_URL` | Platform Postgres DSN | `postgres://user:pass@127.0.0.1:5434/sandbox_platform?sslmode=disable` |
| `REDIS_URL` | Platform Redis | `redis://127.0.0.1:6380` |
| `SECRETS_ENCRYPTION_KEY` | AES-256-GCM key (32 bytes, base64) | Generate: `openssl rand -base64 32` |
| `GITHUB_CLIENT_ID` | GitHub OAuth client ID | Empty for dev auth |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth client secret | Empty for dev auth |
| `GITHUB_OAUTH_CALLBACK_URL` | OAuth callback | `http://localhost:8080/api/auth/github/callback` |
| `MAX_ACTIVE_SANDBOXES_PER_USER` | Per-user live sandbox cap | `3` (default) |
| `MAX_RUNNING_SANDBOXES` | Global live sandbox cap | `6` (default) |
| `BUILD_CONCURRENCY` | Simultaneous builds | `2` (default) |
| `MAX_LIFETIME_SECONDS` | Maximum lifetime | `604800` (7 days) |
| `FRONTEND_URL` | OAuth/CORS origin | `http://localhost:3000` |
| `SANDBOX_BASE_DOMAIN` | Sandbox host suffix | `sandbox.localhost` |
| `SANDBOX_WORK_DIR` | Host clone directory | `/tmp/api-sandbox-links/work` |
| `NIXPACKS_BIN` | nixpacks path | `nixpacks` |

Notes verified against the code:

- `APP_ENV`, `GITHUB_WEBHOOK_SECRET`, and `BUILD_RATE_LIMIT` appear in the example files but are **not read** by `backend/internal/config/config.go`. There is no production-mode validation.
- Dev auth is active whenever `GITHUB_CLIENT_ID` is empty. It is not gated by `APP_ENV`.
- The `.env` file was committed in git history at `e979a42` and removed at `08561a3`; history was not purged. Rotate `SECRETS_ENCRYPTION_KEY` and all other credentials before any real deployment.

## 2. Start infrastructure

```bash
docker compose -f docker-compose.dev.yml up -d
docker compose -f docker-compose.dev.yml ps
```

Expected services: `sandbox-platform-db` (Postgres), `sandbox-platform-redis`, `sandbox-platform-traefik`.

Confirm platform services are loopback-only:

```bash
docker compose -f docker-compose.dev.yml ps --format '{{.Names}}\t{{.Ports}}'
# Postgres: 127.0.0.1:5434->5432/tcp
# Redis:    127.0.0.1:6380->6379/tcp
# Traefik:  0.0.0.0:80->80/tcp, 127.0.0.1:8081->8081/tcp
```

Traefik uses the file provider in `traefik/dynamic.yml`, routes `*.sandbox.localhost` to the backend gateway at `host.docker.internal:8090`, and exposes its dashboard/API at `http://127.0.0.1:8081` (do not expose this port publicly).

## 3. Backend

Build from `backend/` (that is where `go.mod` lives) and run from the repo root:

```bash
cd backend
go build ./...
go vet ./...
go build -o server ./cmd/server
cd ..
```

Dev-auth run (recommended for local testing):

```bash
GITHUB_CLIENT_ID= GITHUB_CLIENT_SECRET= ./backend/server
```

If real OAuth credentials are present in `.env`, start it without the overrides:

```bash
./backend/server
```

Health check:

```bash
curl -s localhost:8080/api/health
# {"dev_auth":true,"status":"ok","time":"..."}
```

The process serves the REST API on `:8080` and the sandbox traffic gateway on `:8090`. Traefik forwards subdomain traffic to `:8090`.

> The API binds all interfaces. A sandbox can reach the bridge gateway on `:8080` and call the platform API. See "Known limitations".

## 4. Frontend

```bash
cd frontend
npm install
npm run dev
```

Open http://localhost:3000.

The frontend currently does not type-check or build. These commands fail as of this audit:

```bash
cd frontend
npm run build
# Type error: Type '"zinc"' is not assignable to type '"blue" | "green" | "red" | "violet" | "slate" | "amber" | undefined'
#   at src/components/sandbox-card.tsx:116 and src/components/sandbox-detail.tsx:143

npm run lint
# 3 errors: unexpected `any` in src/app/dashboard/page.tsx:109,115;
# unescaped apostrophe in src/app/page.tsx:202

npm test
# No test files found, exiting with code 1
```

## 5. CLI

There is no CLI in this repository. `cli/` does not exist, and `git ls-files | grep -i cli` only matches `backend/internal/ai/client.go`.

Commands that earlier documentation mentioned (`apisandbox login`, `deploy`, `list`, `logs -f`, `extend`) are not available and were not run. Do not add them back to this guide until the CLI exists.

## 6. Quick end-to-end demo

Create a sandbox (dev auth). This sample was verified:

```bash
curl -s -X POST localhost:8080/api/sandboxes/ \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":3600,"idle_timeout_seconds":900,"database":"none"}'
# HTTP 202 with {"status":"queued",...}
```

Poll until `running`:

```bash
curl -s localhost:8080/api/sandboxes/ | python3 -c 'import sys,json; d=json.load(sys.stdin); [print(x["id"][:8], x["status"], x["url"]) for x in d["sandboxes"]]'
```

The list response is an object with a `sandboxes` key, not a bare array. Destroyed sandboxes are also returned by this endpoint; filter on `destroyed_at == null` when counting live ones.

Open the public URL through Traefik on `:80` (works):

```bash
curl -s -H 'Host: <subdomain>.sandbox.localhost' http://127.0.0.1/
# HTTP 200
```

In a browser: `http://<subdomain>.sandbox.localhost` (may require adding `sandbox.localhost` to your resolver).

## 7. Verification checklist

### 7.1 Tests

```bash
cd backend
go test ./...
# every package prints "[no test files]"; exit code 0

cd ../frontend
npm test
# No test files found, exiting with code 1
```

There are no automated tests for the state machine, quota, TTL, teardown, traversal, or webhook signatures. `ASL_TEST_DATABASE_URL` and `go test -race ./...` were used by earlier docs but there are no test files that consume them.

### 7.2 Lifetime and quota

```bash
# Below the 1-hour minimum: must be 400
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/api/sandboxes/ \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":600,"database":"none"}'
# 400 {"error":"lifetime_seconds must be between 3600 and 604800"}

# Create until the fourth live sandbox: must be 409 with limit 3
curl -s -X POST localhost:8080/api/sandboxes/ \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":3600,"database":"none"}'
# after 3 live sandboxes: 409 {"error":"active sandbox quota reached (3); ...","limit":3}
```

Race check (fire six concurrent creates with two already active; exactly one should be accepted):

```bash
for i in 1 2 3 4 5 6; do
  curl -s -o /tmp/race-$i.json -w "%{http_code}\n" -X POST localhost:8080/api/sandboxes/ \
    -H 'Content-Type: application/json' \
    -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":3600,"database":"none"}' &
done
wait
```

### 7.3 Extend

The path is `/actions/extend` and the field is `additional_seconds`:

```bash
curl -s -X POST localhost:8080/api/sandboxes/<id>/actions/extend \
  -H 'Content-Type: application/json' -d '{"additional_seconds":3600}'
# 200, expiry moved +1h
# Asking for more than the 7-day cap returns at_cap=true and clamps to activation+7d
```

`{"seconds":3600}` returns `400 additional_seconds must be positive`.

### 7.4 Sidecar databases (currently broken)

Only Postgres and MySQL are recognized. Redis and MongoDB are not implemented.

```bash
curl -s -X POST localhost:8080/api/sandboxes/ \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":3600,"database":"postgres"}'

docker ps -a --filter "label=api-sandbox-links.sandbox-id=<id>" --format '{{.Names}}\t{{.Status}}'
# app container Up; db_<id8> Exited (1)

docker logs db_<id8>
# Postgres: "Database is uninitialized and superuser password is not specified."
# MySQL:    "Database is uninitialized and password option is not specified ... MYSQL_ROOT_PASSWORD"
```

The app is still marked `running` because `WaitForDatabase` is never called. Do not demo a DB-backed sandbox until this is fixed.

### 7.5 Container hardening

The label is `api-sandbox-links.sandbox-id` (not `sandbox_id`):

```bash
for c in $(docker ps -aq --filter "label=api-sandbox-links.sandbox-id"); do
  docker inspect "$c" --format '{{.Name}} priv={{.HostConfig.Privileged}} capdrop={{.HostConfig.CapDrop}} capadd={{.HostConfig.CapAdd}} secopt={{.HostConfig.SecurityOpt}} mem={{.HostConfig.Memory}} swap={{.HostConfig.MemorySwap}} nano={{.HostConfig.NanoCpus}} pids={{.HostConfig.PidsLimit}} binds={{.HostConfig.Binds}} log={{.HostConfig.LogConfig}} restart={{.HostConfig.RestartPolicy.Name}}'
done
```

Expected for the app: `priv=false`, `capdrop=[ALL]`, `capadd=[CAP_NET_BIND_SERVICE]`, `no-new-privileges:true`, `mem=536870912`, `swap=536870912`, `nano=750000000`, `pids=256`, no binds, json-file max 10m × 3, `on-failure:3`. `ReadonlyRootfs` is not set.

### 7.6 Isolation: platform services must be unreachable from a sandbox

Get the sandbox gateway and test from inside the app container (Node is available; `wget`/`nc` are not in the Node image):

```bash
APP=$(docker ps --format '{{.Names}}' | grep sandbox_ | grep <id8> | head -1)
GW=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$APP")
docker exec "$APP" node -e "fetch('http://$GW:5434',{signal:AbortSignal.timeout(2500)}).then(r=>console.log('OPEN',r.status)).catch(e=>console.log('BLOCKED',e.name))"
docker exec "$APP" node -e "fetch('http://$GW:6380',{signal:AbortSignal.timeout(2500)}).then(r=>console.log('OPEN',r.status)).catch(e=>console.log('BLOCKED',e.name))"
# Both must report BLOCKED
```

Test cross-sandbox isolation by fetching another sandbox's container IP on its app port; it must time out. `docker.sock` is not mounted. The platform API on `$GW:8080` **is** reachable; that is a known finding, not expected behavior.

### 7.7 Leak test after a failed deploy

```bash
# After a sandbox reaches status failed:
docker ps -a --filter "label=api-sandbox-links.sandbox-id=<id>"   # empty
docker network ls --format '{{.Name}}' | grep "sandbox_<id8>"    # empty
docker images --format '{{.Repository}}:{{.Tag}}' | grep "<subdomain>"  # empty
psql "$DATABASE_URL" -c "select count(*) from sandbox_databases where sandbox_id='<id>';"  # 0
```

Verified: failed Go/Python/Java builds and two failed clones left no containers, images, networks, volumes, or `sandbox_databases` rows.

### 7.8 Hibernate and wake-on-request

```bash
docker exec sandbox-platform-redis redis-cli DEL lr:<id>
# within ~10s the sandbox becomes hibernated and the container is stopped
curl -i -H 'Host: <subdomain>.sandbox.localhost' http://127.0.0.1/
# HTTP 200; status returns to running (wake took 0.77s when tested)
```

### 7.9 Logs

Runtime logs stream over the WebSocket at `/api/sandboxes/<id>/logs/ws` and are app-container only. There is no db/redis selector.

Build output is stored on the deployment record (`log_excerpt`). There is no live build-log stream and no CLI:

```bash
docker exec sandbox-platform-db psql -U user -d sandbox_platform -c \
  "select trigger,status,started_at,finished_at,left(log_excerpt,500) from deployments where sandbox_id='<id>' order by started_at;"
```

The frontend exposes a Runtime Logs panel (xterm) and a Build Logs view, but `npm run build` currently fails.

### 7.10 Webhooks

Not implemented. All of these return 404:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/api/webhooks/github \
  -H 'Content-Type: application/json' -d '{"zen":"test"}'
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/webhooks/github -d '{}'
```

Do not use smee.io: there is no endpoint to forward to, and `GITHUB_WEBHOOK_SECRET` is unused. There is no pending-redeploy WebSocket event or approve/dismiss flow.

### 7.11 File API safety

```bash
curl -s -o /dev/null -w '%{http_code}\n' "localhost:8080/api/sandboxes/<id>/file?path=../../etc/passwd"
# 400 {"error":"path must be relative and inside the working directory"}

curl -s -o /dev/null -w '%{http_code}\n' "localhost:8080/api/sandboxes/<id>/file?path=/etc/passwd"
# 400
```

Lexical traversal is blocked. Symlinks are not resolved, so a symlink committed inside the repo can escape the work directory; there are no size limits either. Treat the editor as unsafe for untrusted repositories until this is fixed.

Save and redeploy:

```bash
curl -s -X PUT localhost:8080/api/sandboxes/<id>/file \
  -H 'Content-Type: application/json' \
  -d '{"path":"README.md","content":"edited"}'
# {"path":"README.md","redeploying":true,"saved":true}
```

### 7.12 Language samples

Use one real sample per language and record the result. Results from this audit:

| Language | Sample | Result |
|---|---|---|
| Node | `heroku/node-js-getting-started` | PASS, running; 117.0s cold, 15.9s save-redeploy |
| Go | `heroku/go-getting-started` | FAIL: `go1.27 ... toolchain not available` |
| Python | `render-examples/flask-hello-world` | FAIL: `No start command could be found` |
| Java | `heroku/java-getting-started` | FAIL: exceeded 20-minute timeout, ran 2,550.9s, killed |

Pre-warm Node builds before a demo. Do not assume Go/Python/Java will work.

## 8. Reconciliation test (currently fails)

```bash
# Start a running sandbox, then stop the backend and remove its app container:
docker rm -f <app_container>
./backend/server

# Observe: the sandbox remains "running" with a stale container_id;
curl -s localhost:8080/api/sandboxes/<id>
# public URL returns 503 {"error":"Sandbox container is unreachable."}
```

There is no startup reconciliation. A stale row is not repaired.

## 9. Stop and cleanup

Stop application processes first (Ctrl+C for the backend and frontend), then remove the sandboxes through the API so the orchestrator performs its normal teardown:

```bash
# For each live sandbox ID you recorded during the run:
curl -s -X POST "localhost:8080/api/sandboxes/<id>/actions/destroy"
# repeat until the active list is empty
```

Verify nothing is left for the IDs you created:

```bash
docker ps -a --format '{{.Names}}' | grep -E 'sandbox_|^db_' || true
docker images --format '{{.Repository}}:{{.Tag}}' | grep '^sandbox-' || true
docker network ls --format '{{.Name}}' | grep '^sandbox_' || true
docker volume ls --format '{{.Name}}' | grep '^sandboxdata_' || true
```

Stop the infrastructure (this preserves the database volume):

```bash
docker compose -f docker-compose.dev.yml stop
```

Use `down` only when you also want to remove the platform container network (add `-v` to wipe platform data):

```bash
docker compose -f docker-compose.dev.yml down
```

Remove the local binary if you built it:

```bash
rm -f backend/server
```

Do not delete the pre-existing `sandboxdata_*` volumes or `sandbox_*` networks that you did not create.

## 10. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `curl: (7) Failed to connect ... :8080` after a cleanup | `backend/server` was removed. Rebuild: `cd backend && go build -o server ./cmd/server`, then run from the repo root. |
| `buildx component is missing` | `nixpacksEnv` must preserve `HOME`; it does in the current code. Confirm `HOME` points to the user with `~/.docker/cli-plugins`. |
| Public URL 404 on `:80` | Traefik must mount `traefik/dynamic.yml`, disable the Docker provider, and reach `host.docker.internal:8090`. Check `docker compose logs sandbox-platform-traefik`. |
| Go build: `toolchain not available` | The repo's `go.mod` requests a newer Go than nixpacks provides (tested: `go1.27`). Use a repo with an older `go` directive. |
| Python: `No start command could be found` | The repo has no Procfile and nixpacks cannot infer a start command. There is no platform override. |
| Java build never finishes | The 20-minute timeout does not kill orphaned `docker buildx`/`nix-env` children. Find and kill them: `ps -eo pid,etime,args | grep -E 'nixpacks|docker build|nix-env'`; then destroy the sandbox. |
| Sandbox stuck `building` | Same orphan-process issue, or a build genuinely exceeds 20 minutes. Check `docker ps` and the processes above. |
| 409 on create | Per-user limit of 3 live sandboxes. Destroy one or wait for expiry. |
| 503 on create | Global running cap (default 6) reached. |
| `db_<id>` exits 1 | Known Postgres/MySQL sidecar bug (empty credentials, no readiness wait). Do not rely on DB sandboxes. |
| `git clone` times out | Transient network failure; retry. The failed clone cleans up after itself. |
| `npm test` exits 1 | No test files exist. |
| `npm run build` fails with `Type '"zinc"' ...` | Known frontend type error in `sandbox-card.tsx` and `sandbox-detail.tsx`. |
| `GET /api/auth/github` returns 501 | Expected in dev-auth mode; it is only available when `GITHUB_CLIENT_ID` is set. |
| Sandbox is reachable but platform DB is not | Expected: compose binds Postgres/Redis to `127.0.0.1`; the bridge gateway ports are blocked. |

## 11. Known limitations

- No CLI and no API-token management.
- No webhook endpoint, signature verification, pending-redeploy event, or approve/dismiss flow.
- No Redis or MongoDB sidecars. Postgres and MySQL sidecars exist but exit immediately because credentials are not passed, and readiness is never checked.
- Private repositories are not supported (`git.Clone` uses no auth; OAuth `repo` scope is unused for cloning).
- Builds run on the host through nixpacks/Docker; there is no gVisor or other user-container runtime isolation.
- Egress from sandboxes is unrestricted; metadata and port-25 blocking are not enforced.
- HTTPS is not configured; local mode is HTTP-only.
- No startup reconciliation for containers removed outside the platform.
- The 20-minute deploy timeout does not reliably terminate builds; orphaned Docker/nix processes may outlive it.
- No rate limiting on auth, create, deploy, webhook, or general API.
- Session cookies are `HttpOnly` and `SameSite=Lax` but not `Secure` behind Traefik TLS termination.
- The Traefik dashboard/API is exposed on `127.0.0.1:8081`.
- The sandbox backend API is reachable on the bridge gateway at `:8080` from inside sandboxes.
- The file API blocks lexical traversal but follows symlinks and has no size limits.
- DevAuth activates whenever `GITHUB_CLIENT_ID` is empty; there is no `APP_ENV=production` guard.
- `.env` remains in git history; rotate all secrets and purge history before production use.
- No automated tests; `go test ./...` has no test files and `npm test` exits 1.
- The frontend does not type-check or build, and `frontend` is a gitlink without `.gitmodules`.
- Docs still drift in `docs/SETUP.md` (host Postgres `5432` vs actual `5434`, references to removed test files) and `docs/ARCHITECTURE.md` (removed `logs_integration_test.go`).
