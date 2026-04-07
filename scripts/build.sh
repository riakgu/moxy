#!/bin/bash
################################################################################
# Moxy Build Script
#
# Cross-compiles Moxy for Linux. Runs on the dev machine (or any machine
# with Go + Node.js installed).
#
# Usage:
#   ./scripts/build.sh              # defaults to arm64
#   ./scripts/build.sh amd64        # for x86_64 servers
#
# Output: moxy-bin in the project root
################################################################################

set -euo pipefail

ARCH="${1:-arm64}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ── 1. Check prerequisites ───────────────────────────────────────────────────
echo "==> [1/3] Checking prerequisites"

if ! command -v go &>/dev/null; then
    echo "ERROR: Go not found. Install from https://go.dev/dl/" >&2
    exit 1
fi
echo "    Go: $(go version)"

if ! command -v node &>/dev/null; then
    echo "ERROR: Node.js not found. Install from https://nodejs.org/" >&2
    exit 1
fi
echo "    Node: $(node --version)"

if ! command -v npm &>/dev/null; then
    echo "ERROR: npm not found. Install Node.js from https://nodejs.org/" >&2
    exit 1
fi
echo "    npm: $(npm --version)"

# ── 2. Build React dashboard ─────────────────────────────────────────────────
echo "==> [2/3] Building dashboard"
cd "${PROJECT_ROOT}/web/dashboard"
npm ci --silent
npm run build

# ── 3. Cross-compile Go binary ───────────────────────────────────────────────
echo "==> [3/3] Compiling moxy-bin (linux/${ARCH})"
cd "${PROJECT_ROOT}"
CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build -o moxy-bin ./cmd/web/

# ── Done ──────────────────────────────────────────────────────────────────────
SIZE=$(ls -lh moxy-bin | awk '{print $5}')
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Build complete"
echo ""
echo "  Binary:  moxy-bin"
echo "  Target:  linux/${ARCH}"
echo "  Size:    ${SIZE}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
