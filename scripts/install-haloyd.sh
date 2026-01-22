#!/bin/sh
# Haloy Daemon (haloyd) Installation Script
#
# This script downloads and installs the haloyd daemon on Linux servers.
# It creates a dedicated haloy user, installs the systemd/OpenRC service,
# and starts the daemon.
#
# USAGE:
#   curl -fsSL https://sh.haloy.dev/install-haloyd.sh | sudo sh
#
# OPTIONS (via environment variables):
#   VERSION=v0.1.0      - Install specific version (default: latest)
#   SKIP_START=true     - Don't start the service after installation
#   INSTALL_DOCKER=true - Automatically install Docker if not present
#   API_DOMAIN=...      - Set API domain during init
#
# PREREQUISITES:
#   - Linux (Ubuntu, Debian, CentOS, RHEL, Fedora, Alpine)
#   - Docker installed and running
#   - Root privileges (sudo)
#
# MORE INFO: https://haloy.dev/docs/server-installation

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

STEP=0
TOTAL_STEPS=8

step() {
    STEP=$((STEP + 1))
    printf "\n${BLUE}[%d/%d]${RESET} ${BOLD}%s${RESET}\n" "$STEP" "$TOTAL_STEPS" "$1"
}

success() {
    printf "  ${GREEN}✓${RESET} %s\n" "$1"
}

warn() {
    printf "  ${YELLOW}!${RESET} %s\n" "$1" >&2
}

error() {
    printf "  ${RED}✗${RESET} %s\n" "$1" >&2
}

error_exit() {
    error "$1"
    if [ -n "$2" ]; then
        echo ""
        echo "  Troubleshooting:"
        shift
        for hint in "$@"; do
            echo "    - $hint"
        done
    fi
    echo ""
    echo "  Documentation: https://haloy.dev/docs/server-installation"
    exit 1
}

