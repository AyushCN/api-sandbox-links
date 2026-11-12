#!/usr/bin/env bash
# Prunes orphaned sandbox Docker bridge networks that have 0 connected containers.
# Recommended to run via cron hourly or daily.

set -euo pipefail

echo "==> [$(date -u +'%Y-%m-%dT%H:%M:%SZ')] Checking for orphaned sandbox networks..."

# Find all docker networks starting with sandbox_
networks=$(docker network ls --filter "name=^sandbox_" --format "{{.Name}}")

if [[ -z "${networks}" ]]; then
    echo "--> No sandbox networks found."
    exit 0
fi

reclaimed=0
for net in ${networks}; do
    # Count attached containers
    count=$(docker network inspect "${net}" --format '{{len .Containers}}' 2>/dev/null || echo "0")
    if [[ "${count}" -eq 0 ]]; then
        echo "--> Removing empty network: ${net}"
        if docker network rm "${net}" 2>/dev/null; then
            reclaimed=$((reclaimed + 1))
        fi
    fi
done

echo "==> [$(date -u +'%Y-%m-%dT%H:%M:%SZ')] Reclaimed ${reclaimed} orphan networks."
