#!/usr/bin/env bash
# Minimal host firewall for the single-host deployment. Run as root.
#
# The backend API binds 127.0.0.1:8080 (safe), but the sandbox gateway listens
# on all interfaces at :8090 because Traefik/Caddy must reach it. Blocking 8090
# at the host firewall is what prevents sandbox traffic from bypassing TLS.
set -euo pipefail
[[ ${EUID} -eq 0 ]] || { echo "run as root" >&2; exit 1; }
command -v ufw >/dev/null || { echo "ufw is not installed" >&2; exit 1; }
if [[ ${FORCE:-0} != 1 ]]; then
  read -r -p "Configure ufw to allow only 22/80/443 and enable it? [y/N] " answer
  [[ $answer == y || $answer == Y ]] || exit 0
fi
ufw default deny incoming
ufw default allow outgoing
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw deny 8080/tcp
ufw deny 8090/tcp
ufw deny 5432/tcp
ufw deny 6379/tcp
ufw --force enable
ufw status verbose
