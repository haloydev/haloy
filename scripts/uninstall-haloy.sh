#!/usr/bin/env bash

set -e

echo "Uninstalling Haloy client..."

# Default installation directory
DEFAULT_DIR="$HOME/.local/bin"
DIR="${DIR:-$DEFAULT_DIR}"

BINARY_PATH="$DIR/haloy"

# Check if binary exists
if [ ! -f "$BINARY_PATH" ]; then
    echo "Haloy client not found at $BINARY_PATH"
    echo "If you installed to a different directory, set the DIR environment variable:"
    echo "  DIR=/custom/path $0"
    exit 1
fi

# Remove the binary
rm -f "$BINARY_PATH"

# Remove client configuration files if they exist
CLIENT_CONFIG_DIR="$HOME/.config/haloy"
if [ -d "$CLIENT_CONFIG_DIR" ]; then
    echo "Removing client configuration from $CLIENT_CONFIG_DIR..."
    rm -rf "$CLIENT_CONFIG_DIR"
fi

echo "✅ Haloy client has been uninstalled successfully."
echo ""
echo "Note: This only removes the client binary and configuration."
echo "Server-side components (haloyd, haloyadm) are not affected."
echo "To remove server components, run the server uninstall script on your server."
