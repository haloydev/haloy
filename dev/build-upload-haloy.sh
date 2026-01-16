#!/usr/bin/env bash

set -e

# Ensure an argument is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

CLI_BINARY_NAME=haloy

HOSTNAME=$1

# Use the current username from the shell
USERNAME=$(whoami)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
version=$("$SCRIPT_DIR/get-version.sh")
echo "Building version: $version"

# Detect target platform
if [ "$HOSTNAME" = "localhost" ] || [ "$HOSTNAME" = "127.0.0.1" ]; then
    # Local deployment - detect current platform
    OS=$(uname -s)
    ARCH=$(uname -m)

    case "$OS" in
        "Darwin")
            GOOS="darwin"
            ;;
        "Linux")
            GOOS="linux"
            ;;
        *)
            echo "Unsupported OS: $OS"
            exit 1
            ;;
    esac

    case "$ARCH" in
        "x86_64")
            GOARCH="amd64"
            ;;
        "arm64")
            GOARCH="arm64"
            ;;
        "aarch64")
            GOARCH="arm64"
            ;;
        *)
            echo "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    echo "Building for local platform: $GOOS/$GOARCH"
else
    # Remote deployment - assume Linux amd64
    GOOS="linux"
    GOARCH="amd64"
    echo "Building for remote platform: $GOOS/$GOARCH"
fi

# Build the CLI binary using detected/default platform
# Using same flags as production: -s -w strips debug symbols, -trimpath for reproducible builds
CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" -o $CLI_BINARY_NAME ../cmd/haloy

# Support localhost: If HOSTNAME is localhost (or 127.0.0.1), use local commands instead of SSH/SCP.
if [ "$HOSTNAME" = "localhost" ] || [ "$HOSTNAME" = "127.0.0.1" ]; then
    echo "Using local deployment for ${HOSTNAME}"

    # Handle different home directory structures
    if [ "$OS" = "Darwin" ]; then
        # macOS
        LOCAL_BIN_DIR="/Users/${USERNAME}/.local/bin"
    else
        # Linux
        LOCAL_BIN_DIR="/home/${USERNAME}/.local/bin"
    fi

    mkdir -p "$LOCAL_BIN_DIR"
    cp $CLI_BINARY_NAME "$LOCAL_BIN_DIR/$CLI_BINARY_NAME"

    # Make binary executable
    chmod +x "$LOCAL_BIN_DIR/$CLI_BINARY_NAME"
else
    ssh "${USERNAME}@${HOSTNAME}" "mkdir -p /home/${USERNAME}/.local/bin"
    scp $CLI_BINARY_NAME ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$CLI_BINARY_NAME
fi

# Remove binary after copying
if [ -f "$CLI_BINARY_NAME" ]; then
    rm "$CLI_BINARY_NAME"
fi

echo "Successfully built and deployed haloy CLI for $GOOS/$GOARCH."
