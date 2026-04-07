#!/bin/bash
################################################################################
# Moxy Install / Update Script
#
# Supports two modes:
#   1. Quick install (default): downloads latest release from GitHub
#   2. Local install (--local): uses moxy-bin from current directory
#
# Usage:
#   # Quick install (downloads latest release)
#   curl -fsSL https://raw.githubusercontent.com/riakgu/moxy/main/scripts/install.sh | sudo bash
#
#   # Local install (after running build.sh)
#   sudo ./scripts/install.sh --local
#
# What it does:
#   1. Verify running as root
#   2. Install system dependency (adb)
#   3. Set resource limits (nofile, nproc)
#   4. Scaffold /opt/moxy/
#   5. Obtain binary (download from GitHub or use local)
#   6. Stop service if running, install binary
#   7. Install config.json (first install only)
#   8. Install systemd unit file
#   9. Install rsyslog ignore rule
#  10. Enable + (re)start service
################################################################################

set -euo pipefail

# ── Constants ─────────────────────────────────────────────────────────────────
GITHUB_REPO="riakgu/moxy"
INSTALL_DIR="/opt/moxy"
BINARY="${INSTALL_DIR}/moxy-bin"
CONFIG="${INSTALL_DIR}/config.json"
SERVICE_NAME="moxy"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
LOCAL_MODE=false

# ── Parse args ────────────────────────────────────────────────────────────────
for arg in "$@"; do
    case "$arg" in
        --local) LOCAL_MODE=true ;;
        *) echo "Unknown argument: $arg"; exit 1 ;;
    esac
done

# ── 1. Root check ─────────────────────────────────────────────────────────────
if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: This script must be run as root (sudo ./scripts/install.sh)" >&2
    exit 1
fi

echo "==> [1/10] Verified running as root"

# ── 2. System packages ────────────────────────────────────────────────────────
echo "==> [2/10] Installing system dependencies"
apt-get update -qq
apt-get install -y -qq adb curl

# ── 3. Resource limits ────────────────────────────────────────────────────────
echo "==> [3/10] Writing /etc/security/limits.d/99-moxy.conf"
cat > /etc/security/limits.d/99-moxy.conf << 'EOF'
# Moxy resource limits
# Allows Moxy to manage large numbers of sockets and network namespaces.
*    soft    nofile    unlimited
*    hard    nofile    unlimited
*    soft    nproc     65535
*    hard    nproc     65535
EOF

# ── 4. Directory scaffold ────────────────────────────────────────────────────
echo "==> [4/10] Scaffolding ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"

# ── 5. Obtain binary ─────────────────────────────────────────────────────────
if [[ "${LOCAL_MODE}" == true ]]; then
    # Local mode: use moxy-bin from current directory
    echo "==> [5/10] Using local binary"

    # Try current directory first, then script directory's parent (project root)
    if [[ -f "./moxy-bin" ]]; then
        LOCAL_BINARY="./moxy-bin"
    elif [[ -f "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/moxy-bin" ]]; then
        LOCAL_BINARY="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/moxy-bin"
    else
        echo "ERROR: moxy-bin not found. Run scripts/build.sh first." >&2
        exit 1
    fi

    echo "    Found: ${LOCAL_BINARY}"
    TMP_BINARY=$(mktemp /tmp/moxy.XXXXXX)
    cp "${LOCAL_BINARY}" "${TMP_BINARY}"
    TAG="local"
