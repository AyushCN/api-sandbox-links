# Setup (local dev)

Verified environment: Linux, Go 1.26, Node 24, Docker 29 + Compose 2.x, nixpacks 1.x.
The steps below target this box but transfer to any Linux/macOS dev machine.

`backend/go.mod` declares `go 1.26.0`, so a 1.25 toolchain still builds — Go's
`GOTOOLCHAIN=auto` default fetches 1.26.0 on demand. The version that actually
compiles the code is the one `go version` reports from `backend/`, not the one
you started with.

> There is **no Makefile** despite older references to `make bootstrap`/`make up` —
> use the commands below directly. `go.mod` lives in `backend/`, so build from
> `backend/`; the backend reads `.env` relative to its **working directory**, so
> run the binary from the repo root.

## 1. Prerequisites

- Docker engine running (needed for builder image compilation and sandbox containers).
- **Railpack (Default builder)**:
  `curl -sSL https://railpack.com/install.sh | sh`, then confirm with `railpack --version`.
  Point the backend at it via `RAILPACK_BIN` (defaults to `railpack` on PATH).
  Requires BuildKit running on `BUILDKIT_HOST` (defaults to `tcp://127.0.0.1:1234`, provided by `docker-compose.dev.yml`).
- **Nixpacks (Supported deprecated fallback)**:
  `curl -sSL https://nixpacks.com/install.sh | bash`, then confirm with `nixpacks --version`.
  Configure `BUILDER=nixpacks` in `.env` if needed during the deprecation transition. Point the backend at it via `NIXPACKS_BIN` (defaults to `nixpacks` on PATH).
- Redis is provided by compose; the host's own `:6379`, if any, is never used.

## 2. Environment — `.env`

Copy `.env.example` (the one at the **repo root**) to `.env` and fill it in:

```bash
cp .env.example .env
```

Required values (backend refuses to start without them):

| Key | Value / note |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@localhost:5432/sandbox_platform` — matches `docker-compose.dev.yml` |
| `REDIS_URL` | `redis://localhost:6380` |
| `SECRETS_ENCRYPTION_KEY` | `openssl rand -base64 32`. Rotating it invalidates stored env vars and GitHub tokens |

Dev-auth mode vs real OAuth — leave `GITHUB_CLIENT_ID` empty for dev auth; the API
auto-seeds a `devuser` and every `requireUser` request is auto-assigned it (no
OAuth round-trip). Set real `GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET` to enable
GitHub single sign-on and push-from-sandbox (section 5).

The rest of the keys are optional overrides (`DOCKER_HOST`, `SANDBOX_WORK_DIR`,
`BACKEND_PORT`, `GATEWAY_PORT`, `FRONTEND_URL`, `SANDBOX_BASE_DOMAIN`,
`NIXPACKS_BIN`, `GEMINI_API_KEY`, `MAX_LIFETIME_SECONDS`). Defaults live in
`backend/internal/config/config.go`.

## 3. Infrastructure — `docker-compose.dev.yml`

```bash
docker compose -f docker-compose.dev.yml up -d
```

| Service | Host port | Notes |
|---|---|---|
| Postgres | `5432` | platform metadata (`user:pass`, db `sandbox_platform`) |
| Redis | `6380` | hot state (idle TTL keys, newest live metric per sandbox) |
| Traefik | `80`, `8081` | sandbox edge (`:80`), dashboard (`:8081`) |
| BuildKit | `1234` | BuildKit daemon for Railpack builds (default builder) |

Traefik runs with the Docker provider **disabled** and a file-provider wildcard
router (`traefik/dynamic.yml`) that sends every `*.sandbox.localhost` host to the
backend's gateway on the host at `:8090` (via `extra_hosts: host.docker.internal`).
Dashboard: `http://127.0.0.1:8081/dashboard/`.

## 4. Backend

Build from `backend/` (that is where `go.mod` lives):

