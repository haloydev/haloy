#!/usr/bin/env bash

set -e

# Build and deploy haloyd daemon to a remote server for development testing.
# haloyd runs as a native binary via systemd (not in Docker).

USER_INSTALL=false
WITH_PROXY=false
HOSTNAME=""
VERSION_OVERRIDE=""
VERSION_SUFFIX="-dev"

while [[ $# -gt 0 ]]; do
    case $1 in
        --user)
            USER_INSTALL=true
            shift
            ;;
        --with-proxy)
            WITH_PROXY=true
            shift
            ;;
        --version)
            if [ -z "${2:-}" ]; then
                echo "Missing value for --version"
                exit 1
            fi
            VERSION_OVERRIDE=$2
            shift 2
            ;;
        --version=*)
            VERSION_OVERRIDE=${1#*=}
            shift
            ;;
        --version-suffix)
            if [ -z "${2:-}" ]; then
                echo "Missing value for --version-suffix"
                exit 1
            fi
            VERSION_SUFFIX=$2
            shift 2
            ;;
        --version-suffix=*)
            VERSION_SUFFIX=${1#*=}
            shift
            ;;
        --no-dev-suffix)
            VERSION_SUFFIX=""
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
    echo "Usage: $0 [--user] [--with-proxy] [--version <value>] [--version-suffix <suffix>] [--no-dev-suffix] <host|user@host>"
    echo ""
    echo "This script builds and deploys the haloyd daemon."
    echo ""
    echo "Options:"
    echo "  --user                    Install to ~/.local/bin instead of /usr/local/bin (no sudo)"
    echo "  --with-proxy              Also build/deploy haloy-proxy (restarts it: brief traffic blip)"
    echo "  --version <value>         Override the embedded haloyd version completely"
    echo "  --version-suffix <value>  Append a custom suffix to the detected version"
    echo "  --no-dev-suffix           Use the detected version without the default -dev suffix"
    echo ""
    echo "Default installs to /usr/local/bin (requires sudo)."
    exit 1
fi

