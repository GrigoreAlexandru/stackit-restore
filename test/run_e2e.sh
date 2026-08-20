#!/usr/bin/env bash
set -euo pipefail

# Discover repository root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

echo "========================================================="
echo " Building stackit-restore binary for E2E testing"
echo "========================================================="
mkdir -p /tmp/stackit-restore-e2e-bin
if ! go build -o /tmp/stackit-restore-e2e-bin/stackit-restore ./cmd/stackit-restore; then
    echo "Warning: go build failed. Checking for existing binary fallback..."
    if [ -f "${REPO_ROOT}/stackit-restore" ]; then
        echo "Using existing pre-built binary ${REPO_ROOT}/stackit-restore"
        cp "${REPO_ROOT}/stackit-restore" /tmp/stackit-restore-e2e-bin/stackit-restore
    else
        echo "Error: No pre-built binary found at ${REPO_ROOT}/stackit-restore"
        exit 1
    fi
fi

echo ""
echo "========================================================="
echo " Running stackit-restore E2E Test Suite"
echo "========================================================="
go test -v ./test/e2e/... "$@"
