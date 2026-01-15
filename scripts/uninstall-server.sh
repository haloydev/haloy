#!/bin/sh

set -e

echo "Uninstalling Haloy server components..."
echo "This will remove haloyd and all associated configuration files."
echo ""

# Check if running as root for system-wide uninstall
if [ "$(id -u)" -ne 0 ]; then
    echo "Error: Server uninstallation requires root privileges." >&2
    echo "Run with sudo:" >&2
    echo "  sudo $0" >&2
    exit 1
fi

echo "Stopping haloyd service..."

# Stop and disable systemd service if it exists
if systemctl is-active --quiet haloyd 2>/dev/null; then
    systemctl stop haloyd
    echo "Stopped haloyd service"
fi

if systemctl is-enabled --quiet haloyd 2>/dev/null; then
    systemctl disable haloyd
    echo "Disabled haloyd service"
fi

# Remove systemd service file
if [ -f "/etc/systemd/system/haloyd.service" ]; then
    rm -f /etc/systemd/system/haloyd.service
    systemctl daemon-reload
    echo "Removed systemd service file"
fi

echo "Removing binaries..."

# Remove binaries
rm -f /usr/local/bin/haloyd
rm -f "$HOME/.local/bin/haloyd"

echo "Removing configuration and data..."

# Remove configuration directory
rm -rf /etc/haloy/

# Remove data directory
rm -rf /var/lib/haloy/

# Remove user-mode directories if they exist
rm -rf "$HOME/.config/haloy/"
rm -rf "$HOME/.local/share/haloy/"

echo ""
echo "Haloy server components have been completely removed."
echo ""
echo "Removed components:"
echo "  - haloyd daemon binary"
echo "  - systemd service"
echo "  - Configuration files"
echo "  - SSL certificates"
echo "  - Deployment data"
echo ""
echo "Note: Application containers deployed by Haloy are not automatically removed."
echo "To clean up application containers, run:"
echo "  docker ps -a --filter label=dev.haloy.role=app --format 'table {{.ID}}\t{{.Names}}\t{{.Image}}'"
echo "  docker rm -f \$(docker ps -aq --filter label=dev.haloy.role=app)"
