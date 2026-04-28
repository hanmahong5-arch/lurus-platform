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

# Pinned to a Zitadel release tag instead of `main` to keep CI builds
# reproducible — sparse-checkouts of upstream `main` broke twice in
# April 2026 when the monorepo layout shifted underneath us. Bump this
# manually after verifying the new tag's apps/login still builds with
# our overlay (see overrides/ below).
ARG ZITADEL_REF=v4.14.0

# ── Stage 1: Clone & build ────────────────────────────────────────────────────
FROM node:22-alpine AS builder

ARG ZITADEL_REF
ENV NEXT_TELEMETRY_DISABLED=1

RUN apk add --no-cache git

WORKDIR /src

# Full clone — apps/login is part of a pnpm workspace and pulls in
# `@zitadel/client` + `@zitadel/proto` from /packages/. The earlier
# `git sparse-checkout set apps/login` left those workspace packages
# missing, so `pnpm install` couldn't resolve them and Next.js Turbopack
# logged 96 "Module not found: Can't resolve '@zitadel/client'" errors.
# Trade ~600 MB clone time for a build that actually works.
RUN git clone \
      --depth 1 \
      --branch "${ZITADEL_REF}" \
      https://github.com/zitadel/zitadel.git .

# Apply Lurus overrides on top of upstream source.
# overrides/ mirrors the apps/login directory structure; files here
# replace the corresponding upstream files before the build runs.
COPY overrides/ /src/apps/login/

# Install pnpm
RUN corepack enable && corepack prepare pnpm@latest --activate

# Install dependencies at the workspace root so @zitadel/client +
# @zitadel/proto resolve from /packages/.
RUN pnpm install --frozen-lockfile

# Build the workspace packages @zitadel/login depends on FIRST
# (zitadel-client + zitadel-proto), then the login app itself. The
# packages export `./dist/*.js` from their package.json — without
# their build step, Next.js Turbopack errors with 96 "Module not
# found: Can't resolve '@zitadel/client'" because dist/ is empty.
#
# `...` after the filter is pnpm's "include dependencies" suffix:
# topologically resolves and builds @zitadel/login + every
# transitive workspace dep in order.
ENV NODE_ENV=production
RUN pnpm --filter "@zitadel/login..." build

WORKDIR /src/apps/login

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
