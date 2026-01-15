# Haloy
Haloy is a lightweight deployment and orchestration system designed for developers who want a simple, reliable way to deploy Docker‑based applications to their own servers.

[Website](https://haloy.dev) · [Docs](https://haloy.dev/docs)

## Features

- **Own your infrastructure** – Deploy to any Linux server you control.
- **Simple config** – One `haloy.yaml` describes your app, domains, and routes.
- **Docker-native** – Builds and deploys from your existing Dockerfile.
- **TLS & domains** – Automated HTTPS via ACME / Let’s Encrypt.
- **Batteries included** – Single CLI for setup, deploy, and status.
- **Secure** - Built-in secret management

## Quickstart

### Prerequisites

- **Server**: Any modern Linux server
- **Local**: Docker for building your app
- **Domain**: A domain or subdomain pointing to your server for secure API access

### 1. Install haloy

**Install script:**

```bash
curl -fsSL https://sh.haloy.dev/install-haloy.sh | sh
```

**Homebrew (macOS / Linux):**

```bash
brew install haloydev/tap/haloy
```

**npm/pnpm/bun**
```bash
npm i -g haloy

pnpm add -g haloy

bun add -g haloy
```

### 2. Server Setup

Use the `server setup` command to provision your server automatically. This command will SSH into your server as `root` (default), install Docker if needed, install `haloyd`, and configure the remote haloyd service via systemd.

```bash
haloy server setup <server-ip> --api-domain haloy.yourserver.com --acme-email you@email.com
```

**Note:** This command requires SSH access to the `root` user on the server. If you don't have this or if you prefer to install manually, check the [Server Installation](https://haloy.dev/docs/server-installation#manual-installation) guide.

### 3. Create haloy.yaml
Create a `haloy.yaml` file:

```yaml
name: "my-app"
server: haloy.yourserver.com
domains:
  - domain: "my-app.com"
    aliases:
      - "www.my-app.com" # Redirects to my-app.com
```

This will look for a Dockerfile in the same directory as your config file, build it and upload it to the server. This is the Haloy configuration in its simplest form.

Check out the [examples repository](https://github.com/haloydev/examples) for complete configurations showing how to deploy common web apps like Next.js, TanStack Start, static sites, and more.

### 4. Deploy

```bash
haloy deploy

# Check status
haloy status
```

That's it! Your application is now deployed and accessible at your configured domain.

## Learn More
- [Configuration Reference](https://haloy.dev/docs/configuration-reference)
- [Commands Reference](https://haloy.dev/docs/commands-reference)
- [Architecture](https://haloy.dev/docs/architecture)
- [Examples Repository](https://github.com/haloydev/examples)