else
    # Default mode: download from GitHub Releases
    echo "==> [5/10] Fetching latest release from github.com/${GITHUB_REPO}"

    RELEASE_JSON=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest")
    TAG=$(echo "${RELEASE_JSON}" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

    if [[ -z "${TAG}" ]]; then
        echo "ERROR: Could not determine latest release tag from GitHub API" >&2
        exit 1
    fi

    echo "    Latest tag: ${TAG}"

    # Detect architecture
    ARCH=$(uname -m)
    case "$ARCH" in
        aarch64) ASSET_NAME="moxy-linux-arm64" ;;
        x86_64)  ASSET_NAME="moxy-linux-amd64" ;;
        *)
            echo "ERROR: Unsupported architecture: $ARCH" >&2
            echo "       Supported: aarch64 (arm64), x86_64 (amd64)" >&2
            exit 1
            ;;
    esac

    ASSET_URL=$(echo "${RELEASE_JSON}" | grep '"browser_download_url"' | grep "${ASSET_NAME}" | head -1 | sed 's/.*"browser_download_url": *"\([^"]*\)".*/\1/')

    if [[ -z "${ASSET_URL}" ]]; then
        echo "ERROR: Could not find '${ASSET_NAME}' asset in release ${TAG}" >&2
        exit 1
    fi

    echo "    Asset: ${ASSET_NAME}"
    echo "    URL: ${ASSET_URL}"

    # Download
    TMP_BINARY=$(mktemp /tmp/moxy.XXXXXX)
    curl -fsSL --output "${TMP_BINARY}" "${ASSET_URL}"

    # SHA-256 verification
    EXPECTED_SHA=$(echo "${RELEASE_JSON}" | grep -o 'SHA-256[^`"]*`[a-f0-9]\{64\}`\|SHA-256: *[a-f0-9]\{64\}' | grep -o '[a-f0-9]\{64\}' | head -1 || true)

    if [[ -n "${EXPECTED_SHA}" ]]; then
        echo "    Verifying SHA-256..."
        ACTUAL_SHA=$(sha256sum "${TMP_BINARY}" | awk '{print $1}')
        if [[ "${ACTUAL_SHA}" != "${EXPECTED_SHA}" ]]; then
            echo "ERROR: SHA-256 mismatch!" >&2
            echo "  Expected: ${EXPECTED_SHA}" >&2
            echo "  Actual:   ${ACTUAL_SHA}" >&2
            rm -f "${TMP_BINARY}"
            exit 1
        fi
        echo "    SHA-256 OK: ${ACTUAL_SHA}"
    else
        echo "    WARNING: No SHA-256 found in release body — skipping integrity check"
    fi
fi

# Ensure temp file is cleaned up on failure
trap 'rm -f "${TMP_BINARY}"' EXIT

# ── 6. Stop service, install binary ───────────────────────────────────────────
echo "==> [6/10] Installing binary to ${BINARY}"
if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
    echo "    Stopping ${SERVICE_NAME} service..."
    systemctl stop "${SERVICE_NAME}"
fi

chmod 755 "${TMP_BINARY}"
mv "${TMP_BINARY}" "${BINARY}"
trap - EXIT  # binary moved, no need to clean up

echo "    Installed: ${BINARY}"

# ── 7. Config file ────────────────────────────────────────────────────────────
echo "==> [7/10] Checking config.json"
if [[ -f "${CONFIG}" ]]; then
    echo "    Existing config.json preserved"
else
    if [[ "${LOCAL_MODE}" == true ]]; then
        # Look for config.json in current directory or project root
        if [[ -f "./config.json" ]]; then
            cp "./config.json" "${CONFIG}"
            echo "    Default config.json installed (from local)"
        elif [[ -f "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/config.json" ]]; then
            cp "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/config.json" "${CONFIG}"
            echo "    Default config.json installed (from project root)"
        else
            echo "    WARNING: No config.json found. Moxy will use defaults."
        fi
    else
        echo "    Downloading default config.json..."
        curl -fsSL -o "${CONFIG}" \
            "https://raw.githubusercontent.com/${GITHUB_REPO}/main/config.json"
        echo "    Default config.json installed"
    fi
fi

# ── 8. systemd service ────────────────────────────────────────────────────────
echo "==> [8/10] Installing systemd service"
cat > "${SERVICE_FILE}" << EOF
[Unit]
Description=Moxy Proxy Server
After=network-online.target
Wants=network-online.target

[Service]
SyslogIdentifier=moxy
StandardOutput=journal
StandardError=journal

User=root
WorkingDirectory=${INSTALL_DIR}
ExecStart=${BINARY}

Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"

# ── 9. rsyslog ignore rule ───────────────────────────────────────────────────
echo "==> [9/10] Installing rsyslog ignore rule"
cat > /etc/rsyslog.d/00-moxy-ignore.conf << 'EOF'
# Prevent Moxy stdout from double-logging to /var/log/syslog
if $programname == 'moxy' then stop
EOF

if systemctl is-active --quiet rsyslog 2>/dev/null; then
    systemctl restart rsyslog
fi

# ── 10. Start service ─────────────────────────────────────────────────────────
echo "==> [10/10] Starting ${SERVICE_NAME}..."
systemctl start "${SERVICE_NAME}"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Moxy ${TAG} installed successfully"
echo ""
echo "  Binary:  ${BINARY}"
echo "  Config:  ${CONFIG}"
echo ""
echo "  systemctl status moxy         — check status"
echo "  systemctl restart moxy        — restart"
echo "  journalctl -u moxy -f         — live logs"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
