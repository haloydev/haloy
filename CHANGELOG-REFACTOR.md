# Major Architecture Refactor

This document describes the significant architectural changes introduced in the `haloyd-proxy` branch.

## Overview

This refactor simplifies the Haloy server architecture by:

1. **Replacing HAProxy with a custom Go-based reverse proxy**
2. **Running haloyd as a native systemd service instead of a Docker container**
3. **Removing haloyadm and merging its functionality into haloyd**

## Changes in Detail

### 1. Custom Reverse Proxy (Replaces HAProxy)

A new `internal/proxy/` package provides a lightweight, purpose-built reverse proxy:

- **`proxy.go`** - Core HTTP/HTTPS reverse proxy with TLS termination and host-based routing
- **`certs.go`** - Certificate manager with hot-reload support via filesystem watching
- **`router.go`** - Route builder for mapping domains to backends
- **`websocket.go`** - WebSocket upgrade handling with bidirectional tunneling

**Key features:**
- Atomic configuration updates without restarts
- SNI-based TLS certificate selection (including alias and one-level wildcard matches)
- Automatic HTTP to HTTPS redirects with canonical domain normalization
- ACME challenge passthrough for Let's Encrypt
- Connection pooling for backend connections
- Structured JSON request logging

**Removed:**
- `internal/haloyd/haproxy.go` - HAProxy configuration generator and container management
- HAProxy Docker container dependency
- HAProxy configuration templates

### 2. Native Systemd Service (No Longer Dockerized)

haloyd now runs as a native binary managed by systemd instead of inside a Docker container.

**New haloyd CLI commands:**

```
haloyd serve      # Start the daemon (replaces running in Docker)
haloyd init       # Initialize directories and configuration
haloyd config     # Get/set configuration values
haloyd upgrade    # Self-update to latest version
haloyd version    # Print version
```

**Benefits:**
- Simpler deployment and debugging
- Direct access to host networking (no Docker networking complexity)
- Easier log management via journalctl
- Faster startup and lower resource overhead

**New files:**
- `cmd/haloyd/main.go` - Expanded with Cobra CLI
- `cmd/haloyd/init.go` - Server initialization (creates directories, generates API token, installs systemd service)
- `cmd/haloyd/config.go` - Configuration management commands
- `cmd/haloyd/upgrade.go` - Self-update functionality

### 3. Removed haloyadm

The `haloyadm` binary has been completely removed. Its functionality has been absorbed into:

- **haloyd subcommands** - For server-side operations (init, config, upgrade)
- **haloy CLI** - For remote server management

**Command mapping (old -> new):**

| Old (haloyadm)           | New (haloyd)                    |
|--------------------------|---------------------------------|
| `haloyadm api token`     | `haloyd config get api-token`   |
| `haloyadm api domain`    | `haloyd config get api-domain`  |
| `haloyadm init`          | `haloyd init`                   |
| `haloyadm start`         | `systemctl start haloyd`        |
| `haloyadm stop`          | `systemctl stop haloyd`         |
| `haloyadm restart`       | `systemctl restart haloyd`      |
| `haloyadm version`       | `haloyd version`                |

**Removed files:**
- `cmd/haloyadm/main.go`
- `internal/haloyadm/` (entire package)
  - `api.go` - API client functionality
  - `init.go` - Initialization logic
  - `permissions.go` - Permission management
  - `restart.go` - Service restart handling
  - `root.go` - CLI root command
  - `services.go` - Service management (HAProxy, haloyd containers)
  - `start.go` / `stop.go` - Service lifecycle
  - `version.go` - Version command

**Removed scripts:**
- `scripts/install-haloyadm.sh` (renamed to `install-haloyd.sh`)
- `scripts/uninstall-haloyadm.sh`

### 4. Updated Server Setup and Upgrade

**Server setup (`internal/haloy/server_setup.go`):**
- Now installs haloyd binary directly instead of pulling Docker images
- Runs `haloyd init` to set up the server

**Server upgrade (`internal/haloy/server_upgrade.go`, `scripts/upgrade-server.sh`):**
- Downloads new haloyd binary directly
- Uses systemd to restart the service
- No longer needs to manage Docker container updates

