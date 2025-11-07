#!/usr/bin/env bash

set -e

echo "Uninstalling Haloy admin tool (haloyadm)..."

# Detect installation location
if [ -f "/usr/local/bin/haloyadm" ]; then
    BINARY_PATH="/usr/local/bin/haloyadm"
elif [ -f "$HOME/.local/bin/haloyadm" ]; then
    BINARY_PATH="$HOME/.local/bin/haloyadm"
else
    echo "Haloy admin tool not found in standard locations."
    echo "If you installed to a custom directory, set the DIR environment variable:"
    echo "  DIR=/custom/path $0"
    exit 1
fi

# Check permissions for system installation
if [[ "$BINARY_PATH" == "/usr/local/bin/haloyadm" ]] && [ "$(id -u)" -ne 0 ]; then
    echo "Error: Uninstalling from $BINARY_PATH requires root privileges." >&2
    echo "Run with sudo:" >&2
    echo "  sudo $0" >&2
    exit 1
fi

# Remove the binary
rm -f "$BINARY_PATH"

echo "✅ Haloy admin tool (haloyadm) has been uninstalled successfully."
echo ""
echo "Note: This only removes the admin tool binary."
echo "Server daemon (haloyd) and configuration files are not affected."
echo "To remove all server components, run the server uninstall script."
