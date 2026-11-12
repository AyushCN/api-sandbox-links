#!/usr/bin/env bash
# Runs the automated end-to-end smoke test suite against live Docker, PostgreSQL,
# and Redis for both Nixpacks and Railpack builders.
#
# Usage:
#   ./scripts/run_smoke_verification.sh [nixpacks|railpack|all]

set -euo pipefail

TARGET_BUILDER="${1:-all}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ANSI colors
GREEN="\033[0;32m"
RED="\033[0;31m"
YELLOW="\033[1;33m"
CYAN="\033[0;36m"
NC="\033[0m"

echo -e "${CYAN}================================================================${NC}"
echo -e "${CYAN}      API Sandbox Links — Live Smoke Verification Suite         ${NC}"
echo -e "${CYAN}================================================================${NC}"

# Pre-flight check 1: Docker
echo -n "Checking Docker daemon... "
if docker info >/dev/null 2>&1; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAILED${NC}"
    echo "ERROR: Docker daemon is not accessible. Please start Docker (sudo systemctl start docker)." >&2
    exit 1
fi

# Pre-flight check 2: Compose dependencies
echo -n "Checking infrastructure containers (postgres, redis)... "
running_containers=$(docker ps --format "{{.Names}}")
if echo "${running_containers}" | grep -q "postgres" && echo "${running_containers}" | grep -q "redis"; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${YELLOW}NOT RUNNING${NC}"
    echo "--> Bringing up dev dependencies via docker-compose.dev.yml..."
    (cd "${REPO_ROOT}" && docker compose -f docker-compose.dev.yml up -d postgres redis buildkit)
    echo "--> Waiting 5s for database readiness..."
    sleep 5
fi

run_test_for_builder() {
    local b="$1"
    echo ""
    echo -e "${CYAN}----------------------------------------------------------------${NC}"
    echo -e "${CYAN}>>> Executing Smoke Test for BUILDER=${b}${NC}"
    echo -e "${CYAN}----------------------------------------------------------------${NC}"

    # Check CLI availability
    if ! command -v "${b}" >/dev/null 2>&1; then
        echo -e "${YELLOW}WARNING: CLI binary '${b}' not found in PATH.${NC}"
        echo "Install via:"
        if [[ "${b}" == "nixpacks" ]]; then
            echo "  curl -sSL https://nixpacks.com/install.sh | sudo bash"
        else
            echo "  curl -sSL https://railpack.com/install.sh | sudo bash"
        fi
        echo "Skipping ${b} smoke test."
        return 0
    fi

    echo "Running go test with -tags=smoke..."
    START_TIME=$(date +%s)
    
    if (cd "${REPO_ROOT}/backend" && ASL_SMOKE_BUILDER="${b}" go test -v -tags=smoke -run TestSmokeFullLifecycle ./test/e2e -timeout 10m); then
        DURATION=$(( $(date +%s) - START_TIME ))
        echo -e "${GREEN}✓ Smoke test passed for ${b} in ${DURATION}s.${NC}"
    else
        echo -e "${RED}✗ Smoke test failed for ${b}.${NC}"
        return 1
    fi
}

case "${TARGET_BUILDER}" in
    nixpacks)
        run_test_for_builder "nixpacks"
        ;;
    railpack)
        run_test_for_builder "railpack"
        ;;
    all)
        run_test_for_builder "nixpacks"
        run_test_for_builder "railpack"
        ;;
    *)
        echo "Unknown target builder: ${TARGET_BUILDER}. Expected 'nixpacks', 'railpack', or 'all'."
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}================================================================${NC}"
echo -e "${GREEN}      Live Smoke Verification Suite Completed!                  ${NC}"
echo -e "${GREEN}================================================================${NC}"
