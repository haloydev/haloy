#!/bin/sh

set -e

# Haloy Server Upgrade Script
# Upgrades native Linux haloy server installs managed by systemd, OpenRC, or SysVinit.
#
# The server runs two daemons:
#   haloyd      - control plane (deployments, certificates, API)
#   haloy-proxy - data plane (ports 80/443), keeps serving while haloyd restarts
#
# USAGE:
#   upgrade-server.sh                    Upgrade haloyd. Traffic is not
#                                        interrupted: haloy-proxy keeps serving.
#                                        If haloy-proxy is not installed yet
#                                        (pre-split install), it is installed
#                                        first (brief traffic blip while ports
#                                        move from old haloyd to haloy-proxy).
#   upgrade-server.sh --component=proxy  Upgrade haloy-proxy (brief restart).

GITHUB_REPO="${GITHUB_REPO:-haloydev/haloy}"
SERVICE_NAME="${HALOYD_SERVICE_NAME:-haloyd}"
PROXY_SERVICE_NAME="${HALOY_PROXY_SERVICE_NAME:-haloy-proxy}"
SLEEP_SECONDS="${HALOY_UPGRADE_SLEEP_SECONDS:-3}"
SYSTEMD_UNIT_DIR="${HALOY_SYSTEMD_UNIT_DIR:-/etc/systemd/system}"
INIT_D_DIR="${HALOY_INIT_D_DIR:-/etc/init.d}"

HALOYD_TMP=""
PROXY_TMP=""
BACKUP_FILE=""
UNIT_BACKUP_FILE=""
UNIT_RESTORE_PATH=""
SERVICE_STOPPED=false
PROXY_STARTED=false

error_exit() {
    echo "Error: $1" >&2
    exit 1
}

warn() {
    echo "Warning: $1" >&2
}

normalize_version() {
    echo "$1" | sed 's/^v//'
}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error_exit "This script must be run as root. Run it with sudo."
    fi
}

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        error_exit "'$1' is required but was not found."
    fi
}

detect_init_system() {
    if [ -n "${HALOY_UPGRADE_INIT_SYSTEM:-}" ]; then
        echo "$HALOY_UPGRADE_INIT_SYSTEM"
    elif [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
        echo "systemd"
    elif [ -f /sbin/openrc-run ] && command -v rc-service >/dev/null 2>&1; then
        echo "openrc"
    elif [ -f "$INIT_D_DIR/haloyd" ]; then
        echo "sysvinit"
    else
        echo "unknown"
    fi
}

# All service helpers take the service name as $1.
service_stop() {
    case "$INIT_SYSTEM" in
        systemd) systemctl stop "$1" ;;
        openrc) rc-service "$1" stop ;;
        sysvinit) "$INIT_D_DIR/$1" stop ;;
        *) return 1 ;;
    esac
}

service_start() {
    case "$INIT_SYSTEM" in
        systemd) systemctl start "$1" ;;
        openrc) rc-service "$1" start ;;
        sysvinit) "$INIT_D_DIR/$1" start ;;
        *) return 1 ;;
    esac
}

service_restart() {
    case "$INIT_SYSTEM" in
        systemd) systemctl restart "$1" ;;
        openrc) rc-service "$1" restart ;;
        sysvinit) "$INIT_D_DIR/$1" restart ;;
        *) return 1 ;;
    esac
}

service_is_active() {
    case "$INIT_SYSTEM" in
        systemd) systemctl is-active --quiet "$1" ;;
        openrc) rc-service "$1" status >/dev/null 2>&1 ;;
        sysvinit) "$INIT_D_DIR/$1" status >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

service_enable() {
    case "$INIT_SYSTEM" in
        systemd) systemctl enable "$1" >/dev/null 2>&1 || true ;;
        openrc) rc-update add "$1" default >/dev/null 2>&1 || true ;;
        sysvinit)
            if command -v update-rc.d >/dev/null 2>&1; then
                update-rc.d "$1" defaults >/dev/null 2>&1 || true
            elif command -v chkconfig >/dev/null 2>&1; then
                chkconfig --add "$1" >/dev/null 2>&1 || true
            fi
            ;;
    esac
}

