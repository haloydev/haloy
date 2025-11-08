#!/usr/bin/env bash
# filepath: scripts/uninstall-server.sh

set -e

echo "Uninstalling Haloy server components..."
echo "⚠️  This will remove haloyd, HAProxy, and all associated configuration files."
echo ""

# Check if running as root for system-wide uninstall
if [ "$(id -u)" -ne 0 ]; then
    echo "Error: Server uninstallation requires root privileges." >&2
    echo "Run with sudo:" >&2
    echo "  sudo $0" >&2
    exit 1
fi

echo "Stopping services..."

# Stop services using haloyadm if available
if command -v haloyadm >/dev/null 2>&1; then
    echo "Stopping haloyd and HAProxy services..."
    haloyadm stop 2>/dev/null || echo "Failed to stop services with haloyadm (continuing anyway)"
else
    echo "haloyadm not found, stopping containers manually..."
    # Stop containers manually if haloyadm is not available
    docker stop haloy-haloyd 2>/dev/null || echo "haloy-haloyd container not running"
    docker stop haloy-haproxy 2>/dev/null || echo "haloy-haproxy container not running"
fi

echo "Removing containers and images..."

# Remove containers
docker rm -f haloy-haloyd 2>/dev/null || echo "haloy-haloyd container not found"
docker rm -f haloy-haproxy 2>/dev/null || echo "haloy-haproxy container not found"

# Remove Haloy images
docker image rm -f haloy-haloyd:latest 2>/dev/null || echo "haloy-haloyd image not found"

echo "Removing binaries..."

# Remove binaries
rm -f /usr/local/bin/haloyadm
rm -f "$HOME/.local/bin/haloyadm"

echo "Removing configuration and data..."

# Remove configuration directory
rm -rf /etc/haloy/

# Remove data directory
rm -rf /var/lib/haloy/

# Remove user-mode directories if they exist
rm -rf "$HOME/.config/haloy/"
rm -rf "$HOME/.local/share/haloy/"

echo ""
echo "✅ Haloy server components have been completely removed."
echo ""
echo "Removed components:"
echo "  - haloyd daemon container"
echo "  - HAProxy container"
echo "  - haloyadm admin tool"
echo "  - Configuration files"
echo "  - SSL certificates"
echo "  - Deployment data"
echo ""
echo "Note: Application containers deployed by Haloy are not automatically removed."
echo "To clean up application containers, run:"
echo "  docker ps -a --filter label=haloy.app --format 'table {{.ID}}\t{{.Names}}\t{{.Image}}'"
echo "  docker rm -f \$(docker ps -aq --filter label=haloy.app)"
