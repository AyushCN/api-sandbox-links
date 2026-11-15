# API Sandbox Links: Run Guide

## Prerequisites
```bash
docker --version       # Docker 29.1.3+
docker compose version # Compose 2.40.3+
go version             # go1.26.0+
node --version && npm --version # Node 24.15.0 / npm 12.0.2
git --version
nixpacks --version     # nixpacks 1.41.0
```

## 0. Environment
Copy the root `.env.example` (gitignored) and edit:

```bash
cp backend/.env.example .env
# Edit .env - never commit it
```

Start the backend from repo root with dev auth:

```bash
GITHUB_CLIENT_ID= GITHUB_CLIENT_SECRET= ./backend/server
```

## 1. Infrastructure
```bash
docker compose -f docker-compose.dev.yml up -d
# Verify: sandbox-platform-db, sandbox-platform-redis
```

## 2. Backend
```bash
cd backend && go build ./... && go build -o server ./cmd/server
cd ..

# Dev-auth run
GITHUB_CLIENT_ID= GITHUB_CLIENT_SECRET= ./backend/server

# Health check
curl -s localhost:8080/api/health
```

## 3. Frontend
```bash
cd frontend && npm install && npm run dev
# Open http://localhost:3000
```

## 4. Quick E2E
```bash
# Create sandbox (dev auth)
curl -s -X POST localhost:8080/api/sandboxes/ \
  -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/heroku/node-js-getting-started","lifetime_seconds":3600,"idle_timeout_seconds":900,"database":"none"}'

# Open public URL via Traefik on :80
# Browser: http://<subdomain>.sandbox.localhost
```

## 5. Stop
```bash
# Destroy sandboxes
curl -s -X POST localhost:8080/api/sandboxes/<id>/actions/destroy

# Stop infrastructure (preserves DB volume)
docker compose -f docker-compose.dev.yml stop

# Remove local binary
rm -f backend/server
```