# --- Public IP detection ---
detect_public_ip() {
    curl -sS --max-time 5 https://api.ipify.org 2>/dev/null || echo ""
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

# --- Docker installation ---
install_docker() {
    curl -fsSL https://sh.haloy.dev/install-docker.sh | sh || return 1
}

# --- Main installation ---
main() {
    setup_colors

    echo ""
    echo "=============================================="
    echo "  ${BOLD}Haloy Server Installation${RESET}"
    echo "=============================================="

    # Check root
    if [ "$(id -u)" -ne 0 ]; then
        error_exit "This script must be run as root" \
            "Run with: sudo sh install-haloyd.sh" \
            "Or: curl -fsSL https://sh.haloy.dev/install-haloyd.sh | sudo sh"
    fi

    # --- Step 1: Detect system ---
    step "Detecting system configuration"

    OS=$(uname -s)
    case "$OS" in
        Linux*)
            success "OS: Linux"
            ;;
        *)
            error_exit "Unsupported OS: $OS" \
                "haloyd only runs on Linux servers" \
                "Use 'haloy' (client CLI) on macOS/Windows to manage remote servers"
            ;;
    esac

    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)
            ARCH="amd64"
            success "Architecture: amd64 (x86_64)"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            success "Architecture: arm64"
            ;;
        *)
            error_exit "Unsupported architecture: $ARCH" \
                "Haloy supports amd64 (x86_64) and arm64 only"
            ;;
    esac

    INIT_SYSTEM=$(detect_init_system)
    success "Init system: $INIT_SYSTEM"

    # --- Step 2: Check prerequisites ---
    step "Checking prerequisites"

    # Check Docker
    if ! command -v docker >/dev/null 2>&1; then
        if [ "$INSTALL_DOCKER" = "true" ]; then
            warn "Docker is not installed - installing automatically"
            install_docker || error_exit "Failed to install Docker"
            success "Docker installed"
        else
            error_exit "Docker is not installed" \
                "Auto-install: INSTALL_DOCKER=true curl -fsSL https://sh.haloy.dev/install-haloyd.sh | sh" \
                "Or run: curl -fsSL https://sh.haloy.dev/install-docker.sh | sh"
        fi
    fi
    DOCKER_VERSION=$(docker --version 2>/dev/null | awk '{print $3}' | tr -d ',')
    success "Docker: $DOCKER_VERSION"

    # Check Docker daemon is running
    if ! docker info >/dev/null 2>&1; then
        error_exit "Docker daemon is not running" \
            "Start Docker with: systemctl start docker" \
            "Or: service docker start"
    fi
    success "Docker daemon: running"

    # Check disk space (at least 500MB free in /var/lib)
    AVAILABLE_MB=$(df -m /var/lib 2>/dev/null | tail -1 | awk '{print $4}')
    if [ -n "$AVAILABLE_MB" ] && [ "$AVAILABLE_MB" -lt 500 ]; then
        warn "Low disk space: ${AVAILABLE_MB}MB available (500MB+ recommended)"
    else
        success "Disk space: ${AVAILABLE_MB}MB available"
    fi

    # --- Step 3: Create haloy user ---
    step "Creating haloy system user"

    if id "haloy" >/dev/null 2>&1; then
        success "User 'haloy' already exists"
    else
        useradd --system --shell /sbin/nologin --home-dir /var/lib/haloy --no-create-home haloy 2>/dev/null || \
        useradd -r -s /bin/false -d /var/lib/haloy haloy 2>/dev/null || \
        adduser -S -D -H -h /var/lib/haloy -s /sbin/nologin haloy 2>/dev/null
        success "Created system user 'haloy'"
    fi

    # Add haloy to docker group
    if getent group docker >/dev/null 2>&1; then
        usermod -aG docker haloy 2>/dev/null || adduser haloy docker 2>/dev/null || true
        success "Added 'haloy' to docker group"
    else
        warn "Docker group not found - haloy may not be able to access Docker"
    fi

    # --- Step 4: Download binary ---
    step "Downloading haloyd"

    # Determine version
    if [ -z "$VERSION" ]; then
        # Get latest release
        VERSION=$(curl -sL -H 'Accept: application/json' "https://api.github.com/repos/haloydev/haloy/releases/latest" 2>/dev/null | \
            sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' || echo "")

        if [ -z "$VERSION" ]; then
            # Try getting most recent release (including prereleases)
            VERSION=$(curl -sL -H 'Accept: application/json' "https://api.github.com/repos/haloydev/haloy/releases" | \
                sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' | head -1)
        fi
    fi

    if [ -z "$VERSION" ]; then
        error_exit "Could not determine latest version" \
            "Check your internet connection" \
            "Try specifying VERSION=v0.1.0 manually"
    fi

    success "Version: $VERSION"

    BINARY_NAME="haloyd-linux-${ARCH}"
    DOWNLOAD_URL="https://github.com/haloydev/haloy/releases/download/${VERSION}/${BINARY_NAME}"
    INSTALL_PATH="/usr/local/bin/haloyd"

    curl -fSL -o "$INSTALL_PATH" "$DOWNLOAD_URL" || \
        error_exit "Failed to download haloyd" \
            "Check if version $VERSION exists" \
            "URL: $DOWNLOAD_URL"

    chmod +x "$INSTALL_PATH"
    success "Installed to $INSTALL_PATH"

    # --- Step 5: Initialize Haloy ---
    step "Initializing Haloy"

    INIT_ARGS=""
    if [ -n "$API_DOMAIN" ]; then
        INIT_ARGS="$INIT_ARGS --api-domain=$API_DOMAIN"
    fi

    # Run init (may fail if already initialized, that's OK)
    if [ -d /var/lib/haloy ] && [ -d /etc/haloy ]; then
        warn "Haloy directories already exist, skipping init"
        warn "Use 'haloyd init --override' to reinitialize"
    else
        # shellcheck disable=SC2086
        "$INSTALL_PATH" init $INIT_ARGS || error_exit "Failed to initialize Haloy"
        success "Configuration created"
    fi

    # --- Step 6: Set ownership ---
    step "Setting file permissions"

    chown -R haloy:haloy /var/lib/haloy
    chown -R haloy:haloy /etc/haloy
    chmod 700 /var/lib/haloy
    chmod 700 /etc/haloy
    success "Ownership set to haloy:haloy"

    # --- Step 7: Install service ---
    step "Installing service"

    case "$INIT_SYSTEM" in
        systemd)
            install_systemd_service
            ;;
        openrc)
            install_openrc_service
            ;;
        sysvinit)
            install_sysvinit_service
            ;;
        *)
            warn "Unknown init system - skipping service installation"
            warn "You can start haloyd manually with: haloyd serve"
            ;;
    esac

    # --- Step 8: Start service ---
    step "Starting service"

    if [ "$SKIP_START" = "true" ]; then
        warn "Skipping service start (SKIP_START=true)"
    else
        case "$INIT_SYSTEM" in
            systemd)
                systemctl daemon-reload
                systemctl enable haloyd >/dev/null 2>&1
                systemctl start haloyd
                success "Service started and enabled"
                ;;
            openrc)
                rc-update add haloyd default >/dev/null 2>&1
                rc-service haloyd start
                success "Service started and enabled"
                ;;
            sysvinit)
                if command -v update-rc.d >/dev/null 2>&1; then
                    update-rc.d haloyd defaults >/dev/null 2>&1
                elif command -v chkconfig >/dev/null 2>&1; then
                    chkconfig --add haloyd >/dev/null 2>&1
                fi
                /etc/init.d/haloyd start
                success "Service started"
                ;;
            *)
                warn "Start haloyd manually with: haloyd serve"
                ;;
        esac
    fi

    # --- Done ---
    print_success
}

