#!/bin/sh

# Haloy Complete Cleanup Script
# Removes all haloy-related files from the system

set -e

IS_ROOT=false
if [ "$(id -u)" -eq 0 ]; then
    IS_ROOT=true
fi

echo "Haloy Complete Cleanup"
echo "======================"
echo ""

if [ "$IS_ROOT" = true ]; then
    echo "Running as root - will remove both system-wide and user-local files"
else
    echo "Running as non-root - will only remove user-local files"
    echo "Run with sudo to also remove system-wide installations"
fi

echo ""
echo "The following will be removed:"
echo ""

if [ "$IS_ROOT" = true ]; then
    echo "System-wide:"
    echo "  - Systemd service (haloyd.service)"
    echo "  - /usr/local/bin/haloy"
    echo "  - /usr/local/bin/haloyd"
    echo "  - /etc/haloy/"
    echo "  - /var/lib/haloy/"
    echo "  - /etc/bash_completion.d/haloy"
    echo "  - /usr/local/etc/bash_completion.d/haloy"
    echo ""
fi

echo "User-local:"
echo "  - ~/.local/bin/haloy"
echo "  - ~/.local/bin/haloyd"
echo "  - ~/.config/haloy/"
echo "  - ~/.local/share/haloy/"
echo "  - ~/.local/share/zsh/site-functions/_haloy"
echo "  - ~/.config/fish/completions/haloy.fish"
echo ""
echo "Docker:"
echo "  - haloy network"
echo ""


# Helper function to remove a file/directory with feedback
remove_path() {
    path="$1"
    if [ -e "$path" ] || [ -L "$path" ]; then
        rm -rf "$path"
        echo "Removed: $path"
    fi
}

# System-wide cleanup (requires root)
if [ "$IS_ROOT" = true ]; then
    echo "Cleaning up system-wide installation..."

    # Stop and disable systemd service
    if command -v systemctl >/dev/null 2>&1; then
        if systemctl is-active --quiet haloyd 2>/dev/null; then
            systemctl stop haloyd
            echo "Stopped haloyd service"
        fi
        if systemctl is-enabled --quiet haloyd 2>/dev/null; then
            systemctl disable haloyd
            echo "Disabled haloyd service"
        fi
        if [ -f "/etc/systemd/system/haloyd.service" ]; then
            remove_path "/etc/systemd/system/haloyd.service"
            systemctl daemon-reload
            echo "Reloaded systemd daemon"
        fi
    fi

    # Remove system binaries
    remove_path "/usr/local/bin/haloy"
    remove_path "/usr/local/bin/haloyd"

    # Remove system configuration
    remove_path "/etc/haloy"

    # Remove system data
    remove_path "/var/lib/haloy"

    # Remove system shell completions
    remove_path "/etc/bash_completion.d/haloy"
    remove_path "/usr/local/etc/bash_completion.d/haloy"

    echo ""
fi

# User-local cleanup (always runs)
echo "Cleaning up user-local installation..."

# Remove user binaries
remove_path "$HOME/.local/bin/haloy"
remove_path "$HOME/.local/bin/haloyd"

# Remove user configuration
remove_path "$HOME/.config/haloy"

# Remove user data
remove_path "$HOME/.local/share/haloy"

# Remove user shell completions
remove_path "$HOME/.local/share/zsh/site-functions/_haloy"
remove_path "$HOME/.config/fish/completions/haloy.fish"

echo ""

# Docker cleanup
echo "Cleaning up Docker resources..."
if command -v docker >/dev/null 2>&1; then
    if docker network inspect haloy >/dev/null 2>&1; then
        docker network rm haloy 2>/dev/null && echo "Removed: haloy docker network" || echo "Warning: Could not remove haloy docker network (may have connected containers)"
    fi
else
    echo "Docker not found, skipping network cleanup"
fi

echo ""
echo "Haloy cleanup complete."
echo ""
echo "Note: Application containers deployed by Haloy are not automatically removed."
echo "To list haloy-managed containers:"
echo "  docker ps -a --filter label=dev.haloy.appName"
echo ""
echo "To remove all haloy-managed containers:"
echo "  docker ps -aq --filter label=dev.haloy.appName | xargs -r docker rm -f"