```bash
cd api-sandbox-links/backend
go build ./... && go vet ./...
go test -race ./...     # see the note below
go build -o server ./cmd/server
```

The tests need no backend process, but four suites reach for real infrastructure
and skip themselves when it is absent:

- `internal/orchestrator/logs_integration_test.go` and
  `internal/orchestrator/teardown_test.go` drive a real **Docker** daemon,
  starting throwaway containers to check the frame demux, the per-fd tail
  backfill, and teardown against a real Postgres. Honour `ASL_TEST_IMAGE`
  (default `node:20-alpine`) and `DOCKER_HOST`. Point it at a small local image
  so it does not pull 800 MB.
- `internal/api/websocket_e2e_test.go` opens **real HTTP + WebSocket** connections
  against the real chi router — including `requireUser` with a genuinely sealed
  session cookie, because a refused handshake is the failure that a
  context-injecting test double cannot see. It needs both Docker and Postgres.
- `internal/db/queries_test.go` and `internal/db/activation_test.go` need a real
  **Postgres**, because the migrations, the `ON CONFLICT` clause, the
  newest-N window and the activation upsert are not things a unit test can check.
  Set `ASL_TEST_DATABASE_URL`:

  ```bash
  docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=pw postgres:16-alpine
  ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' \
    go test ./internal/db/ ./internal/api/ ./internal/orchestrator/
  ```

  All three packages share that one database and `go test` runs them in
  parallel, so each fixture uses a **disjoint** `github_id` range (`internal/db`
  `1–4` and `101–105`, `internal/orchestrator` `201–204`, `internal/api`
  `301–306`). Register a cleanup with `t.Cleanup` **before** the first insert —
  a fatal between two inserts otherwise leaves rows that fail the next run.

Then run from the **repo root** (godotenv loads `./.env` from the CWD — running
from `backend/` makes config fail):

```bash
cd api-sandbox-links
./backend/server &              # :8080 API + :8090 gateway
```

Health check — and run it before debugging anything in the UI:

```bash
curl http://localhost:8080/api/health
# {"status":"ok","time":"…","dev_auth":true}
```

A curl that prints nothing and exits non-zero (`curl -o /dev/null -w '%{http_code}'`
prints `000`) means **nothing is listening**, which is a different problem from a
bug. The log panel cannot tell you this: a WebSocket to a dead port is reported
by the browser as close code **1006**, exactly as a dropped connection would be,
so the panel will sit there retrying with no log lines while the API is simply
not running. Check the process and the ports before reading any code:

```bash
ss -ltn | grep -E ':(8080|3000|8090)'   # both listeners + the frontend
pgrep -af './backend/server'
docker ps -a                            # exited containers are the usual culprit
```

To stop the backend, `kill $(pgrep -f './backend/server')` (never `pkill -f
"./backend/server"` from a shell whose own command line matches the pattern).

## 5. Frontend

```bash
cd frontend
npm install
npm run dev      # http://localhost:3000
```

