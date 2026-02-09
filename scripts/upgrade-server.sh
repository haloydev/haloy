#!/bin/sh

set -e

# Haloy Server Upgrade Script
# This script upgrades haloyd to the latest version and restarts the service.
# It uses the built-in 'haloyd upgrade' command to handle binary updates.

echo "Starting Haloy server upgrade..."

# --- Check haloyd is available ---
if ! command -v haloyd >/dev/null 2>&1; then
    echo "Error: haloyd not found in PATH. Cannot upgrade." >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
    echo "Error: systemctl is required but not found. This script only supports systemd." >&2
    exit 1
fi

# --- Show current version ---
CURRENT_VERSION=$(haloyd version 2>/dev/null | head -1 || echo "unknown")
echo "Current version: $CURRENT_VERSION"

# --- Rollback function ---
rollback() {
    echo ""
    echo "Upgrade failed, attempting rollback..."

    # Check if backup exists (created by haloyd upgrade)
    HALOYD_PATH=$(command -v haloyd)
    if [ -f "${HALOYD_PATH}.backup" ]; then
        cp "${HALOYD_PATH}.backup" "$HALOYD_PATH"
        echo "Restored haloyd from backup"
        echo "Restarting service with previous version..."
        systemctl restart haloyd || echo "Warning: Failed to restart service during rollback"
    else
        echo "Warning: No backup found, cannot rollback haloyd"
    fi
    echo "Rollback completed. Please check service status manually."
}

# --- Update haloyd binary ---
echo ""
echo "Updating haloyd binary..."
if ! haloyd upgrade; then
    rollback
    exit 1
fi

# --- Restart service ---
echo ""
echo "Restarting haloyd service..."
if ! systemctl restart haloyd; then
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

# --- Check service status ---
if systemctl is-active --quiet haloyd; then
    echo "Service status: running"
else
    echo "Warning: Service may not be running correctly"
    echo "Check status with: systemctl status haloyd"
fi

echo ""
echo "Haloy server upgrade completed successfully!"
echo ""
echo "Next steps:"
echo "1. Verify the upgrade by checking the version: haloyd version"
echo "2. Check service status: systemctl status haloyd"
echo "3. Review logs if needed: journalctl -u haloyd -f"
