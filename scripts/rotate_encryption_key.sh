#!/usr/bin/env bash
# Procedure to safely rotate the ENCRYPTION_KEY used for database secrets.
#
# Steps:
#   1. Generate new 32-byte key: openssl rand -hex 32
#   2. Run this script: OLD_ENCRYPTION_KEY=<old> NEW_ENCRYPTION_KEY=<new> ./scripts/rotate_encryption_key.sh
#   3. Update ENCRYPTION_KEY=<new> in your .env or systemd EnvironmentFile
#   4. Restart the backend service

set -euo pipefail

if [[ -z "${OLD_ENCRYPTION_KEY:-}" ]] || [[ -z "${NEW_ENCRYPTION_KEY:-}" ]]; then
    echo "ERROR: OLD_ENCRYPTION_KEY and NEW_ENCRYPTION_KEY environment variables are required." >&2
    echo "Example: OLD_ENCRYPTION_KEY=\$(grep ENCRYPTION_KEY .env | cut -d= -f2) NEW_ENCRYPTION_KEY=\$(openssl rand -hex 32) ./scripts/rotate_encryption_key.sh" >&2
    exit 1
fi

if [[ -f .env ]]; then
    # shellcheck disable=SC1091
    source .env
fi

if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "ERROR: DATABASE_URL is not set." >&2
    exit 1
fi

echo "==> Creating safety backup before rotating keys..."
"$(dirname "$0")/backup_db.sh"

echo "==> Re-encrypting secrets in PostgreSQL..."
cd "$(dirname "$0")/../backend"
go run ./cmd/rekey

echo ""
echo "=================================================================="
echo "KEY ROTATION COMPLETE"
echo "Remember to update your .env file with:"
echo "ENCRYPTION_KEY=${NEW_ENCRYPTION_KEY}"
echo "Then restart the backend service: sudo systemctl restart api-sandbox-backend"
echo "=================================================================="

