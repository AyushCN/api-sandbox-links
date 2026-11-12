# API Sandbox Links — Production Operations Runbook

This runbook covers the operational deployment, maintenance, disaster recovery, and troubleshooting procedures for running **API Sandbox Links** as a resilient single-node production service.

---

## 1. System Architecture & Port Allocation

API Sandbox Links runs as a single-host control plane and edge proxy managing isolated Docker containers for application sandboxes and sidecar databases.

```
       Internet / Clients
               │
               ▼
┌──────────────────────────────┐
│  Traefik Edge Reverse Proxy  │  Ports 80 / 443 (Wildcard TLS: *.sandbox.domain.com)
└──────────────┬───────────────┘
               │
       ┌───────┴────────────────────────┐
       │ (app traffic)                  │ (API traffic)
       ▼                                ▼
┌──────────────────────┐      ┌─────────────────────────────┐
│ Sandbox Wake Gateway │      │ Backend Control Plane (Chi) │
│ Port 8090 (Internal) │      │ Port 8080 (Internal)        │
└──────────┬───────────┘      └──────────────┬──────────────┘
           │                                 │
           ▼                                 ▼
┌──────────────────────┐      ┌─────────────────────────────┐
│ App Sandboxes        │      │ PostgreSQL 16 (State)       │
│ sandbox_<subdomain>  │      │ Redis 7 (AOF - Idle TTLs)   │
│ Sidecar DBs (pg/my)  │      │ BuildKit Daemon (:1234)     │
└──────────────────────┘      └─────────────────────────────┘
```

### Standard Port Allocation Table

| Service | Port | Binding | Purpose |
|---|---|---|---|
| **Traefik HTTP** | `80` | `0.0.0.0` | HTTP traffic (automatically redirects to HTTPS) |
| **Traefik HTTPS** | `443` | `0.0.0.0` | Wildcard TLS entrypoint (`*.sandbox.domain.com`) |
| **Traefik Dashboard** | `8082` | `127.0.0.1` | Internal admin and routing metrics |
| **Backend API** | `8080` | `127.0.0.1` (or internal network) | REST & WebSocket control plane |
| **Sandbox Gateway** | `8090` | `127.0.0.1` (or internal network) | Single-flight wake-on-request reverse proxy |
| **PostgreSQL** | `5432` | `127.0.0.1` (or Docker bridge) | Primary persistent data store |
| **Redis** | `6379` | `127.0.0.1` (or Docker bridge) | Ephemeral idle TTL markers & live stats cache |
| **BuildKit Daemon** | `1234` | `127.0.0.1` (or Docker bridge) | Fast rootless image builder for Railpack |

---

## 2. Server Prerequisites & Base OS Setup

### Recommended Server Specifications
- **Operating System:** Ubuntu 22.04 LTS, Ubuntu 24.04 LTS, or Debian 12.
- **CPU:** 4+ vCPUs.
- **Memory:** 8GB+ RAM (16GB+ recommended if running concurrent Railpack/BuildKit builds).
- **Disk:** 100GB+ NVMe SSD (fast I/O for Docker layers and Git checkouts).

### Base System Packages

```bash
sudo apt update && sudo apt install -y \
  ca-certificates curl gnupg lsb-release git jq gzip openssl
```

### Docker & Compose Installation

```bash
# Add Docker official GPG key and repo
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt update && sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Enable Docker on boot
sudo systemctl enable --now docker
```

---

## 3. Pluggable Builder Setup

API Sandbox Links supports two build orchestrators: **Nixpacks** (default) and **Railpack** (modern, BuildKit-backed).

### Nixpacks Installation (Default Builder)

```bash
curl -sSL https://nixpacks.com/install.sh | sudo bash
nixpacks --version
```

### Railpack Installation (Recommended Next-Gen Builder)

```bash
# Install Railpack binary
curl -sSL https://railpack.com/install.sh | sudo bash
railpack --version
```

When using `BUILDER=railpack`, ensure the BuildKit daemon is accessible. The included `docker-compose.prod.yml` and `docker-compose.dev.yml` deploy a rootless BuildKit daemon on `127.0.0.1:1234`.

---

## 4. DNS & Wildcard TLS Configuration (ACME DNS-01)

Traefik terminates TLS for all incoming sandbox traffic using Let's Encrypt Wildcard certificates via the DNS-01 challenge.

### DNS Records Setup
Create the following DNS records in your DNS provider (e.g. Cloudflare or AWS Route53):

| Type | Name | Content / Target | Proxy Status |
|---|---|---|---|
| `A` | `api.yourdomain.com` | `<SERVER_PUBLIC_IP>` | DNS Only (Gray Cloud) |
| `A` | `*.sandbox.yourdomain.com` | `<SERVER_PUBLIC_IP>` | DNS Only (Gray Cloud) |

