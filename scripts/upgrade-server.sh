#!/bin/sh

set -e

# Haloy Server Upgrade Script
# This script upgrades haloyadm to the latest version and restarts services
# The haloyd daemon runs in a Docker container, so haloyadm restart handles
# pulling the new image and recreating the container.

echo "Starting Haloy server upgrade..."

# --- Auto-detect OS and Architecture ---
OS=$(uname -s)
case "$OS" in
    Linux*)   PLATFORM="linux" ;;
    Darwin*)  PLATFORM="darwin" ;;
    *)        echo "Error: Unsupported OS '$OS'. Haloy supports Linux and macOS." >&2; exit 1 ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64)   ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)        echo "Error: Unsupported architecture '$ARCH'. Haloy supports amd64 (x86_64) and arm64." >&2; exit 1 ;;
esac

echo "Detected platform: ${PLATFORM}/${ARCH}"

# --- Store current haloyadm path for backup/rollback ---
HALOYADM_PATH=""
if command -v haloyadm >/dev/null 2>&1; then
    HALOYADM_PATH=$(command -v haloyadm)
else
    echo "Error: haloyadm not found in PATH. Cannot upgrade." >&2
    exit 1
fi

# --- Rollback function ---
rollback() {
    echo ""
    echo "Upgrade failed, attempting rollback..."
    if [ -f "${HALOYADM_PATH}.backup" ]; then
        cp "${HALOYADM_PATH}.backup" "$HALOYADM_PATH"
        echo "Restored haloyadm from backup"
        echo "Restarting services with previous version..."
        haloyadm restart --no-logs || echo "Warning: Failed to restart services during rollback"
    else
        echo "Warning: No backup found, cannot rollback haloyadm"
    fi
    echo "Rollback completed. Please check service status manually."
}

# Set trap for rollback on error
trap rollback ERR

# --- Fetch the latest version from GitHub ---
echo "Finding the latest version of Haloy..."
GITHUB_LATEST_VERSION=$(curl -sL -H 'Accept: application/json' "https://api.github.com/repos/haloydev/haloy/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' || echo "")

if [ -z "$GITHUB_LATEST_VERSION" ]; then
    echo "Error: Could not determine the latest Haloy version from GitHub." >&2
    exit 1
fi

echo "Latest version: $GITHUB_LATEST_VERSION"

# --- Check current version ---
CURRENT_VERSION=$(haloyadm --version 2>/dev/null | head -1 || echo "unknown")
echo "Current version: $CURRENT_VERSION"

if [ "$CURRENT_VERSION" = "$GITHUB_LATEST_VERSION" ]; then
    echo "Already running the latest version. No upgrade needed."
    exit 0
fi

# --- Download haloyadm ---
echo ""
echo "Downloading haloyadm ${GITHUB_LATEST_VERSION}..."
HALOYADM_BINARY_NAME="haloyadm-${PLATFORM}-${ARCH}"
HALOYADM_DOWNLOAD_URL="https://github.com/haloydev/haloy/releases/download/${GITHUB_LATEST_VERSION}/${HALOYADM_BINARY_NAME}"
HALOYADM_TMP="/tmp/haloyadm-upgrade"

curl -L -o "$HALOYADM_TMP" "$HALOYADM_DOWNLOAD_URL" 2>/dev/null || {
    echo "Error: Failed to download haloyadm from $HALOYADM_DOWNLOAD_URL" >&2
    exit 1
}
chmod +x "$HALOYADM_TMP"

# --- Backup and install haloyadm ---
echo "Backing up existing haloyadm from $HALOYADM_PATH..."
cp "$HALOYADM_PATH" "${HALOYADM_PATH}.backup"

echo "Installing new haloyadm..."
cp "$HALOYADM_TMP" "$HALOYADM_PATH"
rm -f "$HALOYADM_TMP"

echo "haloyadm updated successfully"

# --- Restart services using haloyadm ---
# This will:
# - Stop and remove existing haloyd and HAProxy containers
# - Pull new Docker images (haloyd uses version from constants, HAProxy uses HAProxyVersion)
# - Start containers with the new images
echo ""
echo "Restarting Haloy services (this will pull new container images)..."
haloyadm restart --no-logs

# --- Verify upgrade ---
echo ""
echo "Verifying upgrade..."
NEW_VERSION=$(haloyadm --version 2>/dev/null | head -1 || echo "unknown")
echo "haloyadm version: $NEW_VERSION"

if [ "$NEW_VERSION" != "$GITHUB_LATEST_VERSION" ]; then
    echo "Warning: Version mismatch after upgrade. Expected $GITHUB_LATEST_VERSION, got $NEW_VERSION"
fi

# --- Cleanup backup on success ---
# Disable ERR trap since we succeeded
trap - ERR
rm -f "${HALOYADM_PATH}.backup"

echo ""
echo "Haloy server upgrade to $GITHUB_LATEST_VERSION completed successfully!"
echo ""
echo "Next steps:"
echo "1. Verify the upgrade by checking the version: haloyadm --version"
echo "2. Check service status: docker ps"
echo "3. Review logs if needed: docker logs haloyd"