# --- Service file installers ---

install_systemd_service() {
    cat > /etc/systemd/system/haloyd.service << 'EOF'
[Unit]
Description=Haloy Daemon
After=network-online.target docker.service
Requires=docker.service
Wants=network-online.target

[Service]
Type=simple
User=haloy
Group=haloy
ExecStart=/usr/local/bin/haloyd serve
Restart=always
RestartSec=5
Environment=HALOY_DATA_DIR=/var/lib/haloy
Environment=HALOY_CONFIG_DIR=/etc/haloy

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/haloy
ReadOnlyPaths=/etc/haloy
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
    success "Installed systemd service"
}

install_openrc_service() {
    cat > /etc/init.d/haloyd << 'EOF'
#!/sbin/openrc-run

name="haloyd"
description="Haloy Daemon"
command="/usr/local/bin/haloyd"
command_args="serve"
command_background="yes"
command_user="haloy:haloy"
pidfile="/run/haloyd/haloyd.pid"

export HALOY_DATA_DIR="/var/lib/haloy"
export HALOY_CONFIG_DIR="/etc/haloy"

depend() {
    need net docker
    after firewall
}

start_pre() {
    checkpath --directory --owner haloy:haloy --mode 0755 /run/haloyd
}
EOF
    chmod +x /etc/init.d/haloyd
    success "Installed OpenRC service"
}

install_sysvinit_service() {
    cat > /etc/init.d/haloyd << 'EOF'
#!/bin/sh
### BEGIN INIT INFO
# Provides:          haloyd
# Required-Start:    $network $remote_fs docker
# Required-Stop:     $network $remote_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Haloy Daemon
### END INIT INFO

NAME="haloyd"
DAEMON="/usr/local/bin/haloyd"
DAEMON_ARGS="serve"
PIDFILE="/var/run/haloyd.pid"
LOGFILE="/var/log/haloyd.log"
USER="haloy"

export HALOY_DATA_DIR="/var/lib/haloy"
export HALOY_CONFIG_DIR="/etc/haloy"

start() {
    echo "Starting $NAME..."
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "$NAME is already running"
        return 1
    fi
    su -s /bin/sh -c "nohup $DAEMON $DAEMON_ARGS >> $LOGFILE 2>&1 & echo \$! > $PIDFILE" "$USER"
    echo "$NAME started"
}

stop() {
    echo "Stopping $NAME..."
    if [ ! -f "$PIDFILE" ]; then
        echo "$NAME is not running"
        return 1
    fi
    kill "$(cat "$PIDFILE")" 2>/dev/null
    rm -f "$PIDFILE"
    echo "$NAME stopped"
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "$NAME is running (PID: $(cat "$PIDFILE"))"
    else
        echo "$NAME is not running"
        return 1
    fi
}

case "$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; start ;;
    status)  status ;;
    *)       echo "Usage: $0 {start|stop|restart|status}" ;;
