#!/bin/sh
# Haloy Server Uninstallation Script
#
# Removes haloyd daemon and all associated files.
# Optionally creates a backup before removal.
#
# USAGE:
#   sudo sh uninstall-server.sh
#
# OPTIONS (via environment variables):
#   FORCE=true          - Skip confirmation prompts
#   NO_BACKUP=true      - Skip backup prompt

set -e

# --- Colors and output helpers ---
setup_colors() {
    if [ -t 1 ] && command -v tput >/dev/null 2>&1; then
        GREEN=$(tput setaf 2)
        YELLOW=$(tput setaf 3)
        RED=$(tput setaf 1)
        BLUE=$(tput setaf 4)
        BOLD=$(tput bold)
        RESET=$(tput sgr0)
    else
        GREEN="" YELLOW="" RED="" BLUE="" BOLD="" RESET=""
    fi
}

success() {
    printf "  ${GREEN}✓${RESET} %s\n" "$1"
}

warn() {
    printf "  ${YELLOW}!${RESET} %s\n" "$1"
}

error() {
    printf "  ${RED}✗${RESET} %s\n" "$1" >&2
}

# Check if running interactively
is_interactive() {
    [ -t 0 ] && [ -t 1 ]
}

confirm() {
    local prompt="$1"

    if [ "$FORCE" = "true" ]; then
        return 0
    fi

    if ! is_interactive; then
        echo "Non-interactive mode: use FORCE=true to skip prompts"
        return 1
    fi

    printf "%s [y/N] " "$prompt"
    read -r response
    case "$response" in
        [yY][eE][sS]|[yY]) return 0 ;;
        *) return 1 ;;
    esac
}

# --- Init system detection ---
detect_init_system() {
    if [ -d /run/systemd/system ]; then
        echo "systemd"
    elif [ -f /sbin/openrc-run ]; then
        echo "openrc"
    elif [ -d /etc/init.d ] && [ ! -d /run/systemd/system ]; then
        echo "sysvinit"
    else
        echo "unknown"
    fi
}

