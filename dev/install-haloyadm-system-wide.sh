#!/usr/bin/env bash

set -e

if [ -n "$SUDO_USER" ]; then
    LOCAL_BIN_DIR="/home/${SUDO_USER}/.local/bin"
else
    LOCAL_BIN_DIR="$HOME/.local/bin"
fi
SYSTEM_BIN_DIR="/usr/local/bin"
BINARY_NAME="haloyadm"

if [ ! -f "$LOCAL_BIN_DIR/$BINARY_NAME" ]; then
    echo "Error: $BINARY_NAME not found in $LOCAL_BIN_DIR"
    exit 1
fi

echo "Installing $BINARY_NAME system-wide..."
sudo mv "$LOCAL_BIN_DIR/$BINARY_NAME" "$SYSTEM_BIN_DIR/$BINARY_NAME"
sudo chmod +x "$SYSTEM_BIN_DIR/$BINARY_NAME"

echo "Successfully installed $BINARY_NAME to $SYSTEM_BIN_DIR"