service_disable() {
    case "$INIT_SYSTEM" in
        systemd) systemctl disable "$1" >/dev/null 2>&1 || true ;;
        openrc) rc-update del "$1" default >/dev/null 2>&1 || true ;;
        sysvinit)
            if command -v update-rc.d >/dev/null 2>&1; then
                update-rc.d -f "$1" remove >/dev/null 2>&1 || true
            elif command -v chkconfig >/dev/null 2>&1; then
                chkconfig --del "$1" >/dev/null 2>&1 || true
            fi
            ;;
    esac
}

file_mode() {
    if stat -c '%a' "$1" >/dev/null 2>&1; then
        stat -c '%a' "$1"
    elif stat -f '%Lp' "$1" >/dev/null 2>&1; then
        stat -f '%Lp' "$1"
    else
        return 1
    fi
}

fetch_release_tag() {
    curl -fsSL -H "Accept: application/json" "$1" 2>/dev/null |
        sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
        head -n 1 || true
}

# set_bind_capability BINARY_PATH
# Binding ports 80/443 needs CAP_NET_BIND_SERVICE on non-systemd installs
# (systemd grants it via AmbientCapabilities in the unit). Normally only the
# proxy needs it; a rolled-back pre-split haloyd needs it too.
set_bind_capability() {
    if [ "$INIT_SYSTEM" = "systemd" ]; then
        return 0
    fi

    if ! command -v setcap >/dev/null 2>&1; then
        echo "setcap is required to grant CAP_NET_BIND_SERVICE on $INIT_SYSTEM installs." >&2
        return 1
    fi

    setcap cap_net_bind_service=+ep "$1"
}

# download_and_verify BINARY_BASENAME MODE
# Downloads the release binary next to the existing install and verifies it by
# running its "version" command. Prints the temp file path on stdout.
download_and_verify() {
    dv_name="$1"
    dv_mode="$2"
    dv_url="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/${dv_name}-${OS}-${ARCH}"
    dv_tmp=$(mktemp "${HALOYD_DIR}/.${dv_name}-upgrade.XXXXXX") || error_exit "Failed to create temp file in $HALOYD_DIR"

    echo "Downloading ${dv_name}-${OS}-${ARCH}..." >&2
    if ! curl -fsSL -o "$dv_tmp" "$dv_url"; then
        rm -f "$dv_tmp"
        error_exit "Failed to download from $dv_url"
    fi

    chmod "$dv_mode" "$dv_tmp" || { rm -f "$dv_tmp"; error_exit "Failed to preserve binary permissions on downloaded file"; }

    echo "Verifying download..." >&2
    dv_version=$("$dv_tmp" version 2>/dev/null | head -n 1 || true)
    if [ -z "$dv_version" ]; then
        rm -f "$dv_tmp"
        error_exit "Downloaded binary failed verification."
    fi

    if [ "$(normalize_version "$dv_version")" != "$NORM_LATEST" ]; then
        rm -f "$dv_tmp"
        error_exit "Downloaded binary version $dv_version does not match release $LATEST_VERSION."
    fi
    echo "Downloaded version: $dv_version" >&2

    echo "$dv_tmp"
}

write_proxy_systemd_unit() {
    cat > "$SYSTEMD_UNIT_DIR/haloy-proxy.service" << EOF
[Unit]
Description=Haloy Proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=haloy
Group=haloy
ExecStart=${PROXY_PATH} serve
Restart=always
RestartSec=5
Environment=HALOY_DATA_DIR=/var/lib/haloy

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/haloy
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
}

