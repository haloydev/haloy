<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./images/haloy-logo-text-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="./images/haloy-logo-text-light.svg">
    <img src="./images/haloy-logo-text-light.svg" alt="Haloy" width="220">
  </picture>
</p>

<p align="center">Turn any VPS into a production-ready app platform in minutes.</p>

<p align="center">Zero-downtime deploys, automatic HTTPS, and instant rollbacks, without complex setup or vendor lock-in.</p>

<p align="center">
  <a href="https://haloy.dev">Website</a> ·
  <a href="https://haloy.dev/docs">Docs</a>
</p>

## ✨ Features

Haloy helps you deploy apps with:

- Fast setup on a fresh VPS
- Automatic HTTPS
- Zero-downtime deploys
- Simple rollbacks
- Preview environments
- No Kubernetes required
- No vendor lock-in

## 🚀 Quickstart

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

SSH into your server and run the install script with your API domain:

```bash
curl -fsSL https://sh.haloy.dev/install-haloyd.sh | API_DOMAIN=haloy.yourserver.com sh
```

**Note:** If you're not logged in as root, use `| sudo sh` instead of `| sh`.

After installation completes, copy the API token from the output and add the server to your local machine:

```bash
haloy server add haloy.yourserver.com <token>
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

## Development

Local builds can embed Git-derived version metadata:

```bash
make build
```

Releases are tag-driven. To cut a release:

```bash
./tools/create-release-tag.sh --next

# Or provide an explicit tag
./tools/create-release-tag.sh v0.1.0-beta.43
```