- `NEXT_PUBLIC_API_BASE` defaults to `http://localhost:8080`; override in
  `frontend/.env.local` (that file's template is `frontend/.env.example`). An
  empty or whitespace-only value is treated as unset, since the WebSocket panels
  would otherwise build a relative URL the browser refuses to open.
- The build gate is `npm run build` (Next 16 App Router, Tailwind v4).
- Lint/typecheck: `npm run lint` and `npx tsc --noEmit`.
- Frontend unit tests: `npm test` (Vitest, `npm run test:watch` to iterate).
  The suites cover the pure helpers in `src/lib` — reconnect policy in
  `ws.test.ts` and frame normalization in `frames.test.ts` — and need neither a
  DOM nor a running backend. A component test will need `environment: "jsdom"`
  plus `@vitejs/plugin-react` and `@testing-library/react`; see
  `frontend/node_modules/next/dist/docs/01-app/02-guides/testing/vitest.md`.
- `npx tsc --noEmit` needs the generated `frontend/.next/types` to exist. If it
  fails with missing Next route types, run `npm run build` once first.

## 6. GitHub OAuth (optional; skip to stay in dev auth)

1. Create an OAuth App at GitHub → Settings → Developer settings → OAuth Apps.
2. Homepage URL: `http://localhost:3000`. Authorization callback URL must **byte
   -for-byte match** `GITHUB_OAUTH_CALLBACK_URL` from `.env`
   (`http://localhost:8080/api/auth/github/callback`) — no trailing slash, same
   host/port. Mismatches surface as GitHub's `redirect_uri is not associated
   with this application`.
3. Set `GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET` in `.env`, restart the backend.
4. Flow: `/api/auth/github` → GitHub consent → `/api/auth/github/callback` →
   `asl_session` cookie set → redirect to `http://localhost:3000/dashboard?authed=1`.
   Scopes requested: `read:user user:email repo`.

## 7. First end-to-end run

1. Open the dashboard → paste `https://github.com/heroku/node-js-sample`
   (branch `master`) → **Create**.
2. Watch status go `queued → building → running` (first-ever nixpacks build can
   take a couple of minutes; it installs the Nix provider).
3. Open the **public URL** (`<subdomain>.sandbox.localhost` → `:80` → Traefik →
   gateway `:8090` → container) — you should get the app's response.
4. Wait out the idle TTL (or set `idle_timeout_seconds` low when creating), then
   hit the URL again → observe wake-on-request flipping back to `running`.
5. Use the editor tab to change a file → **Save & redeploy** → a `save_redeploy`
   deployment fires; the **Git** tab can commit & push to a `sandbox-edits-*`
   branch (push needs real OAuth).

## Troubleshooting

- `config: DATABASE_URL and REDIS_URL are required` → you launched from the wrong
  directory; start the binary from `api-sandbox-links/`.
- Backend can't reach Postgres (`connection refused` on 5432) though the container
  looks healthy → the port mapping silently dropped; fix with
  `docker compose -f docker-compose.dev.yml up -d --force-recreate postgres`.
- `relation "env_vars" does not exist` → a GORM table-name override is missing;
  `EnvVar`/`LifetimeWarning` map to `sandbox_*` tables (see DATABASE.md).
- Deployed link shows **"Sandbox container is not responding"** → the app
  hardcodes a port the platform didn't predict. The deploy pipeline probes
  listening ports and records the real one (see ARCHITECTURE.md); worktrees that
  bind odd ports may need a redeploy after that fix lands.
- `curl` to `localhost` silently going through an HTTP proxy → append
  `--noproxy '*'`; Traefik logs may show host `-` (no router) for the same reason.
- Docker provider router errors in the Traefik logs are **expected** (the provider
  is disabled by design).
- `/api/auth/github` returns 501 → OAuth config missing; either fill
  `GITHUB_CLIENT_ID`/`SECRET` or use dev auth mode.
- **Log panel shows `connection lost (code 1006)` in a loop and never prints a
  line** → work outward, in this order, because all three look identical from the
  panel:
  1. `curl -o /dev/null -w '%{http_code}' http://localhost:8080/api/health`. `000`
     means nothing is listening — start the backend. This is the most common
     cause and the least obvious, because the code is the same one a dropped
     connection produces.
  2. No error log from `logs: dropping client that stopped reading` on the server
     means the server never objected, so the socket likely never opened.
  3. The panel now probes `GET /api/me` when a handshake fails and says
     *"Your session has expired"* if the cookie is bad. If it does not appear,
     the cookie is fine and the handshake was refused for another reason —
     check `NEXT_PUBLIC_API_BASE` resolves to a host the browser considers
     same-site as the frontend, or the session cookie is not attached to the
     upgrade at all.
- **Log panel is blank but the app is obviously running** → the sandbox row must
  be `running` with a container id; only then do `log` frames arrive. A
  `hibernated` sandbox sends `unavailable` with a reason. Check with
  `GET /api/sandboxes/{id}` before suspecting the socket.