write_haloyd_systemd_unit() {
    cat > "$SYSTEMD_UNIT_DIR/haloyd.service" << EOF
[Unit]
Description=Haloy Daemon
After=network-online.target docker.service haloy-proxy.service
Requires=docker.service
Wants=network-online.target haloy-proxy.service

[Service]
Type=simple
User=haloy
Group=haloy
ExecStart=${HALOYD_PATH} serve
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
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
}

write_proxy_openrc_script() {
    cat > "$INIT_D_DIR/haloy-proxy" << EOF
#!/sbin/openrc-run

name="haloy-proxy"
description="Haloy Proxy"
command="${PROXY_PATH}"
command_args="serve"
command_background="yes"
command_user="haloy:haloy"
pidfile="/run/haloy-proxy/haloy-proxy.pid"
output_log="/var/log/haloy-proxy.log"
error_log="/var/log/haloy-proxy.log"

export HALOY_DATA_DIR="/var/lib/haloy"

depend() {
    need net
    after firewall
}

start_pre() {
    checkpath --directory --owner haloy:haloy --mode 0755 /run/haloy-proxy
    checkpath --file --owner haloy:haloy --mode 0644 /var/log/haloy-proxy.log
}
EOF
    chmod +x "$INIT_D_DIR/haloy-proxy"
}

# Migrated installs get the same haloyd init script as fresh installs
# (ordered after haloy-proxy; no bind capability needed anymore).
write_haloyd_openrc_script() {
    cat > "$INIT_D_DIR/haloyd" << EOF
#!/sbin/openrc-run

name="haloyd"
description="Haloy Daemon"
command="${HALOYD_PATH}"
command_args="serve"
command_background="yes"
command_user="haloy:haloy"
pidfile="/run/haloyd/haloyd.pid"
output_log="/var/log/haloyd.log"
error_log="/var/log/haloyd.log"

export HALOY_DATA_DIR="/var/lib/haloy"
export HALOY_CONFIG_DIR="/etc/haloy"

depend() {
    need net docker
    use haloy-proxy
    after firewall haloy-proxy
}

start_pre() {
    checkpath --directory --owner haloy:haloy --mode 0755 /run/haloyd
    checkpath --file --owner haloy:haloy --mode 0644 /var/log/haloyd.log
}
EOF
    chmod +x "$INIT_D_DIR/haloyd"
}

write_proxy_sysvinit_script() {
    cat > "$INIT_D_DIR/haloy-proxy" << EOF
#!/bin/sh
### BEGIN INIT INFO
# Provides:          haloy-proxy
# Required-Start:    \$network \$remote_fs
# Required-Stop:     \$network \$remote_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Haloy Proxy
### END INIT INFO

NAME="haloy-proxy"
DAEMON="${PROXY_PATH}"
DAEMON_ARGS="serve"
PIDFILE="/var/run/haloy-proxy.pid"
LOGFILE="/var/log/haloy-proxy.log"
USER="haloy"

export HALOY_DATA_DIR="/var/lib/haloy"
EOF
    append_sysvinit_runtime "$INIT_D_DIR/haloy-proxy"
}

# Migrated installs get the same haloyd init script as fresh installs
# (ordered after haloy-proxy; no bind capability needed anymore).
write_haloyd_sysvinit_script() {
    cat > "$INIT_D_DIR/haloyd" << EOF
#!/bin/sh
### BEGIN INIT INFO
# Provides:          haloyd
# Required-Start:    \$network \$remote_fs docker haloy-proxy
# Required-Stop:     \$network \$remote_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Haloy Daemon
### END INIT INFO

NAME="haloyd"
DAEMON="${HALOYD_PATH}"
DAEMON_ARGS="serve"
PIDFILE="/var/run/haloyd.pid"
LOGFILE="/var/log/haloyd.log"
USER="haloy"

export HALOY_DATA_DIR="/var/lib/haloy"
export HALOY_CONFIG_DIR="/etc/haloy"
EOF
    append_sysvinit_runtime "$INIT_D_DIR/haloyd"
}

