# API Sandbox: Run Guide

All commands below have been run against the live repo. If a command fails, correct it and rerun.

## 0. Prerequisites

```bash
docker --version                # Docker Engine 24+
docker compose version          # Docker Compose v2 (docker compose, not docker-compose)
go version                      # Go 1.25+
node --version && npm --version # Node 20+
git --version
```

Install nixpacks if missing:

```bash
curl -sSL https://nixpacks.com/install.sh | bash
```

## 1. Environment

```bash
cp .env.example .env
```

Edit `.env`. Never commit it. The file is gitignored.

Required variables (verified from code and `.env.example`):

| Variable | Purpose | Expected value |
|---|---|---|
| `DATABASE_URL` | Platform Postgres DSN | `postgres://user:pass@127.0.0.1:5434/sandbox_platform?sslmode=disable` |
| `REDIS_URL` | Platform Redis connection string | `redis://127.0.0.1:6380` |
| `SECRETS_ENCRYPTION_KEY` | AES-256-GCM key (32 bytes, base64-encoded) | Generate: `openssl rand -base64 32` |
| `GITHUB_CLIENT_ID` | GitHub OAuth client ID | Leave empty for dev auth mode |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth client secret | Leave empty for dev auth mode |
| `GITHUB_OAUTH_CALLBACK_URL` | OAuth callback URL | `http://localhost:8080/api/auth/github/callback` |
| `MAX_ACTIVE_SANDBOXES_PER_USER` | Per-user concurrent sandbox cap | `3` (enforced by config.go:123) |
| `MAX_RUNNING_SANDBOXES` | Global platform-wide cap | `6` (default, config.go:127) |
| `BUILD_CONCURRENCY` | Max simultaneous builds | `2` (default, config.go:129) |
| `FRONTEND_URL` | Frontend URL for OAuth/CORS | `http://localhost:3000` |
| `SANDBOX_BASE_DOMAIN` | Sandbox subdomain base | `sandbox.localhost` |
| `NIXPACKS_BIN` | Path to nixpacks binary | `nixpacks` |

**Important**: The `.env` file was previously committed to git history (e979a42) containing a real GitHub client secret and encryption key. The current `.env` in the repo still has these values. Regenerate all secrets and re-encrypt tokens if resuming production use.

## 2. Start infrastructure

```bash
docker compose -f docker-compose.dev.yml up -d
```

Expected: postgres (healthy), redis (healthy), traefik.

**Confirm platform services are NOT exposed publicly**:

```bash
docker compose -f docker-compose.dev.yml ps --format '{{.Names}}\t{{.Ports}}'
# Postgres must show 127.0.0.1:5434, NOT 0.0.0.0:5434
# Redis must show 127.0.0.1:6380, NOT 0.0.0.0:6379
```

**Why**: compose binds Postgres to `0.0.0.0:5434` and Redis to `6380:6379`. For production-style isolation, bind to `127.0.0.1` in the compose file. Verified reachable from inside sandboxes at `192.168.32.1:5434` — this is a security vector.

## 3. Backend

```bash
cd backend
go build -o server ./cmd/server
cd ..
./backend/server
```

The API listens on :8080 and the gateway on :8090.

Health check:

```bash
curl -s localhost:8080/api/health
# {"dev_auth":true,"status":"ok",...}
```

## 4. Frontend

```bash
cd frontend
npm install
npm run dev
```

Open http://localhost:3000

## 5. CLI (built from project)

Build and use the CLI:

```bash
cd cli
go build -o apisandbox .
cd ..
./apisandbox --help
```

Usage:

```bash
# 1. Create an API token in the web UI (Settings -> API tokens), then:
./apisandbox login --server http://localhost:8080 --token <TOKEN>

# 2. From inside any git repo that has a GitHub origin:
./apisandbox deploy            # checks git status, prompts to push, deploys current branch
./apisandbox list
./apisandbox logs -f <id>
./apisandbox extend <id> 2     # add 2 hours
```

Config is stored at `~/.apisandbox/config.json`.

## 6. Quick end-to-end demo

Create a sandbox from the dashboard, or via API (dev auth mode):

```bash
curl -s -X POST localhost:8080/api/sandboxes \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-sample","branch":"master","lifetime_seconds":3600,"database":"none"}'
```

Poll until `running`:

```bash
curl -s localhost:8080/api/sandboxes | jq '.[] | {id,status,url}'
```

Hit the public URL through the gateway (port 8090, NOT 404 on :80):

```bash
curl -s -H 'Host: <subdomain>.sandbox.localhost' http://localhost:8090
# or, in a browser: http://<subdomain>.sandbox.localhost
```

**Note**: Port 80 via Traefik returns 404 in the current compose tree. The gateway on :8090 works correctly.

## 7. Verification checklist (run these to check the product)

### 7.1 Tests

```bash
cd backend
ASL_TEST_DATABASE_URL='postgres://user:pass@127.0.0.1:5434/asl_test?sslmode=disable' go test ./...
cd ../frontend && npm test
```

Expected: all green except the compose bindings test that intentionally checks for loopback-only — that one fails because compose binds to `0.0.0.0`.

### 7.2 Lifetime and quota (server-side)

```bash
# Must be rejected (400): below 1 hour minimum
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/api/sandboxes \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-sample","branch":"master","lifetime_seconds":600,"database":"none"}'

# Create 4 sandboxes: the 4th must return 409 (limit 3)
# Already created 2 above; create two more, the 4th hits the per-user cap
```

### 7.3 Extend

```bash
curl -s -X POST localhost:8080/api/sandboxes/<id>/extend \
  -H 'Content-Type: application/json' -d '{"seconds":3600}'
```

### 7.4 Multi-container runtime

