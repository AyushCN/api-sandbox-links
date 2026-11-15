#!/usr/bin/env bash
# Install or upgrade API Sandbox Links on a single Linux host.
#
# Prerequisites (the script verifies them and refuses to guess):
#   docker + compose plugin, go, node 20+, npm, nixpacks
#   deploy/.env filled in from deploy/.env.production.example
#   wildcard DNS A/AAAA for *.SANDBOX_BASE_DOMAIN -> this host
#
# Run from the repository root:
#   sudo deploy/install.sh
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "run as root (sudo deploy/install.sh)" >&2
  exit 1
fi
if [[ ! -f deploy/.env ]]; then
  echo "missing deploy/.env; copy deploy/.env.production.example and fill it in" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1091
source deploy/.env
set +a

missing=()
for key in APP_DOMAIN SANDBOX_BASE_DOMAIN ACME_EMAIL DATABASE_URL REDIS_URL \
  SECRETS_ENCRYPTION_KEY GITHUB_CLIENT_ID GITHUB_CLIENT_SECRET NEXT_PUBLIC_API_BASE; do
  [[ -n "${!key:-}" ]] || missing+=("$key")
done
if ((${#missing[@]})); then
  echo "deploy/.env is missing required values: ${missing[*]}" >&2
  exit 1
fi

for cmd in docker go node npm nixpacks; do
  command -v "$cmd" >/dev/null || { echo "missing command: $cmd" >&2; exit 1; }
done
docker compose version >/dev/null

id sandbox >/dev/null 2>&1 || useradd --system --create-home --shell /usr/sbin/nologin sandbox
getent group docker >/dev/null && usermod -aG docker sandbox
install -d -m 0755 /opt/api-sandbox /etc/api-sandbox
install -d -o sandbox -g sandbox -m 0750 /var/lib/api-sandbox /var/lib/api-sandbox/work

echo "building backend"
go build -o /tmp/api-sandbox-server ./backend/cmd/server
install -m 0755 /tmp/api-sandbox-server /opt/api-sandbox/server
rm -f /tmp/api-sandbox-server

if [[ ! -f /etc/api-sandbox/backend.env ]]; then
  install -m 0600 -o root -g root deploy/.env /etc/api-sandbox/backend.env
  echo "installed /etc/api-sandbox/backend.env (0600)"
else
  echo "keeping existing /etc/api-sandbox/backend.env"
fi

install -m 0644 deploy/systemd/api-sandbox-backend.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now api-sandbox-backend.service

echo "starting data plane and edge"
docker compose --env-file deploy/.env -f deploy/docker-compose.prod.yml up -d --build

echo "waiting for backend readiness"
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8080/api/ready >/tmp/api-sandbox-ready.json 2>/dev/null; then
    break
  fi
  sleep 2
done
cat /tmp/api-sandbox-ready.json 2>/dev/null || true
echo
/opt/api-sandbox/server doctor || true

cat <<MSG

Next steps
  1. GitHub OAuth callback: ${GITHUB_OAUTH_CALLBACK_URL}
  2. DNS: ${APP_DOMAIN} and *.${SANDBOX_BASE_DOMAIN} must point at this host.
     Wildcard TLS uses DNS-01; Caddy needs CF_API_TOKEN (or another plugin).
  3. Firewall: expose only 22, 80 and 443. Never expose 8080, 8090, 5432 or 6379
     (see deploy/harden-firewall.sh).
  4. Logs: journalctl -u api-sandbox-backend -f
            docker compose --env-file deploy/.env -f deploy/docker-compose.prod.yml logs -f caddy
MSG