DAEMON_BINARY_NAME=haloyd
DEFAULT_USERNAME=$(whoami)
TARGET_HOST=${HOSTNAME##*@}
if [[ "$HOSTNAME" == *"@"* ]]; then
    SSH_TARGET=$HOSTNAME
    TARGET_USER=${HOSTNAME%@*}
else
    SSH_TARGET="${DEFAULT_USERNAME}@${HOSTNAME}"
    TARGET_USER=$DEFAULT_USERNAME
fi
LOCAL_IS_ROOT=false
if [ "$(id -u)" -eq 0 ]; then
    LOCAL_IS_ROOT=true
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
version=$(HALOY_VERSION="$VERSION_OVERRIDE" HALOY_VERSION_SUFFIX="$VERSION_SUFFIX" "$SCRIPT_DIR/get-version.sh")
echo "Building version: $version"

# Always build for Linux amd64 (server platform)
GOOS="linux"
GOARCH="amd64"
echo "Building for platform: $GOOS/$GOARCH"

# Build the binary to a temp directory to avoid conflicts with existing directories
BUILD_DIR=$(mktemp -d)
DAEMON_BUILD_PATH="$BUILD_DIR/$DAEMON_BINARY_NAME"
PROXY_BINARY_NAME=haloy-proxy
PROXY_BUILD_PATH="$BUILD_DIR/$PROXY_BINARY_NAME"

# Using same flags as production: -s -w strips debug symbols, -trimpath for reproducible builds
echo "Building haloyd daemon..."
(
    cd "$REPO_ROOT"
    CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" -o "$DAEMON_BUILD_PATH" ./cmd/haloyd
)

if [ "$WITH_PROXY" = true ]; then
    echo "Building haloy-proxy daemon..."
    (
        cd "$REPO_ROOT"
        CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" -o "$PROXY_BUILD_PATH" ./cmd/haloy-proxy
    )
fi

# Deploy
if [ "$TARGET_HOST" = "localhost" ] || [ "$TARGET_HOST" = "127.0.0.1" ]; then
    if [ "$USER_INSTALL" = true ]; then
        # User install: ~/.local/bin (no sudo)
        INSTALL_DIR="$HOME/.local/bin"
        echo "Installing to $INSTALL_DIR..."
        mkdir -p "$INSTALL_DIR"
        mv "$DAEMON_BUILD_PATH" "$INSTALL_DIR/$DAEMON_BINARY_NAME"
        chmod +x "$INSTALL_DIR/$DAEMON_BINARY_NAME"
        if [ "$WITH_PROXY" = true ]; then
            mv "$PROXY_BUILD_PATH" "$INSTALL_DIR/$PROXY_BINARY_NAME"
            chmod +x "$INSTALL_DIR/$PROXY_BINARY_NAME"
        fi
    else
        # System install: /usr/local/bin (requires sudo)
        INSTALL_DIR="/usr/local/bin"
        if [ "$LOCAL_IS_ROOT" = true ]; then
            echo "Installing to $INSTALL_DIR..."
            mv "$DAEMON_BUILD_PATH" "$INSTALL_DIR/$DAEMON_BINARY_NAME"
            chmod +x "$INSTALL_DIR/$DAEMON_BINARY_NAME"
            if [ "$WITH_PROXY" = true ]; then
                mv "$PROXY_BUILD_PATH" "$INSTALL_DIR/$PROXY_BINARY_NAME"
                chmod +x "$INSTALL_DIR/$PROXY_BINARY_NAME"
                if systemctl is-active --quiet haloy-proxy 2>/dev/null; then
                    echo "Restarting haloy-proxy service..."
                    systemctl restart haloy-proxy
                fi
            fi
            if systemctl is-active --quiet haloyd 2>/dev/null; then
                echo "Restarting haloyd service..."
                systemctl restart haloyd
            fi
        else
            echo "Installing to $INSTALL_DIR (requires sudo)..."
            sudo mv "$DAEMON_BUILD_PATH" "$INSTALL_DIR/$DAEMON_BINARY_NAME"
            sudo chmod +x "$INSTALL_DIR/$DAEMON_BINARY_NAME"
            if [ "$WITH_PROXY" = true ]; then
                sudo mv "$PROXY_BUILD_PATH" "$INSTALL_DIR/$PROXY_BINARY_NAME"
                sudo chmod +x "$INSTALL_DIR/$PROXY_BINARY_NAME"
                if systemctl is-active --quiet haloy-proxy 2>/dev/null; then
                    echo "Restarting haloy-proxy service..."
                    sudo systemctl restart haloy-proxy
                fi
            fi
            if systemctl is-active --quiet haloyd 2>/dev/null; then
                echo "Restarting haloyd service..."
                sudo systemctl restart haloyd
            fi
        fi
    fi
else
    echo "Deploying to ${SSH_TARGET}..."
    scp "$DAEMON_BUILD_PATH" "${SSH_TARGET}":/tmp/$DAEMON_BINARY_NAME
    if [ "$WITH_PROXY" = true ]; then
        scp "$PROXY_BUILD_PATH" "${SSH_TARGET}":/tmp/$PROXY_BINARY_NAME
    fi
    if [ "$USER_INSTALL" = true ]; then
        # User install: ~/.local/bin (no sudo)
        INSTALL_DIR="~/.local/bin"
        ssh "$SSH_TARGET" "mkdir -p \$HOME/.local/bin && mv /tmp/$DAEMON_BINARY_NAME \$HOME/.local/bin/$DAEMON_BINARY_NAME && chmod +x \$HOME/.local/bin/$DAEMON_BINARY_NAME"
        if [ "$WITH_PROXY" = true ]; then
            ssh "$SSH_TARGET" "mv /tmp/$PROXY_BINARY_NAME \$HOME/.local/bin/$PROXY_BINARY_NAME && chmod +x \$HOME/.local/bin/$PROXY_BINARY_NAME"
        fi
    else
        # System install: /usr/local/bin (requires sudo)
        INSTALL_DIR="/usr/local/bin"
        if [ "$TARGET_USER" = "root" ]; then
            SUDO=""
        else
            SUDO="sudo "
        fi
        if [ "$WITH_PROXY" = true ]; then
            ssh -t "$SSH_TARGET" "${SUDO}mv /tmp/$PROXY_BINARY_NAME /usr/local/bin/$PROXY_BINARY_NAME && ${SUDO}chmod +x /usr/local/bin/$PROXY_BINARY_NAME && (systemctl is-active --quiet haloy-proxy 2>/dev/null && ${SUDO}systemctl restart haloy-proxy || true)"
        fi
        ssh -t "$SSH_TARGET" "${SUDO}mv /tmp/$DAEMON_BINARY_NAME /usr/local/bin/$DAEMON_BINARY_NAME && ${SUDO}chmod +x /usr/local/bin/$DAEMON_BINARY_NAME && (systemctl is-active --quiet haloyd 2>/dev/null && ${SUDO}systemctl restart haloyd || true)"
    fi
fi

# Cleanup build directory
rm -rf "$BUILD_DIR"

echo ""
echo "Successfully deployed haloyd to ${HOSTNAME}:${INSTALL_DIR}"
if [ "$USER_INSTALL" = false ]; then
    echo ""
    echo "If this is a first-time install, run:"
    if { [ "$TARGET_HOST" = "localhost" ] || [ "$TARGET_HOST" = "127.0.0.1" ]; } && [ "$LOCAL_IS_ROOT" = true ]; then
        echo "  haloyd init --api-domain <domain>"
    elif [ "$TARGET_USER" = "root" ]; then
        echo "  haloyd init --api-domain <domain>"
    else
        echo "  sudo haloyd init --api-domain <domain>"
    fi
fi
