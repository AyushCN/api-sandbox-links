# API Sandbox Links: Run Guide

## Quickstart

```bash
# 1. Ensure infra is up
docker compose -f docker-compose.dev.yml up -d  # Postgres, Redis, Traefik

# 2. Build
cd backend && go build -o server ./cmd/server && cd ..

# 3. Run from repo root (.env is read from the working directory)
./backend/server

# 4. In another shell
curl -s localhost:8080/api/health   # {"dev_auth":false,"status":"ok",...}
```

For dev auth instead of real OAuth, prefix the run with empty credentials:
`GITHUB_CLIENT_ID= GITHUB_CLIENT_SECRET= ./backend/server`

## Full guide

### 0. Prereqs
```bash
docker --version              # Docker 29.1.3+
docker compose version        # Compose 2.40.3+
go version                    # go1.26.0+
node --version && npm --version # Node 24.15.0 / npm 12.0.2
git --version
nixpacks --version            # nixpacks 1.41.0
```

### 1. Environment
The root `.env` and `.env.example` now exist (`.env` is gitignored). Edit `.env` as needed — never commit it.

Key values for local run:

| Variable | Local default |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@localhost:5434/sandbox_platform` |
| `REDIS_URL` | `redis://localhost:6380` |
| `GITHUB_CLIENT_ID` | Empty (dev auth) |
| `GITHUB_CLIENT_SECRET` | Empty |
| `SECRETS_ENCRYPTION_KEY` | Generate: `openssl rand -base64 32` |
| `SANDBOX_BASE_DOMAIN` | `sandbox.localhost` |
| `BACKEND_PORT` | `8080` |
| `GATEWAY_PORT` | `8090` |

> The DB credentials must match `docker-compose.dev.yml`: `POSTGRES_USER=user`,
> `POSTGRES_PASSWORD=pass`, port `5434`. Using `postgres:password` fails with
> `password authentication failed for user "postgres"` (SQLSTATE 28P01).
>
> `GITHUB_CLIENT_ID` set means real OAuth (`dev_auth:false`); empty means dev
> auth. Either way the server boots.

### 2. Start infrastructure
```bash
docker compose -f docker-compose.dev.yml up -d
# Services: sandbox-platform-db, sandbox-platform-redis, sandbox-platform-traefik
```

### 3. Backend
```bash
cd backend && go build ./... && go build -o server ./cmd/server
cd ..

# Run from the repo root so .env is found
./backend/server

# Health check
curl -s localhost:8080/api/health
# {"dev_auth":false,"status":"ok","time":"..."}
```

Expected startup log:

```
configuration loaded  app_env=development  build_mode=  dev_auth=false
nixpacks detected     version=nixpacks 1.41.0
backend listening     api=127.0.0.1:8080  gateway=:8090
```

Migrations run automatically on boot. Verify the schema:

```bash
docker exec sandbox-platform-db psql -U user -d sandbox_platform -c '\dt'
# 10 tables incl. sandboxes, deployments, users, schema_migrations
```

### 4. Frontend
```bash
cd frontend && npm install && npm run dev
# Open http://localhost:3000
```

### 5. Quick E2E
```bash
curl -s -X POST localhost:8080/api/sandboxes/ \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":3600,"idle_timeout_seconds":900,"database":"none"}'

# Open public URL via Traefik on :80
# Browser: http://<subdomain>.sandbox.localhost
```

### 6. Stop
```bash
# Destroy sandboxes
curl -s -X POST localhost:8080/api/sandboxes/<id>/actions/destroy

# Stop infrastructure (preserves DB volume)
docker compose -f docker-compose.dev.yml stop

# Remove local binary
rm -f backend/server
```

### 7. Build verification
```bash
cd backend && go build -o /dev/null ./...  # exits 0
```