# Shared start/stop/status body of the SysVinit scripts. Quoted heredoc so
# nothing expands at install time.
append_sysvinit_runtime() {
    cat >> "$1" << 'EOF'

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
    chmod +x "$1"
}

# backup_haloyd_service_file PATH
# Saves the pre-migration haloyd service definition so rollback can restore it.
backup_haloyd_service_file() {
    if [ -f "$1" ]; then
        UNIT_RESTORE_PATH="$1"
        UNIT_BACKUP_FILE="$1.backup"
        cp -p "$1" "$UNIT_BACKUP_FILE"
    fi
}

# Installs the haloy-proxy service and rewrites the haloyd service definition:
# haloyd no longer binds 80/443 (no bind capability) and is ordered after
# haloy-proxy with a soft dependency, never a hard one.
install_proxy_service() {
    case "$INIT_SYSTEM" in
        systemd)
            write_proxy_systemd_unit
            backup_haloyd_service_file "$SYSTEMD_UNIT_DIR/haloyd.service"
            write_haloyd_systemd_unit
            systemctl daemon-reload
            ;;
        openrc)
            write_proxy_openrc_script
            backup_haloyd_service_file "$INIT_D_DIR/haloyd"
            write_haloyd_openrc_script
            ;;
        sysvinit)
            write_proxy_sysvinit_script
            backup_haloyd_service_file "$INIT_D_DIR/haloyd"
            write_haloyd_sysvinit_script
            ;;
    esac
}

# remove_proxy_install undoes a partial migration completely, so the next
# upgrade-server.sh run detects the pre-split install again and re-attempts
# the migration. Leaving the proxy binary behind would make the next run
# treat this server as already split and install a haloyd that never binds
# ports 80/443.
remove_proxy_install() {
    rm -f "$PROXY_PATH"
    case "$INIT_SYSTEM" in
        systemd)
            rm -f "$SYSTEMD_UNIT_DIR/haloy-proxy.service"
            systemctl daemon-reload 2>/dev/null || true
            ;;
        openrc | sysvinit)
            rm -f "$INIT_D_DIR/haloy-proxy"
            ;;
    esac
    echo "Removed haloy-proxy binary and service"
}

rollback() {
    echo ""
    echo "Upgrade failed, attempting rollback..."

    if [ "$PROXY_STARTED" = "true" ]; then
        # Old haloyd binds 80/443 itself; the proxy must release them first.
        service_stop "$PROXY_SERVICE_NAME" 2>/dev/null || warn "Failed to stop $PROXY_SERVICE_NAME during rollback"
        service_disable "$PROXY_SERVICE_NAME"
    fi

    if [ "$MODE" = "migrate" ]; then
        remove_proxy_install
    fi

    if [ -n "$UNIT_BACKUP_FILE" ] && [ -f "$UNIT_BACKUP_FILE" ]; then
        if mv -f "$UNIT_BACKUP_FILE" "$UNIT_RESTORE_PATH"; then
            if [ "$INIT_SYSTEM" = "systemd" ]; then
                systemctl daemon-reload 2>/dev/null || true
            fi
            echo "Restored haloyd service definition from backup"
        else
            warn "Failed to restore haloyd service definition"
        fi
    fi

    if [ -n "$BACKUP_FILE" ] && [ -f "$BACKUP_FILE" ]; then
        if mv -f "$BACKUP_FILE" "$TARGET_PATH"; then
            chmod "$CURRENT_MODE" "$TARGET_PATH" 2>/dev/null || true
            if [ "$TARGET_PATH" = "$PROXY_PATH" ]; then
                set_bind_capability "$PROXY_PATH" 2>/dev/null || warn "Failed to restore bind capability during rollback"
            elif [ "$MODE" = "migrate" ]; then
                # The restored pre-split haloyd binds 80/443 itself, and cp -p
                # does not preserve the capability xattr: re-grant it.
                set_bind_capability "$HALOYD_PATH" 2>/dev/null || warn "Failed to restore bind capability during rollback"
            fi
            echo "Restored $TARGET_SERVICE from backup"
        else
            warn "Failed to restore $TARGET_SERVICE from backup"
        fi
    else
        warn "No backup found, cannot restore $TARGET_SERVICE binary"
    fi

    if [ "$SERVICE_STOPPED" = "true" ]; then
        service_restart "$TARGET_SERVICE" || warn "Failed to restart service during rollback"
    fi

    echo "Rollback completed. Please check service status manually."
}

