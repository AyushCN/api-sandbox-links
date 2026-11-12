#!/usr/bin/env bash
# Prunes unused Docker objects, BuildKit caches, and abandoned workdirs.
# Recommended to run via cron daily (e.g. 03:00 UTC).

set -euo pipefail

echo "==> [$(date -u +'%Y-%m-%dT%H:%M:%SZ')] Starting api-sandbox-links disk prune..."

# 1. Prune stopped Docker containers older than 24h
echo "--> Pruning stopped containers..."
docker container prune -f --filter "until=24h" || true

# 2. Prune dangling and unused Docker images older than 48h
echo "--> Pruning unused images..."
docker image prune -a -f --filter "until=48h" || true

# 3. Prune dangling build cache
echo "--> Pruning Docker builder cache..."
docker builder prune -f --filter "until=48h" || true

# 4. Prune BuildKit container cache if running
if docker ps --format '{{.Names}}' | grep -q "^buildkit$"; then
    echo "--> Pruning BuildKit container cache..."
    docker exec buildkit buildctl prune --keep-duration 72h || true
fi

# 5. Prune temporary files in /tmp/api-sandbox* older than 24h
echo "--> Cleaning up transient scratch files..."
find /tmp -maxdepth 1 -name "api-sandbox*" -mtime +1 -exec rm -rf {} + 2>/dev/null || true

echo "==> [$(date -u +'%Y-%m-%dT%H:%M:%SZ')] Disk prune completed successfully."

