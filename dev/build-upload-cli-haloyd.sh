#!/usr/bin/env bash

set -e

# Build and deploy haloy CLI and haloyd daemon to a remote server for development testing.
# haloyd runs as a native binary via systemd (not in Docker).

if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    echo ""
    echo "This script builds and deploys:"
    echo "  - haloy CLI (client tool)"
    echo "  - haloyd daemon (server binary)"
    echo ""
    echo "After deployment, run on the server:"
    echo "  sudo mv ~/.local/bin/haloyd /usr/local/bin/haloyd"
    echo "  sudo haloyd init  # if not already initialized"
    echo "  sudo systemctl restart haloyd"
    exit 1
fi

CLI_BINARY_NAME=haloy
DAEMON_BINARY_NAME=haloyd

HOSTNAME=$1
USERNAME=$(whoami)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
version=$("$SCRIPT_DIR/get-version.sh")
echo "Building version: $version"

# Always build for Linux amd64 (server platform)
GOOS="linux"
GOARCH="amd64"
echo "Building for platform: $GOOS/$GOARCH"

# Build the binaries to a temp directory to avoid conflicts with existing directories
BUILD_DIR=$(mktemp -d)
CLI_BUILD_PATH="$BUILD_DIR/$CLI_BINARY_NAME"
DAEMON_BUILD_PATH="$BUILD_DIR/$DAEMON_BINARY_NAME"

# Using same flags as production: -s -w strips debug symbols, -trimpath for reproducible builds
echo "Building haloy CLI..."
CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" -o "$CLI_BUILD_PATH" ../cmd/haloy

echo "Building haloyd daemon..."
CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" -o "$DAEMON_BUILD_PATH" ../cmd/haloyd

# Deploy to server
if [ "$HOSTNAME" = "localhost" ] || [ "$HOSTNAME" = "127.0.0.1" ]; then
    echo "Using local deployment for ${HOSTNAME}"
    LOCAL_BIN_DIR="/home/${USERNAME}/.local/bin"
    mkdir -p "$LOCAL_BIN_DIR"
    cp "$CLI_BUILD_PATH" "$LOCAL_BIN_DIR/$CLI_BINARY_NAME"
    cp "$DAEMON_BUILD_PATH" "$LOCAL_BIN_DIR/$DAEMON_BINARY_NAME"
    chmod +x "$LOCAL_BIN_DIR/$CLI_BINARY_NAME"
    chmod +x "$LOCAL_BIN_DIR/$DAEMON_BINARY_NAME"
else
    echo "Deploying to ${USERNAME}@${HOSTNAME}..."
    ssh "${USERNAME}@${HOSTNAME}" "mkdir -p /home/${USERNAME}/.local/bin"
    scp "$CLI_BUILD_PATH" ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$CLI_BINARY_NAME
    scp "$DAEMON_BUILD_PATH" ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$DAEMON_BINARY_NAME
fi

# Cleanup build directory
rm -rf "$BUILD_DIR"

echo ""
echo "Successfully deployed haloy CLI and haloyd daemon to ${HOSTNAME}"
echo ""
echo "To install haloyd system-wide and restart the service:"
echo "  ssh ${USERNAME}@${HOSTNAME}"
echo "  sudo mv ~/.local/bin/haloyd /usr/local/bin/haloyd"
echo "  sudo systemctl restart haloyd"
