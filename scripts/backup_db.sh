#!/usr/bin/env bash
# Backs up the PostgreSQL database into a compressed archive and prunes old backups.
# Recommended to run via cron daily (e.g. 02:00 UTC).

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/api-sandbox}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
DATE=$(date -u +'%Y%m%d_%H%M%S')
OUT_FILE="${BACKUP_DIR}/db_backup_${DATE}.sql.gz"

mkdir -p "${BACKUP_DIR}"

echo "==> [$(date -u +'%Y-%m-%dT%H:%M:%SZ')] Starting PostgreSQL backup..."

# Extract connection parameters from DATABASE_URL if available
if [[ -n "${DATABASE_URL:-}" ]]; then
    pg_dump "${DATABASE_URL}" | gzip -9 > "${OUT_FILE}"
elif docker ps --format '{{.Names}}' | grep -q "^postgres$"; then
    echo "--> Dumping from running postgres container..."
    docker exec -t postgres pg_dump -U "${POSTGRES_USER:-user}" "${POSTGRES_DB:-sandbox_platform}" | gzip -9 > "${OUT_FILE}"
else
    echo "ERROR: Neither DATABASE_URL nor running postgres container found." >&2
    exit 1
fi

# Validate backup integrity
if ! gzip -t "${OUT_FILE}"; then
    echo "ERROR: Backup integrity verification failed: ${OUT_FILE}" >&2
    rm -f "${OUT_FILE}"
    exit 1
fi

FILE_SIZE=$(du -h "${OUT_FILE}" | cut -f1)
echo "--> Backup created: ${OUT_FILE} (${FILE_SIZE})"

# Prune backups older than retention window
echo "--> Pruning backups older than ${RETENTION_DAYS} days..."
find "${BACKUP_DIR}" -name "db_backup_*.sql.gz" -type f -mtime +"${RETENTION_DAYS}" -delete || true

echo "==> [$(date -u +'%Y-%m-%dT%H:%M:%SZ')] Database backup completed successfully."

