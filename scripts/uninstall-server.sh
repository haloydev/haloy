#!/bin/sh
# Haloy Server Uninstallation Script
#
# Removes haloyd daemon and all associated files.
#
# USAGE:
#   curl -sL https://sh.haloy.dev/uninstall-server.sh | sh
#   # or
#   sudo sh uninstall-server.sh

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

    warn "Removing Haloy server components..."
    warn "Deployed containers will NOT be removed."
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

    # --- Remove user ---
    if id "haloy" >/dev/null 2>&1; then
        echo "Removing user..."
        userdel haloy 2>/dev/null || deluser haloy 2>/dev/null || true
        success "User 'haloy' removed"
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