cleanup() {
    if [ -n "$HALOYD_TMP" ]; then
        rm -f "$HALOYD_TMP"
    fi
    if [ -n "$PROXY_TMP" ]; then
        rm -f "$PROXY_TMP"
    fi
}

trap cleanup EXIT

COMPONENT="haloyd"
for arg in "$@"; do
    case "$arg" in
        --component=haloyd) COMPONENT="haloyd" ;;
        --component=proxy) COMPONENT="proxy" ;;
        --component=*) error_exit "Unknown component '${arg#--component=}' (use haloyd or proxy)" ;;
    esac
done

echo "Starting Haloy server upgrade..."

require_root
require_command haloyd
require_command curl
require_command sed
require_command uname
require_command mktemp
require_command dirname
require_command stat

INIT_SYSTEM=$(detect_init_system)
case "$INIT_SYSTEM" in
    systemd | openrc | sysvinit) ;;
    *) error_exit "Could not detect a supported init system for haloyd." ;;
esac
echo "Detected init system: $INIT_SYSTEM"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux) ;;
    *) error_exit "Unsupported OS: $OS. haloyd server upgrades only run on Linux." ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64 | arm64) ARCH="arm64" ;;
    *) error_exit "Unsupported architecture: $ARCH" ;;
esac

HALOYD_PATH=$(command -v haloyd)
HALOYD_DIR=$(dirname "$HALOYD_PATH")
PROXY_PATH="${HALOYD_DIR}/haloy-proxy"

if [ ! -f "$HALOYD_PATH" ]; then
    error_exit "haloyd path is not a regular file: $HALOYD_PATH"
fi

if [ ! -w "$HALOYD_DIR" ]; then
    error_exit "Install directory is not writable: $HALOYD_DIR"
fi

# Pre-split installs have no haloy-proxy: migrate by installing it first.
MODE="$COMPONENT"
if [ ! -f "$PROXY_PATH" ]; then
    MODE="migrate"
fi

echo "Checking for updates..."
LATEST_VERSION=$(fetch_release_tag "https://api.github.com/repos/${GITHUB_REPO}/releases/latest")

if [ -z "$LATEST_VERSION" ]; then
    echo "No stable release found, checking for prereleases..."
    LATEST_VERSION=$(fetch_release_tag "https://api.github.com/repos/${GITHUB_REPO}/releases")
fi

if [ -z "$LATEST_VERSION" ]; then
    error_exit "Could not determine latest version from GitHub."
fi

echo "Latest version: $LATEST_VERSION"
NORM_LATEST=$(normalize_version "$LATEST_VERSION")

case "$MODE" in

