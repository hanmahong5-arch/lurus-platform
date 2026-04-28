# Builds the Zitadel login UI for the Lurus platform.
#
# Build args:
#   ZITADEL_REF  — git ref to build from (default: main)
#
# The image is functionally identical to the upstream Zitadel login UI.
# Branding is applied at runtime via Zitadel instance settings (logo/colors)
# and environment variables (theme layout/appearance).
#
# Build is intentionally cached on ARG change so nightly builds track upstream.

ARG ZITADEL_REF=main

# ── Stage 1: Clone & build ────────────────────────────────────────────────────
FROM node:22-alpine AS builder

ARG ZITADEL_REF
ENV NEXT_TELEMETRY_DISABLED=1

RUN apk add --no-cache git

WORKDIR /src

# Sparse checkout to avoid pulling the full monorepo (~600 MB)
RUN git clone \
      --depth 1 \
      --filter=blob:none \
      --sparse \
      --branch "${ZITADEL_REF}" \
      https://github.com/zitadel/zitadel.git . && \
    git sparse-checkout set apps/login

WORKDIR /src/apps/login

# Apply Lurus overrides on top of upstream source.
# overrides/ mirrors the apps/login directory structure; files here
# replace the corresponding upstream files before the build runs.
COPY overrides/ /src/apps/login/

# Install pnpm
RUN corepack enable && corepack prepare pnpm@latest --activate

# Install dependencies (the login app has its own package.json)
RUN pnpm install --frozen-lockfile 2>/dev/null || \
    (cd /src && pnpm install --frozen-lockfile)

# Build Next.js standalone output
ENV NODE_ENV=production
RUN pnpm run build 2>/dev/null || \
    (cd /src && pnpm --filter @zitadel/login build)

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM node:22-alpine AS runner

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME="::"

RUN addgroup --system --gid 1001 nodejs && \
    adduser  --system --uid 1001 nextjs

WORKDIR /app

# Copy standalone build from builder
COPY --from=builder --chown=nextjs:nodejs /src/apps/login/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /src/apps/login/.next/static     ./apps/login/.next/static
COPY --from=builder --chown=nextjs:nodejs /src/apps/login/public           ./apps/login/public

USER nextjs

EXPOSE 3000

CMD ["node", "apps/login/server.js"]
