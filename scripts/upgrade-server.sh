#!/bin/sh

set -e

# Haloy Server Upgrade Script
# Upgrades native Linux haloyd installs managed by systemd, OpenRC, or SysVinit.

GITHUB_REPO="${GITHUB_REPO:-haloydev/haloy}"
SERVICE_NAME="${HALOYD_SERVICE_NAME:-haloyd}"
SLEEP_SECONDS="${HALOY_UPGRADE_SLEEP_SECONDS:-3}"

TMP_FILE=""
BACKUP_FILE=""
SERVICE_STOPPED=false

error_exit() {
    echo "Error: $1" >&2
    exit 1
}

warn() {
    echo "Warning: $1" >&2
}

normalize_version() {
    echo "$1" | sed 's/^v//'
}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error_exit "This script must be run as root. Run it with sudo."
    fi
}

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        error_exit "'$1' is required but was not found."
    fi
}

detect_init_system() {
    if [ -n "${HALOY_UPGRADE_INIT_SYSTEM:-}" ]; then
        echo "$HALOY_UPGRADE_INIT_SYSTEM"
    elif [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
        echo "systemd"
    elif [ -f /sbin/openrc-run ] && command -v rc-service >/dev/null 2>&1; then
        echo "openrc"
    elif [ -f /etc/init.d/haloyd ]; then
        echo "sysvinit"
    else
        echo "unknown"
    fi
}

service_stop() {
    case "$INIT_SYSTEM" in
        systemd) systemctl stop "$SERVICE_NAME" ;;
        openrc) rc-service "$SERVICE_NAME" stop ;;
        sysvinit) /etc/init.d/haloyd stop ;;
        *) return 1 ;;
    esac
}

service_start() {
    case "$INIT_SYSTEM" in
        systemd) systemctl start "$SERVICE_NAME" ;;
        openrc) rc-service "$SERVICE_NAME" start ;;
        sysvinit) /etc/init.d/haloyd start ;;
        *) return 1 ;;
    esac
}

service_restart() {
    case "$INIT_SYSTEM" in
        systemd) systemctl restart "$SERVICE_NAME" ;;
        openrc) rc-service "$SERVICE_NAME" restart ;;
        sysvinit) /etc/init.d/haloyd restart ;;
        *) return 1 ;;
    esac
}

service_is_active() {
    case "$INIT_SYSTEM" in
        systemd) systemctl is-active --quiet "$SERVICE_NAME" ;;
        openrc) rc-service "$SERVICE_NAME" status >/dev/null 2>&1 ;;
        sysvinit) /etc/init.d/haloyd status >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

file_mode() {
    if stat -c '%a' "$1" >/dev/null 2>&1; then
        stat -c '%a' "$1"
    elif stat -f '%Lp' "$1" >/dev/null 2>&1; then
        stat -f '%Lp' "$1"
    else
        return 1
    fi
}

fetch_release_tag() {
    curl -fsSL -H "Accept: application/json" "$1" 2>/dev/null |
        sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
        head -n 1 || true
}

set_bind_capability() {
    if [ "$INIT_SYSTEM" = "systemd" ]; then
        return 0
    fi

    if ! command -v setcap >/dev/null 2>&1; then
        echo "setcap is required to grant CAP_NET_BIND_SERVICE on $INIT_SYSTEM installs." >&2
        return 1
    fi

    setcap cap_net_bind_service=+ep "$HALOYD_PATH"
}

rollback() {
    echo ""
    echo "Upgrade failed, attempting rollback..."

    if [ -n "$BACKUP_FILE" ] && [ -f "$BACKUP_FILE" ]; then
        if mv -f "$BACKUP_FILE" "$HALOYD_PATH"; then
            chmod "$CURRENT_MODE" "$HALOYD_PATH" 2>/dev/null || true
            set_bind_capability 2>/dev/null || warn "Failed to restore bind capability during rollback"
            echo "Restored haloyd from backup"
        else
            warn "Failed to restore haloyd from backup"
        fi
    else
        warn "No backup found, cannot restore haloyd binary"
    fi

    if [ "$SERVICE_STOPPED" = "true" ]; then
        service_restart || warn "Failed to restart service during rollback"
    fi

    echo "Rollback completed. Please check service status manually."
}

cleanup() {
    if [ -n "$TMP_FILE" ]; then
        rm -f "$TMP_FILE"
    fi
}

trap cleanup EXIT

echo "Starting Haloy server upgrade..."

require_root
require_command haloyd
require_command curl
require_command sed
require_command uname
require_command mktemp
require_command dirname
require_command stat

INIT_SYSTEM=$(detect_init_system)
case "$INIT_SYSTEM" in
    systemd | openrc | sysvinit) ;;
    *) error_exit "Could not detect a supported init system for haloyd." ;;