esac
EOF
    chmod +x /etc/init.d/haloyd
    success "Installed SysVinit service"
}

# --- Success message ---
print_success() {
    # Get API token
    API_TOKEN=""
    if [ -f /etc/haloy/.env ]; then
        API_TOKEN=$(grep "HALOY_API_TOKEN" /etc/haloy/.env 2>/dev/null | cut -d'=' -f2)
    fi

    # Detect public IP
    PUBLIC_IP=$(detect_public_ip)

    echo ""
    echo "=============================================="
    echo "  ${GREEN}✓${RESET} ${BOLD}Haloy installed successfully!${RESET}"
    echo "=============================================="
    echo ""
    echo "  Version:  $VERSION"
    if [ -n "$PUBLIC_IP" ]; then
        echo "  Server IP: $PUBLIC_IP"
    fi
    if [ -n "$API_DOMAIN" ]; then
        echo "  API:      https://$API_DOMAIN"
    fi
    echo ""
    echo "  User:     haloy"
    echo "  Data:     /var/lib/haloy"
    echo "  Config:   /etc/haloy"
    echo ""

    if [ -n "$API_TOKEN" ]; then
        echo "  ${BOLD}API Token:${RESET}"
        echo "  $API_TOKEN"
        echo ""
    fi

    # Show different output based on whether domain is configured
    if [ -z "$API_DOMAIN" ]; then
        # Unconfigured: show warning and setup instructions
        echo "  ${YELLOW}⚠${RESET} ${BOLD}Configuration Required${RESET}"
        echo ""
        echo "  Your server needs a domain configured for remote access."
        if [ -n "$PUBLIC_IP" ]; then
            echo "  First, point your domain's DNS A record to: ${BOLD}$PUBLIC_IP${RESET}"
            echo ""
        fi
        echo "  Then configure haloy:"
        echo "    ${BOLD}sudo haloyd config set api-domain YOUR_DOMAIN${RESET}"
        case "$INIT_SYSTEM" in
            systemd)
                echo "    ${BOLD}sudo systemctl restart haloyd${RESET}"
                ;;
            openrc)
                echo "    ${BOLD}sudo rc-service haloyd restart${RESET}"
                ;;
            *)
                echo "    ${BOLD}sudo /etc/init.d/haloyd restart${RESET}"
                ;;
        esac
        echo ""
        echo "  Or reinstall with configuration:"
        echo "    API_DOMAIN=... curl -fsSL https://sh.haloy.dev/install-haloyd.sh | sudo sh"
        echo ""
        echo "  Once configured, add this server on your local machine:"
        echo "    haloy server add YOUR_DOMAIN \"$API_TOKEN\""
    else
        # Configured: show useful commands and next step
        echo "  ${BOLD}Useful Commands:${RESET}"
        case "$INIT_SYSTEM" in
            systemd)
                echo "    Check status:   systemctl status haloyd"
                echo "    View logs:      journalctl -u haloyd -f"
                echo "    Restart:        systemctl restart haloyd"
                ;;
            openrc)
                echo "    Check status:   rc-service haloyd status"
                echo "    View logs:      tail -f /var/log/haloyd.log"
                echo "    Restart:        rc-service haloyd restart"
                ;;
            *)
                echo "    Check status:   /etc/init.d/haloyd status"
                echo "    View logs:      tail -f /var/log/haloyd.log"
                echo "    Restart:        /etc/init.d/haloyd restart"
                ;;
        esac
        echo ""
        echo "  ${BOLD}Next Step:${RESET}"
        echo "    On your local machine, add this server:"
        echo "    haloy server add $API_DOMAIN \"$API_TOKEN\""
    fi
    echo ""
    echo "  Documentation: https://haloy.dev/docs"
    echo ""
}

main "$@"
