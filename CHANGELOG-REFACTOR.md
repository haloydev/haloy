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

## File Statistics

```
41 files changed
1,944 insertions(+)
2,216 deletions(-)
```

Net reduction of ~270 lines while adding significant new functionality.
