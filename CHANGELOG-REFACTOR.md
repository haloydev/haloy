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
- SNI-based TLS certificate selection
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

### 5. Other Changes

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