# --- Main ---
main() {
    setup_colors

    echo ""
    echo "=============================================="
    echo "  ${BOLD}Haloy Server Uninstaller${RESET}"
    echo "=============================================="
    echo ""

    # Check root
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root"
        echo "  Run with: sudo $0"
        exit 1
    fi

    # --- Detect installation ---
    echo "Detecting installation..."
    echo ""

    HALOYD_BIN=""
    [ -f /usr/local/bin/haloyd ] && HALOYD_BIN="/usr/local/bin/haloyd"

    CONFIG_DIR=""
    [ -d /etc/haloy ] && CONFIG_DIR="/etc/haloy"

    DATA_DIR=""
    [ -d /var/lib/haloy ] && DATA_DIR="/var/lib/haloy"

    INIT_SYSTEM=$(detect_init_system)
    SERVICE_EXISTS=false

    case "$INIT_SYSTEM" in
        systemd)
            systemctl is-enabled haloyd >/dev/null 2>&1 && SERVICE_EXISTS=true
            ;;
        openrc)
            [ -f /etc/init.d/haloyd ] && SERVICE_EXISTS=true
            ;;
        sysvinit)
            [ -f /etc/init.d/haloyd ] && SERVICE_EXISTS=true
            ;;
    esac

    # Check if anything is installed
    if [ -z "$HALOYD_BIN" ] && [ -z "$CONFIG_DIR" ] && [ -z "$DATA_DIR" ] && [ "$SERVICE_EXISTS" = "false" ]; then
        echo "  No Haloy installation detected."
        exit 0
    fi

    echo "  ${BOLD}Components found:${RESET}"
    [ -n "$HALOYD_BIN" ] && echo "    - Binary: $HALOYD_BIN"
    [ "$SERVICE_EXISTS" = "true" ] && echo "    - Service: haloyd ($INIT_SYSTEM)"
    [ -n "$CONFIG_DIR" ] && echo "    - Config: $CONFIG_DIR"
    [ -n "$DATA_DIR" ] && echo "    - Data: $DATA_DIR"
    echo ""

    # --- Backup prompt ---
    if [ -n "$DATA_DIR" ] && [ "$NO_BACKUP" != "true" ]; then
        warn "Data directory contains:"
        echo "      - SSL certificates"
        echo "      - Deployment database"
        echo "      - Application configurations"
        echo ""

        if confirm "Create a backup before uninstalling?"; then
            BACKUP_FILE="/tmp/haloy-backup-$(date +%Y%m%d-%H%M%S).tar.gz"
            echo ""
            echo "  Creating backup..."
            tar -czf "$BACKUP_FILE" -C /var/lib haloy -C /etc haloy 2>/dev/null || true
            success "Backup created: $BACKUP_FILE"
            echo ""
            echo "  To restore later:"
            echo "    tar -xzf $BACKUP_FILE -C /"
            echo ""
        fi
    fi

    # --- Confirmation ---
    warn "This will permanently remove Haloy server components."
    warn "Deployed containers will NOT be removed."
    echo ""

    if ! confirm "Are you sure you want to uninstall Haloy?"; then
        echo ""
        echo "Uninstall cancelled."
        exit 0
    fi

    echo ""

    # --- Stop and remove service ---
    if [ "$SERVICE_EXISTS" = "true" ]; then
        echo "Stopping service..."
        case "$INIT_SYSTEM" in
            systemd)
                systemctl stop haloyd 2>/dev/null && success "Service stopped" || warn "Service was not running"
                systemctl disable haloyd 2>/dev/null && success "Service disabled" || true
                rm -f /etc/systemd/system/haloyd.service
                systemctl daemon-reload
                success "Systemd service removed"
                ;;
            openrc)
                rc-service haloyd stop 2>/dev/null && success "Service stopped" || warn "Service was not running"
                rc-update del haloyd default 2>/dev/null && success "Service disabled" || true
                rm -f /etc/init.d/haloyd
                success "OpenRC service removed"
                ;;
            sysvinit)
                /etc/init.d/haloyd stop 2>/dev/null && success "Service stopped" || warn "Service was not running"
                if command -v update-rc.d >/dev/null 2>&1; then
                    update-rc.d -f haloyd remove 2>/dev/null || true
                elif command -v chkconfig >/dev/null 2>&1; then
                    chkconfig --del haloyd 2>/dev/null || true
                fi
                rm -f /etc/init.d/haloyd
                success "SysVinit service removed"
                ;;
        esac
    fi

    # --- Remove binary ---
    if [ -n "$HALOYD_BIN" ]; then
        echo "Removing binary..."
        rm -f "$HALOYD_BIN"
        success "Binary removed"
    fi

    # --- Remove config ---
    if [ -n "$CONFIG_DIR" ]; then
        echo "Removing configuration..."
        rm -rf "$CONFIG_DIR"
        success "Configuration removed"
    fi

    # --- Remove data ---
    if [ -n "$DATA_DIR" ]; then
        echo "Removing data..."
        rm -rf "$DATA_DIR"
        success "Data removed"
    fi

    # --- Remove user (optional) ---
    if id "haloy" >/dev/null 2>&1; then
        if confirm "Remove 'haloy' system user?"; then
            userdel haloy 2>/dev/null || deluser haloy 2>/dev/null || true
            success "User 'haloy' removed"
        fi
    fi

    # --- Done ---
    echo ""
    echo "=============================================="
    echo "  ${GREEN}✓${RESET} ${BOLD}Haloy uninstalled${RESET}"
    echo "=============================================="
    echo ""
    echo "  Application containers are still running."
    echo "  To view them:"
    echo "    docker ps -a --filter label=dev.haloy.role=app"
    echo ""
    echo "  To remove them:"
    echo "    docker rm -f \$(docker ps -aq --filter label=dev.haloy.role=app)"
    echo ""
}

main "$@"
