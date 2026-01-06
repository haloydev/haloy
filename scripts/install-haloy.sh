#!/bin/sh
set -e

# The directory to install the binary to. Can be overridden by setting the DIR environment variable.
DIR="${DIR:-"$HOME/.local/bin"}"

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
GITHUB_API_URL="https://api.github.com/repos/haloydev/haloy/releases"
GITHUB_RESPONSE=$(curl -sL -H 'Accept: application/json' "$GITHUB_API_URL")

# Check if the response indicates no releases
if echo "$GITHUB_RESPONSE" | grep -q '"message": "Not Found"'; then
    echo "Error: No releases found for Haloy. Please check https://github.com/haloydev/haloy/releases" >&2
    exit 1
fi

GITHUB_LATEST_VERSION=$(echo "$GITHUB_RESPONSE" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')

if [ -z "$GITHUB_LATEST_VERSION" ]; then
    echo "Error: Could not determine the latest Haloy version from GitHub." >&2
    exit 1
fi

# --- Download and Install ---
BINARY_NAME="haloy-${PLATFORM}-${ARCH}"
DOWNLOAD_URL="https://github.com/haloydev/haloy/releases/download/${GITHUB_LATEST_VERSION}/${BINARY_NAME}"
INSTALL_PATH="$DIR/haloy"

# Create the installation directory if it doesn't exist
mkdir -p "$DIR"

echo "Downloading Haloy ${GITHUB_LATEST_VERSION} for ${PLATFORM}/${ARCH}..."
curl -L -o "$INSTALL_PATH" "$DOWNLOAD_URL"
chmod +x "$INSTALL_PATH"

echo ""
echo "Haloy client has been installed to '$INSTALL_PATH'"
echo ""

# --- Check if DIR is in PATH ---
# Use case statement for POSIX-compliant substring check
case ":$PATH:" in
    *":$DIR:"*)
        echo "You can now run 'haloy' from anywhere!"
        ;;
    *)
        echo "Warning: '$DIR' is not in your PATH."
        echo ""
        echo "Add the following line to your shell profile (~/.bashrc, ~/.zshrc, or equivalent):"
        echo ""
        echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
        echo "Then restart your shell or run: source ~/.bashrc (or ~/.zshrc)"
        ;;
esac
