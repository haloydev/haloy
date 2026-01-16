#!/usr/bin/env bash

set -e

# Build and deploy haloyd daemon to a remote server for development testing.
# haloyd runs as a native binary via systemd (not in Docker).

USER_INSTALL=false
HOSTNAME=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --user)
            USER_INSTALL=true
            shift
            ;;
        -*)
            echo "Unknown option: $1"
            exit 1
            ;;
        *)
            HOSTNAME=$1
            shift
            ;;
    esac
done

if [ -z "$HOSTNAME" ]; then
    echo "Usage: $0 [--user] <hostname>"
    echo ""
    echo "This script builds and deploys the haloyd daemon."
    echo ""
    echo "Options:"
    echo "  --user    Install to ~/.local/bin instead of /usr/local/bin (no sudo)"
    echo ""
    echo "Default installs to /usr/local/bin (requires sudo)."
    exit 1
fi

DAEMON_BINARY_NAME=haloyd
USERNAME=$(whoami)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
version=$("$SCRIPT_DIR/get-version.sh")
echo "Building version: $version"

# Always build for Linux amd64 (server platform)
GOOS="linux"
GOARCH="amd64"
echo "Building for platform: $GOOS/$GOARCH"

# Build the binary to a temp directory to avoid conflicts with existing directories
BUILD_DIR=$(mktemp -d)
DAEMON_BUILD_PATH="$BUILD_DIR/$DAEMON_BINARY_NAME"

# Using same flags as production: -s -w strips debug symbols, -trimpath for reproducible builds
echo "Building haloyd daemon..."
CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" -o "$DAEMON_BUILD_PATH" ../cmd/haloyd

# Deploy
if [ "$HOSTNAME" = "localhost" ] || [ "$HOSTNAME" = "127.0.0.1" ]; then
    if [ "$USER_INSTALL" = true ]; then
        # User install: ~/.local/bin (no sudo)
        INSTALL_DIR="$HOME/.local/bin"
        echo "Installing to $INSTALL_DIR..."
        mkdir -p "$INSTALL_DIR"
        mv "$DAEMON_BUILD_PATH" "$INSTALL_DIR/$DAEMON_BINARY_NAME"
        chmod +x "$INSTALL_DIR/$DAEMON_BINARY_NAME"
    else
        # System install: /usr/local/bin (requires sudo)
        INSTALL_DIR="/usr/local/bin"
        echo "Installing to $INSTALL_DIR (requires sudo)..."
        sudo mv "$DAEMON_BUILD_PATH" "$INSTALL_DIR/$DAEMON_BINARY_NAME"
        sudo chmod +x "$INSTALL_DIR/$DAEMON_BINARY_NAME"
        if systemctl is-active --quiet haloyd 2>/dev/null; then
            echo "Restarting haloyd service..."
            sudo systemctl restart haloyd
        fi
    fi
else
    echo "Deploying to ${USERNAME}@${HOSTNAME}..."
    scp "$DAEMON_BUILD_PATH" "${USERNAME}@${HOSTNAME}":/tmp/$DAEMON_BINARY_NAME
    if [ "$USER_INSTALL" = true ]; then
        # User install: ~/.local/bin (no sudo)
        INSTALL_DIR="~/.local/bin"
        ssh "${USERNAME}@${HOSTNAME}" "mkdir -p \$HOME/.local/bin && mv /tmp/$DAEMON_BINARY_NAME \$HOME/.local/bin/$DAEMON_BINARY_NAME && chmod +x \$HOME/.local/bin/$DAEMON_BINARY_NAME"
    else
        # System install: /usr/local/bin (requires sudo)
        INSTALL_DIR="/usr/local/bin"
        ssh -t "${USERNAME}@${HOSTNAME}" "sudo mv /tmp/$DAEMON_BINARY_NAME /usr/local/bin/$DAEMON_BINARY_NAME && sudo chmod +x /usr/local/bin/$DAEMON_BINARY_NAME && (systemctl is-active --quiet haloyd 2>/dev/null && sudo systemctl restart haloyd || true)"
    fi
fi

# Cleanup build directory
rm -rf "$BUILD_DIR"

echo ""
echo "Successfully deployed haloyd to ${HOSTNAME}:${INSTALL_DIR}"
if [ "$USER_INSTALL" = false ]; then
    echo ""
    echo "If this is a first-time install, run:"
    echo "  sudo haloyd init --api-domain <domain> --acme-email <email>"
fi
