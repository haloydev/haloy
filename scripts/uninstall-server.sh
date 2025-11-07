#!/usr/bin/env bash

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

# Stop haloyd service if running
if systemctl is-active --quiet haloyd 2>/dev/null; then
    echo "Stopping haloyd service..."
    systemctl stop haloyd
    systemctl disable haloyd 2>/dev/null || true
fi

# Stop haproxy service if running (managed by haloyd)
if systemctl is-active --quiet haproxy 2>/dev/null; then
    echo "Stopping HAProxy service..."
    systemctl stop haproxy
    systemctl disable haproxy 2>/dev/null || true
fi

echo "Removing binaries..."

# Remove binaries
rm -f /usr/local/bin/haloyd
rm -f /usr/local/bin/haloyadm

echo "Removing configuration and data..."

# Remove configuration directory
rm -rf /etc/haloy/

# Remove data directory
rm -rf /var/lib/haloy/

# Remove systemd service files
rm -f /etc/systemd/system/haloyd.service
rm -f /etc/systemd/system/haproxy.service

# Reload systemd
systemctl daemon-reload 2>/dev/null || true

echo ""
echo "✅ Haloy server components have been completely removed."
echo ""
echo "Removed components:"
echo "  - haloyd daemon"
echo "  - haloyadm admin tool"
echo "  - HAProxy configuration"
echo "  - SSL certificates"
echo "  - Deployment data"
echo "  - Systemd service files"
echo ""
echo "Note: Docker containers deployed by Haloy are not automatically removed."
echo "To clean up containers, run: docker ps -a | grep haloy | awk '{print \$1}' | xargs docker rm -f"