haloyd)
    TARGET_SERVICE="$SERVICE_NAME"
    TARGET_PATH="$HALOYD_PATH"
    CURRENT_MODE=$(file_mode "$HALOYD_PATH") || error_exit "Failed to read permissions for $HALOYD_PATH"

    CURRENT_VERSION=$(haloyd version 2>/dev/null | head -n 1 || echo "unknown")
    echo "Current haloyd version: $CURRENT_VERSION"

    if [ "$(normalize_version "$CURRENT_VERSION")" = "$NORM_LATEST" ]; then
        echo "Already running the latest version!"
        exit 0
    fi

    BACKUP_FILE="${HALOYD_PATH}.backup"
    if [ -e "$BACKUP_FILE" ]; then
        error_exit "Backup file already exists: $BACKUP_FILE. Remove it after confirming no upgrade is in progress."
    fi

    HALOYD_TMP=$(download_and_verify haloyd "$CURRENT_MODE")

    echo ""
    echo "Stopping haloyd service (haloy-proxy keeps serving traffic)..."
    if ! service_stop "$SERVICE_NAME"; then
        error_exit "Failed to stop haloyd service."
    fi
    SERVICE_STOPPED=true

    echo "Backing up current binary to ${BACKUP_FILE}"
    if ! cp -p "$HALOYD_PATH" "$BACKUP_FILE"; then
        rollback
        exit 1
    fi

    echo "Installing new binary..."
    if ! mv -f "$HALOYD_TMP" "$HALOYD_PATH"; then
        rollback
        exit 1
    fi
    HALOYD_TMP=""

    if ! chmod "$CURRENT_MODE" "$HALOYD_PATH"; then
        rollback
        exit 1
    fi

    echo ""
    echo "Starting haloyd service..."
    if ! service_start "$SERVICE_NAME"; then
        rollback
        exit 1
    fi
    ;;

proxy)
    TARGET_SERVICE="$PROXY_SERVICE_NAME"
    TARGET_PATH="$PROXY_PATH"
    CURRENT_MODE=$(file_mode "$PROXY_PATH") || error_exit "Failed to read permissions for $PROXY_PATH"

    CURRENT_VERSION=$("$PROXY_PATH" version 2>/dev/null | head -n 1 || echo "unknown")
    echo "Current haloy-proxy version: $CURRENT_VERSION"

    if [ "$(normalize_version "$CURRENT_VERSION")" = "$NORM_LATEST" ]; then
        echo "Already running the latest version!"
        exit 0
    fi

    BACKUP_FILE="${PROXY_PATH}.backup"
    if [ -e "$BACKUP_FILE" ]; then
        error_exit "Backup file already exists: $BACKUP_FILE. Remove it after confirming no upgrade is in progress."
    fi

    PROXY_TMP=$(download_and_verify haloy-proxy "$CURRENT_MODE")

    echo ""
    echo "Stopping haloy-proxy service (traffic pauses briefly)..."
    if ! service_stop "$PROXY_SERVICE_NAME"; then
        error_exit "Failed to stop haloy-proxy service."
    fi
    SERVICE_STOPPED=true

    echo "Backing up current binary to ${BACKUP_FILE}"
    if ! cp -p "$PROXY_PATH" "$BACKUP_FILE"; then
        rollback
        exit 1
    fi

    echo "Installing new binary..."
    if ! mv -f "$PROXY_TMP" "$PROXY_PATH"; then
        rollback
        exit 1
    fi
    PROXY_TMP=""

    if ! chmod "$CURRENT_MODE" "$PROXY_PATH"; then
        rollback
        exit 1
    fi

    if ! set_bind_capability "$PROXY_PATH"; then
        rollback
        exit 1
    fi

    echo ""
    echo "Starting haloy-proxy service..."
    if ! service_start "$PROXY_SERVICE_NAME"; then
        rollback
        exit 1
    fi
    ;;

