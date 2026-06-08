Contributions are welcome under the [MIT License](LICENSE). Here's how to get involved.

## Getting Started

Prerequisites: [Go 1.25](https://go.dev/dl/), [Docker](https://docs.docker.com/get-docker/), and [Task](https://taskfile.dev/docs/installation).

```bash
git clone https://github.com/haloydev/haloy.git
cd haloy
task tools
task build
```

Key directories:

- `cmd/haloy` - CLI entrypoint
- `cmd/haloyd` - Server daemon entrypoint
- `internal/` - Shared packages

## Development Workflow

- `task fmt` - format code
- `task test` - run tests
- `task lint` - run linter
- `task vuln` - scan Go code and dependencies for known vulnerabilities
- `task ci-test` - run all CI checks locally (formatting, linting, tests). Run this before pushing.

## Submitting Changes

For larger features or design changes, open an issue first to discuss. This saves effort if the direction doesn't fit the project.

Small bug fixes and improvements can go straight to a PR.

1. Fork the repo and branch from `main`
2. Make your changes
3. Run `task ci-test`
4. Open a PR against `main`

Keep PRs focused: one logical change per PR.

## Commit Messages

Use conventional commit prefixes: `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`

Lowercase, imperative mood. Examples from the commit history:

```
feat: add min_ready_seconds config option for post-healthcheck stabilization
fix: return 502 instead of 404 when container dies after deployment
```

## Reporting Bugs

Open a GitHub issue with:

- Steps to reproduce
- Expected vs actual behavior
- Haloy version (`haloy version`)
- Relevant logs if possible
