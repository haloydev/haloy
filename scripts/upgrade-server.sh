#!/bin/sh

set -e

# Haloy Server Upgrade Script
# This script upgrades haloyd to the latest version and restarts the service.
# It handles the binary replacement directly to support upgrading from any version,
# including versions with the copyFile bus error bug.
#
# Supports systemd, OpenRC, and SysVinit.

GITHUB_REPO="haloydev/haloy"

echo "Starting Haloy server upgrade..."

# --- Check dependencies ---
if ! command -v haloyd >/dev/null 2>&1; then
    echo "Error: haloyd not found in PATH. Cannot upgrade." >&2
    exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required but not found." >&2
    exit 1
fi

# --- Detect init system ---
detect_init_system() {
    if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
        echo "systemd"
    elif [ -f /sbin/openrc-run ] && command -v rc-service >/dev/null 2>&1; then
        echo "openrc"
    elif [ -f /etc/init.d/haloyd ]; then
        echo "sysvinit"
    else
        echo "unknown"
    fi
}

INIT_SYSTEM=$(detect_init_system)
echo "Detected init system: $INIT_SYSTEM"

service_stop() {
    case "$INIT_SYSTEM" in
        systemd)  systemctl stop haloyd || true ;;
        openrc)   rc-service haloyd stop || true ;;
        sysvinit) /etc/init.d/haloyd stop || true ;;
        *)        echo "Warning: Unknown init system, cannot stop service" ;;
    esac
}

service_start() {
    case "$INIT_SYSTEM" in
        systemd)  systemctl start haloyd ;;
        openrc)   rc-service haloyd start ;;
        sysvinit) /etc/init.d/haloyd start ;;
        *)        echo "Warning: Unknown init system, start haloyd manually with: haloyd serve"; return 1 ;;
    esac
}

service_restart() {
    case "$INIT_SYSTEM" in
        systemd)  systemctl restart haloyd || echo "Warning: Failed to restart service during rollback" ;;
        openrc)   rc-service haloyd restart || echo "Warning: Failed to restart service during rollback" ;;
        sysvinit) /etc/init.d/haloyd restart || echo "Warning: Failed to restart service during rollback" ;;
        *)        echo "Warning: Unknown init system, restart haloyd manually" ;;
    esac
}

service_is_active() {
    case "$INIT_SYSTEM" in
        systemd)  systemctl is-active --quiet haloyd ;;
        openrc)   rc-service haloyd status >/dev/null 2>&1 ;;
        sysvinit) /etc/init.d/haloyd status >/dev/null 2>&1 ;;
        *)        return 1 ;;
    esac
}

# --- Detect platform ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Error: Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

BINARY_NAME="haloyd-${OS}-${ARCH}"
HALOYD_PATH=$(command -v haloyd)

# --- Show current version ---
CURRENT_VERSION=$(haloyd version 2>/dev/null | head -1 || echo "unknown")
echo "Current version: $CURRENT_VERSION"

# --- Fetch latest version from GitHub ---
echo "Checking for updates..."

LATEST_VERSION=$(curl -sS "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    -H "Accept: application/json" 2>/dev/null | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -z "$LATEST_VERSION" ]; then
    echo "No stable release found, checking for prereleases..."
    LATEST_VERSION=$(curl -sS "https://api.github.com/repos/${GITHUB_REPO}/releases" \
        -H "Accept: application/json" 2>/dev/null | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)
fi

if [ -z "$LATEST_VERSION" ]; then
    echo "Error: Could not determine latest version from GitHub." >&2
    exit 1
fi

echo "Latest version: $LATEST_VERSION"

# Normalize versions for comparison (strip 'v' prefix)
NORM_CURRENT=$(echo "$CURRENT_VERSION" | sed 's/^v//')
NORM_LATEST=$(echo "$LATEST_VERSION" | sed 's/^v//')

if [ "$NORM_CURRENT" = "$NORM_LATEST" ]; then
    echo "Already running the latest version!"
    exit 0
fi

# --- Download new binary to temp file ---
DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/${BINARY_NAME}"
TMP_FILE=$(mktemp /tmp/haloyd-upgrade.XXXXXX)

cleanup() {
    rm -f "$TMP_FILE"
}
trap cleanup EXIT

echo "Downloading ${BINARY_NAME}..."
if ! curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL"; then
    echo "Error: Failed to download from $DOWNLOAD_URL" >&2
    exit 1
fi

chmod +x "$TMP_FILE"

# --- Verify downloaded binary ---
echo "Verifying download..."
DL_VERSION=$("$TMP_FILE" version 2>/dev/null | head -1 || true)
if [ -z "$DL_VERSION" ]; then
    echo "Error: Downloaded binary failed verification." >&2
    exit 1
fi
echo "Downloaded version: $DL_VERSION"

# --- Rollback function ---
rollback() {
    echo ""
    echo "Upgrade failed, attempting rollback..."
    if [ -f "${HALOYD_PATH}.backup" ]; then
        mv "${HALOYD_PATH}.backup" "$HALOYD_PATH"
        echo "Restored haloyd from backup"
        service_restart
    else
        echo "Warning: No backup found, cannot rollback haloyd"
    fi
    echo "Rollback completed. Please check service status manually."
}

# --- Stop service before replacing binary ---
echo ""
echo "Stopping haloyd service..."
service_stop

# --- Backup and install via atomic rename ---
echo "Backing up current binary to ${HALOYD_PATH}.backup"
cp "$HALOYD_PATH" "${HALOYD_PATH}.backup"

echo "Installing new binary..."
if ! mv "$TMP_FILE" "$HALOYD_PATH"; then
    rollback
    exit 1
fi

# --- Start service ---
echo ""
echo "Starting haloyd service..."
if ! service_start; then
    rollback
    exit 1
fi

# --- Wait for service to start ---
echo "Waiting for service to start..."
sleep 3

# --- Verify upgrade ---
echo ""
echo "Verifying upgrade..."
NEW_VERSION=$(haloyd version 2>/dev/null | head -1 || echo "unknown")
echo "haloyd version: $NEW_VERSION"

if service_is_active; then
    echo "Service status: running"
else
    echo "Warning: Service may not be running correctly"
    case "$INIT_SYSTEM" in
        systemd)  echo "Check status with: systemctl status haloyd" ;;
        openrc)   echo "Check status with: rc-service haloyd status" ;;
        sysvinit) echo "Check status with: /etc/init.d/haloyd status" ;;
    esac
fi

# --- Clean up backup on success ---
rm -f "${HALOYD_PATH}.backup"

echo ""
echo "Haloy server upgrade completed successfully!"
