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

SSH into your server and run the install script:

```bash
ssh root@yourserver.com
curl -fsSL https://sh.haloy.dev/install-haloyd.sh | sh
```

The script will install Docker (if needed), set up haloyd, and display an API token. Copy the token and register the server locally:

```bash
haloy server add https://haloy.yourserver.com <token>
```

For detailed options, see the [Server Installation](https://haloy.dev/docs/server-installation) guide.

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