### 5. Unified Health Check Package

A new `internal/healthcheck/` package provides a centralized, reusable health checking system:

**New files:**
- **`types.go`** - Core types: `Target`, `Result`, `Config`, `TargetState`, and interfaces (`TargetProvider`, `ConfigUpdater`)
- **`checker.go`** - HTTP health checker with single checks, concurrent batch checks (`CheckAll`), and retry-with-backoff (`CheckWithRetry`)
- **`monitor.go`** - `HealthMonitor` for continuous background health checks with configurable interval, fall/rise thresholds
- **`state.go`** - `StateTracker` for tracking health state transitions using fall/rise counters

**Key features:**
- **Fall/Rise thresholds** - Backends are only marked unhealthy after N consecutive failures (fall), and only marked healthy after N consecutive successes (rise). Prevents flapping.
- **Concurrent checks** - `CheckAll` runs health checks in parallel with configurable concurrency limit (default: 10)
- **Configurable via haloyd.yaml** - Enable/disable monitoring, set interval, fall, rise, and timeout values
- **Proxy integration** - `HealthConfigUpdater` bridges the monitor to the proxy, keeping routes active while excluding unhealthy backends

**Configuration example:**
```yaml
health_monitor:
  enabled: true
  interval: "15s"  # Check every 15 seconds
  fall: 3          # Mark unhealthy after 3 failures
  rise: 2          # Mark healthy after 2 successes
  timeout: "5s"    # Per-check timeout
```

**Refactored code:**
- `internal/docker/container.go` - `HealthCheckContainer` now uses the unified `healthcheck.HTTPChecker` with `CheckWithRetry` instead of inline retry logic
- `internal/haloyd/deployments.go` - Added `GetHealthCheckTargets()` method implementing the `TargetProvider` interface
- `internal/haloyd/haloyd.go` - Starts `HealthMonitor` on daemon startup if enabled in config
- `internal/haloyd/health_updater.go` - New file implementing `ConfigUpdater` to rebuild proxy config with healthy backends while keeping routes

### 6. Tunnel Command Improvements

The `haloy tunnel` command now supports defaulting the local port to the target's configured port:

```bash
# Before: local port was required
haloy tunnel 5432 -t postgres

# After: local port is optional, defaults to target's port from haloy.yaml
haloy tunnel -t postgres
```

This reduces user error and makes the common case simpler.

### 7. Other Changes

- **Build scripts updated** (`dev/build-upload-cli-haloyd.sh`) - Builds haloyd as a standalone binary
- **Installer script renamed** - `install-haloyadm.sh` -> `install-haloyd.sh`
- **Uninstall script updated** - Removes haloyd service and binary instead of containers

## Migration Notes

For existing installations:

1. Stop and remove old containers:
   ```bash
   docker stop haloy-haloyd haloy-haproxy
   docker rm haloy-haloyd haloy-haproxy
   ```

2. Install the new haloyd binary:
   ```bash
   curl -fsSL https://haloy.dev/install-haloyd.sh | bash
   ```

3. Initialize the server:
   ```bash
   haloyd init --api-domain api.yourserver.com --acme-email you@example.com
   ```

4. Start the service:
   ```bash
   systemctl enable --now haloyd
   ```

### 8. Installation Flow Redesign

A complete redesign of the server installation experience with improved security, multi-init system support, and better UX.

**Architecture change - separation of concerns:**

| Before | After |
|--------|-------|
| `install-haloyd.sh` downloads binary | `install-haloyd.sh` does everything: creates user, downloads binary, runs init, installs service, starts daemon |
| `haloyd init` creates dirs, config, AND installs systemd | `haloyd init` just creates dirs, config files, Docker network |

**Simplified `haloyd init`:**
- Removed `installSystemdService()` function
- Removed `--no-systemd` and `--local-install` flags
- Removed `config.IsSystemMode()` auto-detection logic
- Added explicit `--data-dir` and `--config-dir` flags for custom paths
- Defaults to `/var/lib/haloy` and `/etc/haloy`

