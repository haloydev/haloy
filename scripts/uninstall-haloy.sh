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

    # Check if installed via Homebrew
    if command -v brew &> /dev/null; then
        if brew list haloy &> /dev/null 2>&1; then
            echo ""
            echo "✓ Found haloy installed via Homebrew"
            echo ""
            echo "To uninstall, please run:"
            echo "  brew uninstall haloy"
            echo ""
            echo "Note: This will remove the binary but not your configuration files."
            echo "To remove configuration, also run:"
            echo "  rm -rf ~/.config/haloy"
            exit 0
        fi
    fi

    echo ""
    echo "If you installed to a different directory, set the DIR environment variable:"
    echo "  DIR=/custom/path $0"
    echo ""
    echo "If installed via Homebrew, run:"
    echo "  brew uninstall haloy"
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
