#!/bin/sh

set -e

# Haloy Server Upgrade Script
# This script upgrades haloyadm to the latest version and restarts services.
# It uses the built-in 'haloyadm self-update' command to handle binary updates.

echo "Starting Haloy server upgrade..."

# --- Check haloyadm is available ---
if ! command -v haloyadm >/dev/null 2>&1; then
    echo "Error: haloyadm not found in PATH. Cannot upgrade." >&2
    exit 1
fi

# --- Show current version ---
CURRENT_VERSION=$(haloyadm --version 2>/dev/null | head -1 || echo "unknown")
echo "Current version: $CURRENT_VERSION"

# --- Rollback function ---
rollback() {
    echo ""
    echo "Upgrade failed, attempting rollback..."
    
    # Check if backup exists (created by haloyadm self-update)
    HALOYADM_PATH=$(command -v haloyadm)
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

# --- Update haloyadm binary ---
echo ""
echo "Updating haloyadm binary..."
if ! haloyadm self-update; then
    rollback
    exit 1
fi

# --- Restart services ---
# This will:
# - Stop and remove existing haloyd and HAProxy containers
# - Pull new Docker images (haloyd uses version from constants, HAProxy uses HAProxyVersion)
# - Start containers with the new images
echo ""
echo "Restarting Haloy services (this will pull new container images)..."
if ! haloyadm restart --no-logs; then
    rollback
    exit 1
fi

# --- Verify upgrade ---
echo ""
echo "Verifying upgrade..."
NEW_VERSION=$(haloyadm --version 2>/dev/null | head -1 || echo "unknown")
echo "haloyadm version: $NEW_VERSION"

echo ""
echo "Haloy server upgrade completed successfully!"
echo ""
echo "Next steps:"
echo "1. Verify the upgrade by checking the version: haloyadm --version"
echo "2. Check service status: docker ps"
echo "3. Review logs if needed: docker logs haloyd"
