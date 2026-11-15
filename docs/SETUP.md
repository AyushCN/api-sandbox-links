# Setup (Local Development)

Verified environment: Linux (Ubuntu/Debian/Fedora), Go 1.25+, Node.js 20+, Docker Engine 28+ & Docker Compose v2.
The steps below target Linux environments but transfer directly to macOS dev machines.

`backend/go.mod` targets Go 1.26 (compatible with Go 1.25+ toolchains). Go's standard `GOTOOLCHAIN=auto` automatically manages newer minor toolchains on demand.

> [!NOTE]
> There is **no Makefile** — use the commands below directly. Because `go.mod` lives in `backend/`, compile from `backend/`; the backend loads `.env` relative to its **current working directory**, so always execute the binary from the **repository root**.

---

## 1. Prerequisites

- **Docker Engine & Docker Compose v2**: Required for container orchestration, sidecar databases, and network isolation.
- **Railpack (Default Builder Engine)**:
  ```bash
  curl -sSL https://railpack.com/install.sh | sh
  railpack --version
  ```
  Point the backend at the binary via `RAILPACK_BIN` (defaults to `railpack` on `$PATH`).
  Railpack requires a running BuildKit daemon configured via `BUILDKIT_HOST` (defaults to `tcp://127.0.0.1:1234`, provisioned automatically by `docker-compose.dev.yml`).
- **Nixpacks (Supported Deprecated Fallback)**:
  ```bash
  curl -sSL https://nixpacks.com/install.sh | bash
  nixpacks --version
  ```
  Set `BUILDER=nixpacks` in `.env` if needed during migration. Emits structured deprecation logs.
- **Node.js 20+ & npm**: Required for running the Next.js 16 frontend console.
- **Go 1.25+**: Required to compile the Go backend control plane.

---

## 2. Environment Configuration — `.env`

Copy `.env.example` from the repository root to `.env`:

```bash
cp .env.example .env
```

### Required Configuration Keys

The backend will refuse to start if any of these are missing:

| Key | Value / Note |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@localhost:5434/sandbox_platform` (matches host port mapping in `docker-compose.dev.yml`) |
| `REDIS_URL` | `redis://localhost:6380` (matches host port mapping in `docker-compose.dev.yml`) |
| `SECRETS_ENCRYPTION_KEY` | Generate via `openssl rand -base64 32`. Rotating this key invalidates stored sandbox env vars and GitHub tokens. |

### Authentication Modes: Dev Auth vs. GitHub OAuth

- **Dev Auth Mode (Default)**: Leave `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` empty in `.env`. The backend automatically seeds and authenticates requests as `devuser` (ID `1`), allowing full access to all sandboxes, terminals, and workspace features without an OAuth flow.
- **GitHub OAuth Mode**: Set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` to enable production GitHub SSO and push-to-branch capabilities from within sandboxes (see Section 6).

### Optional Configuration Overrides

Defaults are defined in [`backend/internal/config/config.go`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/backend/internal/config/config.go):

| Key | Default | Description |
|---|---|---|
| `BACKEND_PORT` | `8080` | Control plane REST and WebSocket port |
| `GATEWAY_PORT` | `8090` | Single-flight wake-on-request proxy port |
| `FRONTEND_URL` | `http://localhost:3000` | Allowed CORS origin and OAuth redirect target |
| `SANDBOX_BASE_DOMAIN` | `sandbox.localhost` | Base domain for sandbox wildcards |
| `BUILDKIT_HOST` | `tcp://127.0.0.1:1234` | BuildKit daemon TCP socket for Railpack builds |
| `BUILDER` | `railpack` | Default builder engine (`railpack` or `nixpacks`) |
| `SANDBOX_RUNTIME` | `""` | Set to `"runsc"` to enable Google gVisor kernel sandboxing |
| `MAX_LIFETIME_SECONDS`| `604800` | Maximum lifetime cap (7 days) |

---

## 3. Infrastructure Services — `docker-compose.dev.yml`

Spin up the supporting infrastructure:

```bash
docker compose -f docker-compose.dev.yml up -d
```

### Dev Infrastructure Port Allocations

| Service | Host Port | Internal Port | Purpose |
|---|---|---|---|
| **PostgreSQL** | `5434` | `5432` | Platform metadata, users, sandboxes, deployments (`user:pass`, DB `sandbox_platform`) |
| **Redis** | `6380` | `6379` | Ephemeral hot state (idle TTL keys `lr:<id>`, live stats buffer) |
| **Traefik Edge** | `80`, `8081` | `80`, `8080` | Wildcard reverse proxy (`:80`) and admin dashboard (`http://localhost:8081/dashboard/`) |
| **BuildKit** | `1234` | `1234` | Rootless BuildKit daemon for high-performance layer-cached Railpack builds |

