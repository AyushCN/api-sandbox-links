# Production Deployment

This guide covers deploying the API Sandbox Links platform to production with Traefik, Caddy, Docker, and systemd.

## Prerequisites

- Linux host with Docker Engine 24+, Docker Compose v2, Go 1.26+, Node 20+, npm 12+
- Wildcard DNS A/AAAA record for `*.SANDBOX_BASE_DOMAIN` pointing to this host
- Cloudflare account with API token for wildcard TLS (DNS-01 challenge)
- Minimum 10GB free disk space for sandbox work directory
- 2GB+ RAM for the host (sandbox builds consume additional resources)

## 1. Environment Configuration

Copy `deploy/.env.production.example` to `deploy/.env` and fill in all values:

```bash
cp deploy/.env.production.example deploy/.env
```

Required `.env` values:

| Variable | Description |
|---|---|
| `APP_DOMAIN` | Public domain for the platform (e.g. `sandbox.example.com`) |
| `SANDBOX_BASE_DOMAIN` | Base domain for sandbox subdomains (e.g. `sandbox.localhost`) |
| `ACME_EMAIL` | Email for Let's Encrypt / Cloudflare DNS-01 |
| `DATABASE_URL` | Postgres DSN for platform metadata |
| `REDIS_URL` | Redis connection string for hot state |
| `SECRETS_ENCRYPTION_KEY` | Base64-encoded 32-byte AES-256 key |
| `GITHUB_CLIENT_ID` | GitHub OAuth client ID |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth client secret |
| `GITHUB_OAUTH_CALLBACK_URL` | OAuth callback URL (e.g. `https://sandbox.example.com/api/auth/github/callback`) |
| `NIXPACKS_BIN` | Path to nixpacks binary (e.g. `/usr/local/bin/nixpacks`) |
| `FRONTEND_URL` | Frontend app URL (e.g. `https://sandbox.example.com`) |
| `BUILD_MODE` | Must be `host-docker` for production |
| `SANDBOX_SCHEME` | Must be `https` for production |
| `BACKEND_PORT` | API port (default 8080) |
| `GATEWAY_PORT` | Traefik gateway port (default 8090) |
| `DISK_MIN_FREE_GB` | Minimum free GB for sandbox work dir (default 5) |
| `MAX_LIFETIME_SECONDS` | Max sandbox lifetime (default 604800 = 7 days) |
| `MAX_ACTIVE_SANDBOXES_PER_USER` | Per-user cap (default 3) |
| `MAX_RUNNING_SANDBOXES` | Global cap (default 6) |
| `BUILD_CONCURRENCY` | Simultaneous builds (default 2) |
| `IMAGE_PRUNE_HOURS` | Image hygiene interval (default 24) |

## 2. Install Script

Run the install script from the repository root as root:

```bash
sudo deploy/install.sh
```

The script will:

1. Verify prerequisites (docker, go, node, nixpacks)
2. Build the backend binary
3. Install systemd service `api-sandbox-backend`
4. Start the data plane with `docker compose -f deploy/docker-compose.prod.yml up -d --build`
5. Wait for backend readiness
6. Run `doctor` and display status

## 3. Production Docker Compose

The production data plane is defined in `deploy/docker-compose.prod.yml`:

```yaml
services:
  caddy:
    image: api-sandbox-links/caddy:latest
    restart: unless-stopped
    ports:
      - "80:80"    # HTTP -> Caddy edge
      - "443:443"  # HTTPS -> Caddy edge
    environment:
      - CLOUDFLARE_DNS_API_TOKEN=${CLOUDFLARE_DNS_API_TOKEN}
      - CF_API_TOKEN=${CLOUDFLARE_API_TOKEN}
    volumes:
      - caddy_data:/data
      - caddy_config:/config
    # Caddy automatically provisions wildcard TLS via Cloudflare DNS-01

  traefik:
    image: traefik:v3.1
    container_name: sandbox-platform-traefik
    restart: unless-stopped
    # Docker provider disabled; using file provider only
    command:
      - --providers.file.filename=/etc/traefik/dynamic.yml
      - --api.insecure=false
      - --api.dashboard=false
      - --entrypoints.web.address=:80
    labels:
      - "traefik.enable=false"
    extra_hosts:
      - "host.docker.internal:host-gateway"

  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile
    container_name: sandbox-platform-backend
    restart: unless-stopped
    environment:
      - DATABASE_URL=${DATABASE_URL}
      - REDIS_URL=${REDIS_URL}
      - SECRETS_ENCRYPTION_KEY=${SECRETS_ENCRYPTION_KEY}
      - GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID}
      - GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET}
      - GITHUB_OAUTH_CALLBACK_URL=${GITHUB_OAUTH_CALLBACK_URL}
      - APP_ENV=production
      - SANDBOX_SCHEME=https
      - SANDBOX_BASE_DOMAIN=${SANDBOX_BASE_DOMAIN}
      - BUILD_MODE=host-docker
    depends_on:
      - db
      - redis
    # API bound to 127.0.0.1; Traefik routes via gateway
    ports:
      - "127.0.0.1:8080:8080"  # API only on loopback
    volumes:
      - ./backend:/go/src/api-sandbox-links:ro
    working_dir: /go/src/api-sandbox-links

  db:
    image: postgres:16
    container_name: sandbox-platform-db
    restart: unless-stopped
    environment:
      - POSTGRES_USER=user
      - POSTGRES_PASSWORD=pass
      - POSTGRES_DB=sandbox_platform
    volumes:
      - db_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U user -d sandbox_platform"]
      interval: 10s
      timeout: 5s
      retries: 5
    ports:
      - "127.0.0.1:5434:5432"  # Postgres loopback-only

  redis:
    image: redis:7-alpine
    container_name: sandbox-platform-redis
    restart: unless-stopped
    command: ["redis-server", "--save", "", "--append-only no"]
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 3s
      retries: 5
    ports:
      - "127.0.0.1:6380:6379"  # Redis loopback-only
```

