# Production Operations Runbook

This runbook covers operational deployment, infrastructure provisioning, disaster recovery, and troubleshooting procedures for running **API Sandbox Links** in production.

---

## 1. System Architecture & Port Allocations

API Sandbox Links operates as a coordinated single-host control plane and edge proxy managing isolated Docker containers for application sandboxes and database sidecars.

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

### Port Allocation Reference

| Service | Dev Port | Prod Port | Host Binding | Purpose |
|---|---|---|---|---|
| **Traefik HTTP** | `80` | `80` | `0.0.0.0` | HTTP traffic (redirects to HTTPS in production) |
| **Traefik HTTPS** | - | `443` | `0.0.0.0` | Wildcard TLS entrypoint (`*.sandbox.domain.com`) |
| **Traefik Dashboard** | `8081` | `8082` | `127.0.0.1` | Internal routing & metrics dashboard |
| **Frontend Console** | `3000` | `3000` | `127.0.0.1` | Next.js 16 Web IDE & management dashboard |
| **Backend API** | `8080` | `8080` | `127.0.0.1` | REST endpoints and WebSocket telemetry |
| **Sandbox Gateway** | `8090` | `8090` | `127.0.0.1` | Single-flight wake-on-request reverse proxy |
| **PostgreSQL** | `5434` | `5432` | `127.0.0.1` (or network) | Primary persistent data store |
| **Redis** | `6380` | `6379` | `127.0.0.1` (or network) | Ephemeral idle TTL markers & live stats cache |
| **BuildKit Daemon** | `1234` | `1234` | `127.0.0.1` (or network) | TCP build daemon for Railpack image builds |

---

## 2. Server Prerequisites & Base System Setup

### Recommended Server Specifications
- **Operating System**: Ubuntu 22.04 LTS, Ubuntu 24.04 LTS, or Debian 12.
- **CPU**: 4+ vCPUs.
- **Memory**: 8GB+ RAM (16GB+ recommended for concurrent Railpack builds).
- **Disk**: 100GB+ NVMe SSD (fast I/O for Docker layers and Git checkouts).

### Base System Packages & Docker Installation

```bash
sudo apt update && sudo apt install -y \
  ca-certificates curl gnupg lsb-release git jq gzip openssl

# Install Docker Engine & Compose plugin
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt update && sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
```

### Install Railpack (Default Builder Engine)

```bash
curl -sSL https://railpack.com/install.sh | sudo bash
railpack --version
```

---

## 3. DNS & Wildcard TLS Configuration (ACME DNS-01)

Traefik terminates TLS for all incoming sandbox traffic using Let's Encrypt Wildcard certificates via the DNS-01 challenge.

### DNS Records Setup
Create the following DNS records in your DNS provider (e.g. Cloudflare or AWS Route53):

| Type | Name | Target | Proxy Status |
|---|---|---|---|
| `A` | `console.yourdomain.com` | `<SERVER_PUBLIC_IP>` | DNS Only (Gray Cloud) |
| `A` | `api.yourdomain.com` | `<SERVER_PUBLIC_IP>` | DNS Only (Gray Cloud) |
| `A` | `*.sandbox.yourdomain.com` | `<SERVER_PUBLIC_IP>` | DNS Only (Gray Cloud) |

> [!IMPORTANT]
> The wildcard record `*.sandbox.yourdomain.com` **must be set to DNS Only** (unproxied in Cloudflare) so Let's Encrypt DNS challenges and direct TCP connections reach Traefik on ports 80 and 443.

### Cloudflare API Token Configuration
Set the following credentials in your production `.env`:
```ini
ACME_EMAIL=admin@yourdomain.com
ACME_DNS_PROVIDER=cloudflare
CF_DNS_API_TOKEN=your_cloudflare_api_token_here
```

---

## 4. Production Service Management (Systemd)

### Backend Service: `/etc/systemd/system/api-sandbox-backend.service`

```ini
[Unit]
Description=API Sandbox Links Backend Control Plane & Gateway
After=network.target docker.service

[Service]
Type=simple
User=sandboxuser
WorkingDirectory=/opt/api-sandbox-links
ExecStart=/opt/api-sandbox-links/backend/server
Restart=always
RestartSec=5s
LimitNOFILE=65536
EnvironmentFile=/opt/api-sandbox-links/.env

[Install]
WantedBy=multi-user.target
```

### Frontend Service: `/etc/systemd/system/api-sandbox-frontend.service`

```ini
[Unit]
Description=API Sandbox Links Next.js Console
After=network.target api-sandbox-backend.service

[Service]
Type=simple
User=sandboxuser
WorkingDirectory=/opt/api-sandbox-links/frontend
ExecStart=/usr/bin/npm start
Restart=always
RestartSec=5s
Environment=NODE_ENV=production
Environment=PORT=3000

[Install]
WantedBy=multi-user.target
```

---

## 5. Routine Operations & Disaster Recovery

### Health Check Verification
```bash
# Verify backend control plane
curl -s http://localhost:8080/api/health | jq .

# Verify Traefik reverse proxy
curl -I http://localhost:80
```

### Backup Procedures
1. **PostgreSQL Database**:
   ```bash
   pg_dump -U user -h localhost -p 5434 sandbox_platform | gzip > /opt/backups/db_$(date +%Y%m%d_%H%M%S).sql.gz
   ```
2. **Workspace Projects**:
   ```bash
   cp /tmp/api-sandbox-links/projects.json /opt/backups/projects_$(date +%Y%m%d_%H%M%S).json
   ```

### Pruning Zombie Containers and Volumes
If the host encounters disk pressure from expired deployments:
```bash
docker system prune -f --filter "label=sandbox_platform=true"
```