**Security hardening:**
- Dedicated `haloy` system user (added to docker group)
- Systemd sandboxing directives:
  - `NoNewPrivileges=true`
  - `PrivateTmp=true`, `ProtectHome=true`, `ProtectSystem=strict`
  - `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` (allows binding ports 80/443)
  - `AmbientCapabilities=CAP_NET_BIND_SERVICE`
  - `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectControlGroups`
  - `RestrictSUIDSGID=true`, `LimitNOFILE=65536`

**Multi-init system support:**
- Automatic detection of init system (systemd, OpenRC, SysVinit)
- Service templates for all three init systems embedded in install script
- OpenRC: `/etc/init.d/haloyd` with `command_user`, `depend()`, `start_pre()`
- SysVinit: LSB-compliant init script with start/stop/status/restart

**Install script UX improvements (`scripts/install-haloyd.sh`):**
- Step-based progress indicators `[1/8] Detecting system configuration`
- Color-coded output (green checkmarks, yellow warnings, red errors)
- Prerequisite validation (Docker installed, daemon running, disk space)
- Troubleshooting hints on errors
- Environment variable options: `VERSION`, `SKIP_START`, `API_DOMAIN`, `ACME_EMAIL`

**Uninstall script improvements (`scripts/uninstall-server.sh`):**
- Detects what's installed before removing
- Offers backup before data deletion
- Interactive confirmation prompts
- Support for all init systems
- Optional removal of `haloy` user

**New `haloyd verify` command:**
- Diagnostic command to verify installation health
- Checks: config dir, data dir, config files validity, Docker connectivity, Docker network, API health

**Removed code:**
- `config.IsSystemMode()` function
- `constants.EnvVarSystemInstall` environment variable
- `constants.UserDataDir`, `UserConfigDir`, `UserBinDir` paths
- User-mode installation logic throughout

**Changed files:**
- `internal/haloydcli/init.go` - Simplified, removed systemd logic
- `internal/config/paths.go` - Removed IsSystemMode(), simplified to env vars + defaults
- `internal/constants/constants.go` - Removed user-mode constants
- `scripts/install-haloyd.sh` - Complete rewrite
- `scripts/uninstall-server.sh` - Added backup prompts, multi-init support
- `internal/haloydcli/verify.go` - New file
- `internal/haloydcli/root.go` - Added verify command

### 9. Removed `haloy server setup` Command

The `haloy server setup` command has been removed to simplify the CLI. Users now install haloyd directly on their server via SSH.

**Old flow:**
```bash
haloy server setup myserver.com --api-domain api.myserver.com
```

**New flow:**
```bash
# Step 1: SSH to server and run install script
ssh root@myserver 'curl -fsSL https://sh.haloy.dev/install-haloyd.sh | sudo sh'

# Step 2: Copy the API token from output, then locally:
haloy server add https://api.myserver.com <token>
```

**Removed files:**
- `internal/haloy/server_setup.go` - SSH-based remote server provisioning (~170 lines)

### 10. Removed SSH Upgrade Support

The `--use-ssh` flag on `haloy server upgrade` and the entire `sshrunner` package have been removed.

**Before:**
```bash
# SSH-based upgrade (removed)
haloy server upgrade --use-ssh
```

**After:**
```bash
# API-based upgrade (default, still works)
haloy server upgrade

# Manual upgrade instructions
haloy server upgrade --manual
```

**Rationale:**
- The API-based upgrade is the preferred method and works reliably
- The `--manual` flag provides SSH instructions for users who prefer direct access
- Removes SSH client code from the CLI entirely
- Simplifies the codebase and reduces dependencies

**Removed files:**
- `internal/sshrunner/sshrunner.go` - SSH command execution package

**Changed files:**
- `internal/haloy/server_upgrade.go` - Removed `--use-ssh` flag and `performSSHUpgrade()` function

## File Statistics

```
41 files changed
1,944 insertions(+)
2,216 deletions(-)
```

Net reduction of ~270 lines while adding significant new functionality.