> [!IMPORTANT]
> - Postgres host port is mapped to **`5434`** (not `5432`) to prevent collisions with existing PostgreSQL instances running on the host machine.
> - Redis host port is mapped to **`6380`** (not `6379`) for the same reason.
> - Traefik runs with the Docker socket provider **disabled**; routing is driven exclusively via the file provider ([`traefik/dynamic.yml`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/traefik/dynamic.yml)), directing `*.sandbox.localhost` traffic to the backend gateway at `:8090`.

---

## 4. Backend Service (Control Plane & Gateway)

Build the Go backend from the `backend/` directory:

```bash
cd backend
go build ./... && go vet ./...
go build -o server ./cmd/server
cd ..
```

### Running Backend Tests

The test suites run without external dependencies by default, skipping suites that require live infrastructure unless configured:

```bash
# Run unit and race detector tests
cd backend && go test -race ./...
```

For integration testing against real Docker and Postgres:
```bash
docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=pw postgres:16-alpine
ASL_TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/postgres?sslmode=disable' \
  go test ./internal/db/ ./internal/api/ ./internal/orchestrator/
```

### Launching the Backend

Always launch from the **repository root** so `godotenv` loads `./.env` correctly:

```bash
# From api-sandbox-links/ root:
./backend/server
```

Verify backend health:
```bash
curl http://localhost:8080/api/health
# Expected output: {"status":"ok","time":"...","dev_auth":true}
```

Check active listening ports:
```bash
ss -ltn | grep -E ':(8080|8090|3000|5434|6380|1234)'
```

---

## 5. Frontend Application (Next.js 16 Console)

From the repository root, install and start the frontend:

```bash
cd frontend
npm install
npm run dev
```

Open **[http://localhost:3000](http://localhost:3000)** in your browser.

- API requests to `/api/*` are handled by Next.js BFF route handlers and proxied to the Go backend on `http://localhost:8080`.
- Verify TypeScript types anytime with:
  ```bash
  npx tsc --noEmit
  ```
- Workspace projects and collaborator mappings are persisted locally at `/tmp/api-sandbox-links/projects.json`.

---

## 6. GitHub OAuth Setup (Optional)

To enable GitHub sign-in and commit-pushing instead of local dev auth:

1. Create a GitHub OAuth App at: **GitHub Settings → Developer Settings → OAuth Apps → New OAuth App**.
2. **Homepage URL**: `http://localhost:3000`
3. **Authorization Callback URL**: `http://localhost:8080/api/auth/github/callback` (must match `GITHUB_OAUTH_CALLBACK_URL` in `.env` byte-for-byte).
4. Fill in `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` in `.env`.
5. Restart `./backend/server`.
6. Scopes requested: `read:user`, `user:email`, `repo`.

---

## 7. First End-to-End Sandbox Deployment

1. Open **[http://localhost:3000/dashboard](http://localhost:3000/dashboard)**.
2. Click **+ New Sandbox**.
3. Enter repository: `https://github.com/heroku/node-js-sample` (branch `master`).
4. Click **Create Sandbox**.
5. Watch the lifecycle transition: `queued → building → running`.
6. Open the public preview URL: `http://<subdomain>.sandbox.localhost:8090` (or through port `:80`).
7. Open the in-browser IDE: edit files, click **Save & Redeploy**, and view live container stdout/stderr.

---

## 8. Troubleshooting & Common Pitfalls

| Symptom | Root Cause | Solution |
|---|---|---|
| `config: DATABASE_URL and REDIS_URL are required` | Backend executed from `backend/` instead of repository root. | `cd ..` to repo root and run `./backend/server`. |
| `connection refused on 5434` | Postgres container not running or ports mapped incorrectly. | Run `docker compose -f docker-compose.dev.yml up -d postgres`. |
| `relation "env_vars" does not exist` | GORM table-name override omitted. | `EnvVar` maps to `sandbox_env_vars` (see [DATABASE.md](DATABASE.md)). |
| `WebSocket connection lost (code 1006)` | Backend `:8080` process is not running, or cookie is expired. | Check `curl http://localhost:8080/api/health`. If backend is up, re-authenticate. |
| Container shows "Port not responding" | Application binds a non-standard port not detected in plan. | Orchestrator probes common ports (`8080`, `3000`, `5000`, etc.) up to 90s. Ensure app listens on `0.0.0.0`. |
| `Railpack build failed: dial tcp 127.0.0.1:1234` | BuildKit daemon container is stopped. | Verify BuildKit is running: `docker compose -f docker-compose.dev.yml up -d buildkit`. |
