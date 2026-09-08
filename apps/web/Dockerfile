# syntax=docker/dockerfile:1.7
#
# Vercel is the primary target for the frontend and builds from source without
# this file. It exists for the VPS fallback: `make deploy-web-vps` runs the
# whole stack, frontend included, behind the same nginx.

FROM node:24-alpine AS base
RUN corepack enable
WORKDIR /repo

# ---- dependencies ---------------------------------------------------------
FROM base AS deps

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json ./apps/web/
COPY packages/shared/package.json ./packages/shared/

# --frozen-lockfile makes a stale lockfile a build failure rather than a silent
# dependency drift.
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --filter @meterrail/web...

# ---- build ----------------------------------------------------------------
FROM base AS build

COPY --from=deps /repo/node_modules ./node_modules
COPY --from=deps /repo/apps/web/node_modules ./apps/web/node_modules
COPY . .

# NEXT_PUBLIC_* values are inlined into the client bundle at build time, so
# they must be present here, not only at runtime.
ARG NEXT_PUBLIC_API_URL
ARG NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID
ARG NEXT_PUBLIC_AGORA_APP_ID
ARG NEXT_PUBLIC_CDN_HOSTNAME
ARG NEXT_PUBLIC_APP_NAME

ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL} \
    NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID=${NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID} \
    NEXT_PUBLIC_AGORA_APP_ID=${NEXT_PUBLIC_AGORA_APP_ID} \
    NEXT_PUBLIC_CDN_HOSTNAME=${NEXT_PUBLIC_CDN_HOSTNAME} \
    NEXT_PUBLIC_APP_NAME=${NEXT_PUBLIC_APP_NAME} \
    NEXT_TELEMETRY_DISABLED=1

RUN pnpm --filter @meterrail/web build

# ---- runtime --------------------------------------------------------------
FROM node:24-alpine AS runtime

WORKDIR /app

RUN addgroup -g 10001 nodejs && adduser -D -u 10001 -G nodejs nextjs

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

# `output: 'standalone'` emits a self-contained server plus only the node_modules
# it actually needs, which is a fraction of the full install.
COPY --from=build --chown=nextjs:nodejs /repo/apps/web/.next/standalone ./
COPY --from=build --chown=nextjs:nodejs /repo/apps/web/.next/static ./apps/web/.next/static
COPY --from=build --chown=nextjs:nodejs /repo/apps/web/public ./apps/web/public

USER nextjs

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/" >/dev/null || exit 1

CMD ["node", "apps/web/server.js"]
