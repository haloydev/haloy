#!/usr/bin/env bash
set -e

# Get version
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
version=$("$SCRIPT_DIR/get-version.sh")

# Build haloyd
echo "Building haloyd $version..."
CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X 'github.com/haloydev/haloy/internal/constants.Version=$version'" \
  -o /tmp/haloyd "$SCRIPT_DIR/../cmd/haloyd"

# Install and restart
echo "Installing to /usr/local/bin/haloyd..."
sudo mv /tmp/haloyd /usr/local/bin/haloyd
sudo chmod +x /usr/local/bin/haloyd

echo "Restarting haloyd service..."
sudo systemctl restart haloyd

echo "Done! haloyd $version is now running."
