#!/usr/bin/env bash

set -e

# Detect if we should install system-wide or user-specific
if [ "$(id -u)" -eq 0 ] || [ "$FORCE_SYSTEM_INSTALL" = "true" ]; then
    # System-wide installation (requires root)
    DEFAULT_DIR="/usr/local/bin"
else
    # User installation
    DEFAULT_DIR="$HOME/.local/bin"
fi

# The directory to install the binary to. Can be overridden by setting the DIR environment variable.
DIR="${DIR:-"$DEFAULT_DIR"}"

echo "Installing Haloy admin tool to: $DIR"

# Check permissions for system installation
if [[ "$DIR" == "/usr/local/bin" ]] && [ "$(id -u)" -ne 0 ]; then
    echo "Error: Installing to $DIR requires root privileges." >&2
    echo "Run with sudo or set DIR environment variable:" >&2
    echo "  sudo $0" >&2
    echo "  DIR=\$HOME/.local/bin $0" >&2
    exit 1
fi

# Warn if installing to user directory for admin tool
if [[ "$DIR" == "$HOME/.local/bin" ]] && [ "$(id -u)" -ne 0 ]; then
    echo "⚠️  Installing to user directory ($DIR)"
    echo "   For system-wide installation, run with sudo:"
    echo "   sudo curl -sL https://raw.githubusercontent.com/haloydev/haloy/main/scripts/install-haloyadm.sh | bash"
    echo ""
fi

# --- Auto-detect OS and Architecture ---
OS=$(uname -s)
case "$OS" in
    Linux*)   PLATFORM="linux" ;;
    Darwin*)  PLATFORM="darwin" ;;
    *)        echo "Error: Unsupported OS '$OS'. Haloy supports Linux and macOS." >&2; exit 1 ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64)   ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)        echo "Error: Unsupported architecture '$ARCH'. Haloy supports amd64 (x86_64) and arm64." >&2; exit 1 ;;
esac

# --- Fetch the latest version from GitHub ---
echo "Finding the latest version of Haloy..."

# Try to get the latest stable release first
GITHUB_LATEST_VERSION=$(curl -sL -H 'Accept: application/json' "https://api.github.com/repos/haloydev/haloy/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' || echo "")

# If no stable release found, get the most recent release (including prereleases)
if [ -z "$GITHUB_LATEST_VERSION" ]; then
    echo "No stable release found, checking for prereleases..."
    GITHUB_LATEST_VERSION=$(curl -sL -H 'Accept: application/json' "https://api.github.com/repos/haloydev/haloy/releases" | sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' | head -1)
fi

if [ -z "$GITHUB_LATEST_VERSION" ]; then
    echo "Error: Could not determine the latest Haloy version from GitHub." >&2
    exit 1
fi

echo "Found version: $GITHUB_LATEST_VERSION"

# --- Download and Install ---
BINARY_NAME="haloyadm-${PLATFORM}-${ARCH}"
DOWNLOAD_URL="https://github.com/haloydev/haloy/releases/download/${GITHUB_LATEST_VERSION}/${BINARY_NAME}"
INSTALL_PATH="$DIR/haloyadm"

# Create the installation directory if it doesn't exist
mkdir -p "$DIR"

echo "Downloading Haloy admin tool ${GITHUB_LATEST_VERSION} for ${PLATFORM}/${ARCH}..."
curl -L -o "$INSTALL_PATH" "$DOWNLOAD_URL"
chmod +x "$INSTALL_PATH"

echo ""
echo "✅ Haloy admin tool (haloyadm) has been installed to '$INSTALL_PATH'"
echo ""

# Show appropriate PATH instructions based on installation location
if [[ "$DIR" == "/usr/local/bin" ]]; then
    echo "The binary is installed in /usr/local/bin which should already be in your PATH."
    echo "You can now run: sudo haloyadm init"
else
    echo "Please ensure '$DIR' is in your system's PATH."
    echo "You can check by running: 'echo \$PATH'"
    echo "If not, add it to your shell's profile (e.g., ~/.bashrc, ~/.zshrc):"
    echo "   export PATH=\"$DIR:\$PATH\""
    echo ""
    echo "You can now run: haloyadm init --local-install"
fi