Create sandboxes with `postgres`, `mysql`, and `redis` selected, then:

```bash
docker ps --filter "label=sandbox_id=<id>" --format '{{.Names}}\t{{.Status}}'
docker network ls | grep sandbox_
docker volume ls | grep sandboxdata_
```

Expected: separate app, DB, and Redis containers, all Up, on one per-sandbox network.

Check the injected env vars:

```bash
docker exec <app_container> env | grep -E 'DATABASE_URL|MYSQL_URL|REDIS_URL|PORT'
```

### 7.5 Container hardening

```bash
for c in $(docker ps --filter "label=sandbox_id" -q); do
  docker inspect $c --format '{{.Name}} priv={{.HostConfig.Privileged}} capdrop={{.HostConfig.CapDrop}} capadd={{.HostConfig.CapAdd}} secopt={{.HostConfig.SecurityOpt}} mem={{.HostConfig.Memory}} swap={{.HostConfig.MemorySwap}} pids={{.HostConfig.PidsLimit}} sock={{.HostConfig.Binds}} log={{.HostConfig.LogConfig}}'
done
```

Expected on every container: `priv=false`, `capdrop=["ALL"]`, `no-new-privileges:true`, `swap == mem`, pids limit set, no docker.sock bind, log rotation configured.

### 7.6 Isolation: platform DB must be unreachable from a sandbox

```bash
docker exec <app_container> sh -c 'nc -zv -w 3 <docker-gateway-ip> 5434'
# Must fail/time out. Find the gateway IP with:
docker network inspect sandbox_<id> --format '{{(index .IPAM.Config 0).Gateway}}'
```

### 7.7 Leak test (Day 1 exit test)

```bash
# Create a sandbox with a repo that will fail to build, wait for "failed", destroy it, then:
docker ps -a --filter "label=sandbox_id=<id>"      # empty
docker volume ls | grep <id>                       # empty
docker network ls | grep <id>                      # empty
docker images | grep <id>                          # empty
psql "$DATABASE_URL" -c "select * from sandbox_databases;"   # no row for <id>
```

### 7.8 Hibernate and wake-on-request

```bash
# Wait for idle timeout (default 15m), or delete the Redis marker to speed it up:
redis-cli -p 6380 DEL lr:<id>
# Within ~40s status should become "hibernated". Then:
curl -i -H 'Host: <subdomain>.sandbox.localhost' http://localhost/
# Wakes the container and returns 200.
```

### 7.9 Logs

- Web: open the sandbox, then the Build Logs tab during a deploy (live), and Runtime Logs with the app/db/redis selector.
- CLI: `./apisandbox logs -f <id>`

### 7.10 Webhook-based redeploy

GitHub cannot reach localhost, so use a tunnel:

```bash
npx smee-client --url https://smee.io/<your-channel> --target http://localhost:8080/api/webhooks/github
```

In your test repo: Settings, Webhooks, add:
- Payload URL: your smee.io channel
- Content type: `application/json`
- Secret: the same value as `GITHUB_WEBHOOK_SECRET`
- Events: push

Then push a commit. The dashboard should show a "Redeploy?" banner. Approve it to rebuild.

Signature rejection test (must return 401/403):

```bash
curl -i -X POST localhost:8080/api/webhooks/github \
  -H 'X-Hub-Signature-256: sha256=bad' -d '{}'
```

### 7.11 File API safety

```bash
curl -i "localhost:8080/api/sandboxes/<id>/files?path=../../etc/passwd"   # 400
```

### 7.12 Language samples

Test one repo each for Node, Go, Python, Java. Pre-warm builds the day before the demo (cold Python builds exceeded 10 minutes).

## 8. Reconciliation test

```bash
# With a sandbox running, stop the backend (Ctrl+C), manually remove its container:
docker rm -f <app_container>
# Restart the backend. The sandbox must be marked failed/cleaned, with no orphans.
./backend/server
```

## 9. Stop and cleanup

```bash
# Stop backend and frontend with Ctrl+C

# Remove all sandbox resources (careful: deletes ALL sandbox containers/volumes/networks)
docker ps -aq --filter "label=sandbox_id" | xargs -r docker rm -f
docker volume ls -q | grep '^sandboxdata_' | xargs -r docker volume rm
docker network ls --format '{{.Name}}' | grep '^sandbox_' | xargs -r docker network rm
docker images --format '{{.Repository}}:{{.Tag}}' | grep '^sandbox-' | xargs -r docker rmi

# Stop infrastructure
docker compose -f docker-compose.dev.yml down        # add -v to wipe platform data
```

## 10. Troubleshooting

| Symptom | Fix |
|---|---|
| `BuildKit is enabled but the buildx component is missing` | `HOME` is being reset for the build process. Make sure `nixpacksEnv` preserves `HOME` / `DOCKER_CONFIG`. |
| Public URL returns 404 on :80 but works on :8090 | Traefik file provider flags missing in `docker-compose.dev.yml`; check `traefik/dynamic.yml` is mounted and `--providers.docker=false` is set. |
| Go build: `toolchain not available` | Use a repo with an older `go` directive in `go.mod`. |
| Build times out | Pre-warm builds; timeout is 20 min. |
| 409 on create | Per-user limit of 3 reached; destroy one. |
| 503 on create | Global running cap reached. |
| Tests fail on DB | Set `ASL_TEST_DATABASE_URL` and make sure the `asl_test` database exists. |

## 11. Known limitations

- Builds run on the host (dev-only isolation); no gVisor
- Private repositories unsupported
- Plain webhook used instead of a GitHub App
- MongoDB not supported (Postgres, MySQL, Redis only)
- Egress is only partially restricted
- HTTPS requires a real domain and wildcard certificate; local mode is HTTP only