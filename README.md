# Haloy
Haloy is a lightweight deployment and orchestration system designed for developers who want a simple, reliable way to deploy Docker‑based applications to their own servers.

## Quickstart

### Prerequisites

- **Server**: Any Linux server with Docker installed
- **Local**: Docker for building your app
- **Domain**: A domain pointing to your server for secure API access

### 1. Install the haloy CLI Tool

The `haloy` CLI tool will trigger deployments from your local machine.

Install `haloy`:

```bash
curl -fsSL https://sh.haloy.dev/install-haloy.sh | bash
```

Ensure `~/.local/bin` is in your PATH by adding the following to your `~/.bashrc`, `~/.zshrc`, or equivalent shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## 2. Install and Initialize haloyd On Your Server
The next step is to install `haloyd` on your server.

Install `haloyadm` (requires sudo access):

```bash
curl -fsSL https://sh.haloy.dev/install-haloyadm.sh | sudo bash
```

Initialize `haloyd` with `haloyadm`:

```bash
sudo haloyadm init --api-domain haloy.yourserver.com --acme-email you@email.com
```

The API domain is required for remote deploys. See [Server Installation](https://haloy.dev/docs/server-installation) for how to set it up without a domain and trigger deploys from the server.

For development or non-root installations, you can install in [user mode](https://haloy.dev/docs/non-root-install).

### 3. Add the Server
Add the server on your local machine:

```bash
haloy server add <server-domain> <api-token>  # e.g., haloy.yourserver.com
```

See [Server Authentication](https://haloy.dev/docs/server-authentication) for more options on how to manage server API tokens.

## 4. Create haloy.yaml
Create a `haloy.yaml` file:

```bash
  name: "my-app"
  server: haloy.yourserver.com
  domains:
    - domain: "my-app.com"
      aliases:
        - "www.my-app.com" # Redirects to my-app.com
```

This will look for a Dockerfile in the same directory as your config file, build it and upload it to the server. This is the Haloy configuration in its simplest form.

Check out the [examples repository](https://github.com/haloydev/examples) for complete configurations showing how to deploy common web apps like Next.js, TanStack Start, static sites, and more.

## 5. Deploy

```bash
  haloy deploy

  # Check status
  haloy status
```

That's it! Your application is now deployed and accessible at your configured domain.

## Learn More
- [Website](https://haloy.dev)
- [What is Haloy?](https://haloy.dev/docs/what-is-haloy)


