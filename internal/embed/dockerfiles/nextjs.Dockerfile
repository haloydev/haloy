FROM node:{{.NodeVersion}}-alpine AS base

# Install dependencies only when needed
FROM base AS deps
RUN apk add --no-cache libc6-compat
WORKDIR /app

COPY package.json {{.LockFile}} ./
{{- if eq .PackageManager "pnpm"}}
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
RUN --mount=type=cache,id=pnpm,target=/pnpm/store {{.InstallCmd}}
{{- else if eq .PackageManager "yarn"}}
RUN corepack enable
RUN {{.InstallCmd}}
{{- else if eq .PackageManager "bun"}}
RUN npm install -g bun
RUN {{.InstallCmd}}
{{- else}}
RUN {{.InstallCmd}}
{{- end}}

# Rebuild the source code only when needed
FROM base AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

{{- if eq .PackageManager "pnpm"}}
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
{{- else if eq .PackageManager "yarn"}}
RUN corepack enable
{{- else if eq .PackageManager "bun"}}
RUN npm install -g bun
{{- end}}

ENV NEXT_TELEMETRY_DISABLED=1
RUN {{.BuildCmd}}

# Production image, copy all the files and run next
FROM base AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

ENV PORT={{.Port}}
EXPOSE ${PORT}

# Uncomment and configure once you have a health check endpoint:
# HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
#   CMD node -e "require('http').get('http://localhost:${PORT}/health', (r) => process.exit(r.statusCode === 200 ? 0 : 1)).on('error', () => process.exit(1))"

ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