migrate)
    echo ""
    echo "haloy-proxy is not installed yet: migrating to the split proxy setup."
    echo "Ports 80/443 move from haloyd to haloy-proxy; expect a few seconds"
    echo "of downtime during this one-time migration."

    TARGET_SERVICE="$SERVICE_NAME"
    TARGET_PATH="$HALOYD_PATH"
    CURRENT_MODE=$(file_mode "$HALOYD_PATH") || error_exit "Failed to read permissions for $HALOYD_PATH"

    CURRENT_VERSION=$(haloyd version 2>/dev/null | head -n 1 || echo "unknown")
    echo "Current haloyd version: $CURRENT_VERSION"

    BACKUP_FILE="${HALOYD_PATH}.backup"
    if [ -e "$BACKUP_FILE" ]; then
        error_exit "Backup file already exists: $BACKUP_FILE. Remove it after confirming no upgrade is in progress."
    fi

    # Download and verify BOTH binaries before touching any service.
    HALOYD_TMP=$(download_and_verify haloyd "$CURRENT_MODE")
    PROXY_TMP=$(download_and_verify haloy-proxy "$CURRENT_MODE")

    # Install the proxy binary and service without starting it: the old
    # haloyd still holds ports 80/443.
    echo ""
    echo "Installing haloy-proxy binary and service..."
    if ! mv -f "$PROXY_TMP" "$PROXY_PATH"; then
        error_exit "Failed to install haloy-proxy binary."
    fi
    PROXY_TMP=""
    chmod "$CURRENT_MODE" "$PROXY_PATH" || error_exit "Failed to set permissions on haloy-proxy binary."
    if ! set_bind_capability "$PROXY_PATH"; then
        error_exit "Failed to grant bind capability to haloy-proxy."
    fi
    install_proxy_service

    # Downtime window opens: old haloyd releases 80/443...
    echo ""
    echo "Stopping haloyd service..."
    if ! service_stop "$SERVICE_NAME"; then
        rollback
        exit 1
    fi
    SERVICE_STOPPED=true

    # ...and haloy-proxy re-binds them immediately. It starts with empty
    # routes (no snapshot yet); routes return as soon as haloyd is back up.
    echo "Starting haloy-proxy service..."
    if ! service_start "$PROXY_SERVICE_NAME"; then
        rollback
        exit 1
    fi
    PROXY_STARTED=true
    service_enable "$PROXY_SERVICE_NAME"

    echo "Backing up current binary to ${BACKUP_FILE}"
    if ! cp -p "$HALOYD_PATH" "$BACKUP_FILE"; then
        rollback
        exit 1
    fi

    echo "Installing new haloyd binary..."
    if ! mv -f "$HALOYD_TMP" "$HALOYD_PATH"; then
        rollback
        exit 1
    fi
    HALOYD_TMP=""

    if ! chmod "$CURRENT_MODE" "$HALOYD_PATH"; then
        rollback
        exit 1
    fi

    echo ""
    echo "Starting haloyd service..."
    if ! service_start "$SERVICE_NAME"; then
        rollback
        exit 1
    fi
    ;;
esac

if [ "$SLEEP_SECONDS" != "0" ]; then
    echo "Waiting for service to start..."
    sleep "$SLEEP_SECONDS"
fi

echo ""
echo "Verifying upgrade..."

if [ "$MODE" = "proxy" ]; then
    NEW_VERSION=$("$PROXY_PATH" version 2>/dev/null | head -n 1 || echo "unknown")
    echo "haloy-proxy version: $NEW_VERSION"
else
    NEW_VERSION=$(haloyd version 2>/dev/null | head -n 1 || echo "unknown")
    echo "haloyd version: $NEW_VERSION"
fi

if [ "$(normalize_version "$NEW_VERSION")" != "$NORM_LATEST" ]; then
    rollback
    exit 1
fi

if service_is_active "$TARGET_SERVICE"; then
    echo "Service status: running"
else
    rollback
    exit 1
fi

if [ "$MODE" = "migrate" ]; then
    if service_is_active "$PROXY_SERVICE_NAME"; then
        echo "haloy-proxy status: running"
    else
        rollback
        exit 1
    fi
fi

rm -f "$BACKUP_FILE"
rm -f "$UNIT_BACKUP_FILE"

echo ""
echo "Haloy server upgrade completed successfully!"
if [ "$MODE" = "migrate" ]; then
    echo "Future haloyd upgrades will not interrupt traffic."
    echo "To upgrade the proxy itself later: upgrade-server.sh --component=proxy"
fi