**Key production settings:**

- `POSTGRES` and `REDIS` are bound to `127.0.0.1` only (not `0.0.0.0`) — this is intentional for isolation
- `traefik` has `--api.insecure=false` and `--api.dashboard=false` disabled
- `backend` service binds to `127.0.0.1:8080` — reachable from inside sandboxes via the bridge gateway
- `caddy` handles wildcard TLS termination at the edge

## 4. Systemd Service

The install script copies and enables `deploy/systemd/api-sandbox-backend.service`:

```ini
[Unit]
Description=API Sandbox Links Backend
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/opt/api-sandbox/server
Restart=on-failure
WorkingDirectory=/go/src/api-sandbox-links
EnvironmentFile=/etc/api-sandbox/backend.env

# Security: drop privileges after binding ports
User=sandbox
Group=sandbox

# Resource limits
MemoryMax=512M
CPUQuota=200000  # 2 CPU cores

# Timeout for graceful shutdown
TimeoutStopSec=10

# Hardening
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
```

## 5. Firewall Hardening

Run the firewall hardening script after deployment:

```bash
sudo deploy/harden-firewall.sh
```

This blocks all inbound traffic except:
- SSH (port 22)
- HTTP (port 80) — for Let's Encrypt DNS-01 challenge
- HTTPS (port 443) — for production sandbox traffic

It explicitly blocks:
- Port 8080 — backend API (loopback only, not publicly exposed)
- Port 8090 — Traefik gateway (loopback only)
- Port 5432/5434 — Postgres (loopback only)
- Port 6379/6380 — Redis (loopback only)

## 6. Caddy Configuration

Caddy uses the Cloudflare DNS plugin for wildcard TLS. The `Caddyfile` is generated from `deploy/.env`:

```
${APP_DOMAIN} {
  tls dns cloudflare ${CLOUDFLARE_DNS_API_TOKEN}
  reverse_proxy ${BACKEND_HOST}:${BACKEND_PORT}
}

*.${SANDBOX_BASE_DOMAIN} {
  tls dns cloudflare ${CLOUDFLARE_DNS_API_TOKEN}
  # Sandbox traffic is routed through Traefik gateway on 127.0.0.1:8090
  reverse_proxy 127.0.0.1:${GATEWAY_PORT}
}
```

## 7. Verification

After deployment, verify:

```bash
# Check backend is healthy
curl -fsS http://127.0.0.1:8080/api/ready

# List sandboxes
curl -s http://127.0.0.1:8080/api/sandboxes/ | python3 -m json.tool

# Run doctor
/opt/api-sandbox/server doctor

# Check Traefik routes
docker compose -f deploy/docker-compose.prod.yml ps
```

## 8. Stop and Cleanup

```bash
# Stop application processes
sudo systemctl stop api-sandbox-backend

# Remove sandboxes through the API (preserves platform data)
curl -s -X POST http://127.0.0.1:8080/api/sandboxes/<id>/actions/destroy

# Stop infrastructure (preserves database volume)
docker compose -f deploy/docker-compose.prod.yml stop

# Remove everything including platform data
docker compose -f deploy/docker-compose.prod.yml down -v
```

## 9. Known Limitations (Production)

- Builds run on the host Docker daemon (no gVisor or user-container runtime isolation)
- Private repositories unsupported (git clone uses no auth)
- MongoDB not supported (Postgres, MySQL only)
- Egress from sandboxes is unrestricted
- HTTPS requires a real domain with wildcard certificate; local mode is HTTP only
- The 20-minute deploy timeout does not reliably terminate orphaned Docker/nix processes
- No rate limiting on auth, create, deploy, or general API
- Session cookies are `HttpOnly` and `SameSite=Lax` but not `Secure` behind Traefik TLS termination
- The Traefik dashboard/API is disabled in production (`--api.insecure=false`)
- The sandbox backend API is reachable on the bridge gateway at `:8080` from inside sandboxes