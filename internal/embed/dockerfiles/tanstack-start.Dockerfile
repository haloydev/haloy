{{- if eq .PackageManager "bun"}}
FROM oven/bun:1 AS base
COPY . /app
WORKDIR /app

FROM base AS prod-deps
RUN bun install --frozen-lockfile --production

FROM base AS build
RUN bun install --frozen-lockfile
RUN bun run build

FROM base
COPY --from=prod-deps /app/node_modules /app/node_modules
COPY --from=build /app/.output /app/.output

ENV PORT={{.Port}}
EXPOSE ${PORT}

# Uncomment and configure once you have a health check endpoint:
# HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
#   CMD bun -e "fetch('http://localhost:${process.env.PORT}/health').then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["bun", "start"]
{{- else if eq .PackageManager "pnpm"}}
FROM node:{{.NodeVersion}}-slim AS base
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
COPY . /app
WORKDIR /app

FROM base AS prod-deps
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --prod --frozen-lockfile

FROM base AS build
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
RUN pnpm run build

FROM base
COPY --from=prod-deps /app/node_modules /app/node_modules
COPY --from=build /app/.output /app/.output

ENV PORT={{.Port}}
EXPOSE ${PORT}

# Uncomment and configure once you have a health check endpoint:
# HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
#   CMD node -e "require('http').get('http://localhost:${process.env.PORT}/health', (r) => process.exit(r.statusCode === 200 ? 0 : 1)).on('error', () => process.exit(1))"

CMD ["pnpm", "start"]
{{- else if eq .PackageManager "yarn"}}
FROM node:{{.NodeVersion}}-slim AS base
RUN corepack enable
COPY . /app
WORKDIR /app

FROM base AS prod-deps
RUN yarn --frozen-lockfile --production

FROM base AS build
RUN yarn --frozen-lockfile
RUN yarn build

FROM base
COPY --from=prod-deps /app/node_modules /app/node_modules
COPY --from=build /app/.output /app/.output

ENV PORT={{.Port}}
EXPOSE ${PORT}

# Uncomment and configure once you have a health check endpoint:
# HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
#   CMD node -e "require('http').get('http://localhost:${process.env.PORT}/health', (r) => process.exit(r.statusCode === 200 ? 0 : 1)).on('error', () => process.exit(1))"

CMD ["yarn", "start"]
{{- else}}
FROM node:{{.NodeVersion}}-slim AS base
COPY . /app
WORKDIR /app

FROM base AS prod-deps
RUN npm ci --omit=dev

FROM base AS build
RUN npm ci
RUN npm run build

FROM base
COPY --from=prod-deps /app/node_modules /app/node_modules
COPY --from=build /app/.output /app/.output

ENV PORT={{.Port}}
EXPOSE ${PORT}

# Uncomment and configure once you have a health check endpoint:
# HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
#   CMD node -e "require('http').get('http://localhost:${process.env.PORT}/health', (r) => process.exit(r.statusCode === 200 ? 0 : 1)).on('error', () => process.exit(1))"

CMD ["npm", "start"]
{{- end}}