> [!IMPORTANT]
> Because Traefik handles wildcard ACME TLS for `*.sandbox.yourdomain.com`, the DNS records MUST be set to **DNS Only** (unproxied in Cloudflare) so Let's Encrypt and TCP connections reach Traefik directly on ports 80/443.

### Cloudflare API Token Configuration
Create an API Token in Cloudflare with the following permission:
- `Zone:DNS:Edit` for your target domain zone.

Set the credentials in your `.env`:
```ini
ACME_EMAIL=admin@yourdomain.com
ACME_DNS_PROVIDER=cloudflare
CF_DNS_API_TOKEN=your_cloudflare_api_token_here
```

### Alternative Providers (e.g., AWS Route53)
To use Route53 instead of Cloudflare:
```ini
ACME_DNS_PROVIDER=route53
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
AWS_REGION=us-east-1
AWS_HOSTED_ZONE_ID=Z2F...
```

---

## 5. Production Deployment

### Option A: Complete Docker Compose Production Overlay

API Sandbox Links provides [`docker-compose.prod.yml`](file:///home/swordrookie/projects/api_sandbox_links/api-sandbox-links/docker-compose.prod.yml) which runs Traefik, PostgreSQL, Redis with AOF persistence, and BuildKit.

```bash
# 1. Clone repository
cd /opt
sudo git clone -b production https://github.com/AyushCN/api-sandbox-links.git
cd /opt/api-sandbox-links

# 2. Configure environment
cp .env.example .env
nano .env

# Required edits in .env:
# - APP_URL=https://api.yourdomain.com
# - SANDBOX_BASE_DOMAIN=sandbox.yourdomain.com
# - ENCRYPTION_KEY=$(openssl rand -hex 32)
# - SESSION_SECRET=$(openssl rand -hex 32)
# - ACME_EMAIL=ops@yourdomain.com
# - CF_DNS_API_TOKEN=<your-token>

# 3. Create persistent volumes and Traefik acme.json with strict 0600 permissions
touch traefik/acme.json
chmod 0600 traefik/acme.json

# 4. Start production services
docker compose -f docker-compose.prod.yml up -d
```

### Option B: Native Host Systemd Service for Backend

For lowest latency and direct Docker socket interaction, run PostgreSQL, Redis, BuildKit, and Traefik in Docker, while running the Go backend binary under systemd:

```bash
# 1. Build backend binary
cd /opt/api-sandbox-links/backend
go build -o /opt/api-sandbox-links/bin/server ./cmd/server

# 2. Install systemd unit
sudo cp systemd/api-sandbox-backend.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now api-sandbox-backend

# 3. Check logs
sudo journalctl -u api-sandbox-backend -f
```

---

## 6. Routine Maintenance & Automated Cron Jobs

Add the following cron entries (`sudo crontab -e`) on the production host:

```cron
# 1. Nightly database backup at 02:00 UTC (14-day retention)
0 2 * * * /opt/api-sandbox-links/scripts/backup_db.sh >> /var/log/api-sandbox-backup.log 2>&1

# 2. Daily Docker image & BuildKit cache pruning at 03:00 UTC
0 3 * * * /opt/api-sandbox-links/scripts/prune_disk.sh >> /var/log/api-sandbox-prune.log 2>&1

# 3. Hourly cleanup of orphaned sandbox networks
15 * * * * /opt/api-sandbox-links/scripts/cleanup_orphan_networks.sh >> /var/log/api-sandbox-networks.log 2>&1
```

---

## 7. Operational Playbooks & Troubleshooting

### Playbook 1: Host Reboot Recovery & Stuck Deployments

**Scenario:** The server rebooted or the backend was restarted while builds were in progress.

**Automatic Self-Healing:**
- On boot, the backend automatically calls `deployer.RecoverStuckBuilds(ctx)`.
- Any deployments left in `pending` or `building` state are reconciled to `failed` with log excerpt: `"build aborted: worker process restarted while build was in progress"`.
- Associated sandboxes in `queued` or `building` state are transitioned to `failed`.
- Every 2 minutes, `deployer.SweepTimedOutBuilds(ctx)` terminates any builds running longer than 20 minutes (`DeployTimeout`).

**Verification:**
```bash
# Check service status and recovery logs
sudo journalctl -u api-sandbox-backend -n 50 | grep "deployer"

# Verify readiness probe
curl -f http://127.0.0.1:8080/api/ready
```

---

### Playbook 2: Redis Restart & Idle Replay Verification

**Scenario:** Redis was restarted.

**State Durability:**
- Redis runs with `--appendonly yes --appendfsync everysec` backed by a persistent volume (`redis_data:/data`).
- All active idle markers (`lr:<sandbox_id>`) are restored from the AOF log upon restart.
- If Redis was down during traffic, containers do not hibernate immediately because the idle reaper checks `r.IsIdle(ctx, sandbox.ID)` on its next pass; any live HTTP request or WebSocket stream immediately executes `r.Touch()` to refresh the marker.

---

### Playbook 3: Gateway Wake Storm / Rate Limit 429 Responses

**Scenario:** A sudden spike of traffic arrives at a sleeping or active sandbox, or clients receive HTTP 429 Too Many Requests.

**Mechanism:**
- The edge gateway enforces a token-bucket rate limiter per client IP (default: 100 req/sec, burst 200).
- Multiple concurrent requests to a hibernated sandbox are deduplicated by a single-flight group (`wakeCall`), issuing exactly **one** `docker start` while other callers wait on the shared channel.
- If the wake fails, all waiting callers immediately receive a clean HTTP 503 instead of hanging.

**Tuning Rate Limits:**
Adjust in `.env`:
```ini
GATEWAY_RATE_LIMIT_RPS=250
GATEWAY_RATE_LIMIT_BURST=500
```
Then restart the backend:
```bash
sudo systemctl restart api-sandbox-backend
```

---

### Playbook 4: Build Failure / Cold-Start Port Discovery Timeout

**Scenario:** A custom application boots but Traefik or Gateway reports 503 Service Unavailable, or deploy fails with port discovery timeout.

**Diagnostic Steps:**
1. Check container logs directly:
   ```bash
   docker logs --tail 100 sandbox_<subdomain>
   ```
2. Verify container listening port:
   ```bash
   docker port sandbox_<subdomain>
   ```
3. Check `internal_port` recorded in PostgreSQL:
   ```sql
   SELECT id, subdomain, status, internal_port, build_config FROM sandboxes WHERE subdomain = '<subdomain>';
   ```
4. If the application requires a non-standard port not detected by Nixpacks/Railpack, inject `PORT=<desired_port>` into the sandbox environment variables via the UI or API.

---

### Playbook 5: Disk Space Full / Docker Cleanup

**Scenario:** Disk alert triggers or builds fail with `no space left on device`.

**Immediate Remediation:**
```bash
# Run the automated disk pruning script
sudo /opt/api-sandbox-links/scripts/prune_disk.sh

# If still low on space, clean up stopped BuildKit build containers
docker exec buildkit buildctl prune --all

# Check disk usage by directory
df -h
du -sh /var/lib/docker
du -sh /var/lib/api-sandbox/workdirs
```

---

### Playbook 6: Rotating Secrets Encryption Key

**Scenario:** Routine security rotation of `ENCRYPTION_KEY`.

**Execution:**
```bash
# 1. Generate new 32-byte key
NEW_KEY=$(openssl rand -hex 32)
OLD_KEY=$(grep "^ENCRYPTION_KEY=" /opt/api-sandbox-links/.env | cut -d= -f2)

# 2. Run the migration script (creates automatic pre-rotation backup)
cd /opt/api-sandbox-links
OLD_ENCRYPTION_KEY="${OLD_KEY}" NEW_ENCRYPTION_KEY="${NEW_KEY}" ./scripts/rotate_encryption_key.sh

# 3. Update .env
sed -i "s/^ENCRYPTION_KEY=.*/ENCRYPTION_KEY=${NEW_KEY}/" /opt/api-sandbox-links/.env

# 4. Restart backend
sudo systemctl restart api-sandbox-backend
```

---

## 8. Health Probes & Monitoring

API Sandbox Links provides two distinct HTTP health endpoints:

### Liveness Probe (`GET /api/health`)
- **Port:** `8080`
- **Response:** `200 OK` with JSON `{"status": "ok", "time": "..."}`
- Used for quick process liveness checks.

### Readiness Probe (`GET /api/ready`)
- **Port:** `8080`
- **Response:**
  - `200 OK`: All critical infrastructure dependencies (PostgreSQL ping, Redis ping, Docker engine ping) are active and accepting connections.
  - `503 Service Unavailable`: One or more components are failing, returning detailed dependency error breakdown in JSON.

```bash
# Production curl check
curl -i http://127.0.0.1:8080/api/ready
```
Example Output:
```json
{
  "status": "ready",
  "dependencies": {
    "database": "ok",
    "docker": "ok",
    "redis": "ok"
  },
  "time": "2026-11-12T11:50:00Z"
}
```