esac
echo "Detected init system: $INIT_SYSTEM"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux) ;;
    *) error_exit "Unsupported OS: $OS. haloyd server upgrades only run on Linux." ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64 | arm64) ARCH="arm64" ;;
    *) error_exit "Unsupported architecture: $ARCH" ;;
esac

BINARY_NAME="haloyd-${OS}-${ARCH}"
HALOYD_PATH=$(command -v haloyd)
HALOYD_DIR=$(dirname "$HALOYD_PATH")

if [ ! -f "$HALOYD_PATH" ]; then
    error_exit "haloyd path is not a regular file: $HALOYD_PATH"
fi

if [ ! -w "$HALOYD_DIR" ]; then
    error_exit "Install directory is not writable: $HALOYD_DIR"
fi

CURRENT_MODE=$(file_mode "$HALOYD_PATH") || error_exit "Failed to read permissions for $HALOYD_PATH"

CURRENT_VERSION=$(haloyd version 2>/dev/null | head -n 1 || echo "unknown")
echo "Current version: $CURRENT_VERSION"

echo "Checking for updates..."
LATEST_VERSION=$(fetch_release_tag "https://api.github.com/repos/${GITHUB_REPO}/releases/latest")

if [ -z "$LATEST_VERSION" ]; then
    echo "No stable release found, checking for prereleases..."
    LATEST_VERSION=$(fetch_release_tag "https://api.github.com/repos/${GITHUB_REPO}/releases")
fi

if [ -z "$LATEST_VERSION" ]; then
    error_exit "Could not determine latest version from GitHub."
fi

echo "Latest version: $LATEST_VERSION"

NORM_CURRENT=$(normalize_version "$CURRENT_VERSION")
NORM_LATEST=$(normalize_version "$LATEST_VERSION")

if [ "$NORM_CURRENT" = "$NORM_LATEST" ]; then
    echo "Already running the latest version!"
    exit 0
fi

DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/${BINARY_NAME}"
TMP_FILE=$(mktemp "${HALOYD_DIR}/.haloyd-upgrade.XXXXXX") || error_exit "Failed to create temp file in $HALOYD_DIR"
BACKUP_FILE="${HALOYD_PATH}.backup"

if [ -e "$BACKUP_FILE" ]; then
    error_exit "Backup file already exists: $BACKUP_FILE. Remove it after confirming no upgrade is in progress."
fi

echo "Downloading ${BINARY_NAME}..."
if ! curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL"; then
    error_exit "Failed to download from $DOWNLOAD_URL"
fi

chmod "$CURRENT_MODE" "$TMP_FILE" || error_exit "Failed to preserve binary permissions on downloaded file"

echo "Verifying download..."
DL_VERSION=$("$TMP_FILE" version 2>/dev/null | head -n 1 || true)
if [ -z "$DL_VERSION" ]; then
    error_exit "Downloaded binary failed verification."
fi

NORM_DL_VERSION=$(normalize_version "$DL_VERSION")
if [ "$NORM_DL_VERSION" != "$NORM_LATEST" ]; then
    error_exit "Downloaded binary version $DL_VERSION does not match release $LATEST_VERSION."
fi
echo "Downloaded version: $DL_VERSION"

echo ""
echo "Stopping haloyd service..."
if ! service_stop; then
    error_exit "Failed to stop haloyd service."
fi
SERVICE_STOPPED=true

echo "Backing up current binary to ${BACKUP_FILE}"
if ! cp -p "$HALOYD_PATH" "$BACKUP_FILE"; then
    rollback
    exit 1
fi

echo "Installing new binary..."
if ! mv -f "$TMP_FILE" "$HALOYD_PATH"; then
    rollback
    exit 1
fi
TMP_FILE=""

if ! chmod "$CURRENT_MODE" "$HALOYD_PATH"; then
    rollback
    exit 1
fi

if ! set_bind_capability; then
    rollback
    exit 1
fi

echo ""
echo "Starting haloyd service..."
if ! service_start; then
    rollback
    exit 1
fi

if [ "$SLEEP_SECONDS" != "0" ]; then
    echo "Waiting for service to start..."
    sleep "$SLEEP_SECONDS"
fi

echo ""
echo "Verifying upgrade..."
NEW_VERSION=$(haloyd version 2>/dev/null | head -n 1 || echo "unknown")
echo "haloyd version: $NEW_VERSION"

NORM_NEW_VERSION=$(normalize_version "$NEW_VERSION")
if [ "$NORM_NEW_VERSION" != "$NORM_LATEST" ]; then
    rollback
    exit 1
fi

if service_is_active; then
    echo "Service status: running"
else
    rollback
    exit 1
fi

rm -f "$BACKUP_FILE"

echo ""
echo "Haloy server upgrade completed successfully!"
