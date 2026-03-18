# lurus-webmail

Self-hosted unified communication platform (email / calendar / contacts).
v2.0 "Stalwart-Native" — migrating from Kurrier fork + Supabase to JMAP-first + Zitadel direct.

**Production URL:** `https://mail.lurus.cn` | **Namespace:** `lurus-platform`

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Frontend | Next.js 16 + React 19, Mantine 8, Tailwind CSS 4, TipTap 3 |
| API / Worker | Nitro (H3), BullMQ, Drizzle ORM |
| Mail Server | Stalwart (JMAP / IMAP / SMTP / CalDAV / CardDAV), RocksDB |
| Auth | Zitadel OIDC PKCE → httpOnly JWT cookie (Supabase Auth removed in v2) |
| Database | PostgreSQL `webmail` schema — app metadata only (mail stored in Stalwart) |
| Cache / Queue | Redis (BullMQ + session cache) |
| Search | Stalwart built-in FTS (Typesense removed in v2) |
| Email Relay | SendCloud SMTP (China domestic delivery) |
| Runtime | Bun (dev/build) → Node 20 Alpine (Docker runtime) |
| Linter | Biome |
| Testing | Vitest + Testing Library |

## Directory Structure

```
lurus-webmail/
├── apps/
│   ├── web/            # Next.js 16 UI — port 3000
│   │   ├── app/
│   │   │   ├── auth/   # OIDC login + callback routes
│   │   │   └── dashboard/(unified)/  # Mail, Calendar, Contacts
│   │   └── Dockerfile
│   ├── worker/         # Nitro API service — port 3001
│   │   ├── server/
│   │   │   ├── routes/api/v1/   # New v2 REST endpoints
│   │   │   ├── routes/api/kurrier/  # Legacy v1 endpoints
│   │   │   └── routes/webdav/   # CalDAV/CardDAV proxy
│   │   └── Dockerfile
│   └── docs/           # Fumadocs documentation site
├── packages/
│   ├── db/             # Drizzle client + migrations (PostgreSQL)
│   ├── schema/         # Drizzle table definitions + Zod validators
│   ├── providers/      # Mail send adapters (SES, SendGrid, Mailgun, SMTP, Postmark)
│   ├── common/         # Shared utils: mail-client, redis-ops, dayjs
│   ├── core/           # Business logic: mail, calendar, contacts, rules
│   ├── api-client/     # Typed fetch client for worker API
│   └── ui/             # Shared UI components
├── db/                 # Local dev DB setup
│   ├── .env.develop    # Dev env (loaded via dotenvx)
│   ├── migrations/     # Drizzle migration SQL files
│   └── docker-compose.dev.yml
├── k8s/                # K8s manifests (kustomize)
│   ├── webmail-app.yaml    # web + worker deployments
│   ├── stalwart/           # Stalwart StatefulSet (ns: mail)
│   ├── ingress.yaml        # Traefik IngressRoute
│   └── kustomization.yaml
└── _bmad-output/       # BMAD planning artifacts
```

## Commands

```bash
# Install
bun install

# Local dev (requires db/.env.develop)
bun run dev              # web:3000 + worker:3001 + docs:3002 concurrently
bun run dev:web          # web only
bun run dev:worker       # worker only

# Build
bun run build            # web (Next.js standalone)
bun run build:worker     # worker (Nitro)

# Test
bun run test             # vitest watch (all workspaces)
bun run test:run         # vitest single run
bun run test:coverage    # coverage report → coverage/

# Lint / Format
bun run lint             # biome check
bun run format           # biome format --write

# DB migrations (run from repo root with dotenvx)
dotenvx run -f db/.env.develop -- bunx drizzle-kit migrate

# K8s — deploy app (lurus-platform namespace)
kubectl apply -k k8s/

# K8s — deploy Stalwart separately (mail namespace)
kubectl apply -k k8s/stalwart/

# K8s — restart pods
ssh root@100.98.57.55 "kubectl rollout restart deployment/webmail-web -n lurus-platform"
ssh root@100.98.57.55 "kubectl rollout restart deployment/webmail-worker -n lurus-platform"

# Docker build (web)
docker build -f apps/web/Dockerfile -t ghcr.io/hanmahong5-arch/lurus-webmail-web:local .
# Docker build (worker)
docker build -f apps/worker/Dockerfile -t ghcr.io/hanmahong5-arch/lurus-webmail-worker:local .
```

## Environment Variables

All vars loaded via `dotenvx run -f db/.env.develop` in dev. Template: `db/example.develop.env`.

| Variable | Used by | Description |
|----------|---------|-------------|
| `WEB_PORT` | web | Next.js listen port (default 3000) |
| `WEB_URL` | web, worker | Public URL (e.g. `https://mail.lurus.cn`) |
| `WORKER_URL` | web | Worker base URL for rewrites (e.g. `http://localhost`) |
| `DATABASE_URL` | web, worker | Postgres superuser DSN |
| `DATABASE_RLS_URL` | web, worker | Postgres RLS client DSN |
| `REDIS_HOST` | web, worker | Redis hostname |
| `REDIS_PORT` | web, worker | Redis port (default 6379) |
| `REDIS_PASSWORD` | web, worker | Redis auth password |
| `ANON_KEY` | web, worker | Supabase anon JWT (legacy v1, may be removed in v2) |
| `SERVICE_ROLE_KEY` | web, worker | Supabase service role JWT (legacy v1) |
| `API_URL` | web | Kong/API base URL (legacy; v2 routes direct to worker) |
| `DAV_URL` | web | CalDAV/CardDAV backend URL |
| `STALWART_API_URL` | worker | Stalwart admin REST API (`http://stalwart.mail.svc.cluster.local:8080`) |
| `STALWART_ADMIN_USER` | worker | Stalwart admin username |
| `STALWART_ADMIN_PASSWORD` | worker | Stalwart admin password (Secret) |
| `STALWART_SMTP_HOST` | worker | Stalwart SMTP hostname |
| `STALWART_SMTP_PORT` | worker | Stalwart SMTP port (587) |
| `SENDCLOUD_HOST` | worker | SendCloud SMTP relay host |
| `SENDCLOUD_PORT` | worker | SendCloud SMTP port (587) |
| `SENDCLOUD_API_USER` | worker | SendCloud API user (Secret) |
| `SENDCLOUD_API_KEY` | worker | SendCloud API key (Secret) |
| `TYPESENSE_*` | web, worker | Search (v1 only; removed in v2) |
| `SEARCH_REBUILD_ON_BOOT` | worker | Rebuild search index on startup (false in prod) |
| `NITRO_PORT` | worker | Nitro listen port (3001) |

## Key Architecture Notes (v2.0)

**Stalwart is the primary data store** — mail, calendar, contacts all live in Stalwart (RocksDB).
PostgreSQL (`webmail` schema) stores only: `user_settings`, `notification_subscriptions`, `audit_log`.

**JMAP flow:**
```
Browser (jmap-jam) → Next.js /api/jmap [adds Stalwart token] → Stalwart :8080
```

**Auth:** Zitadel OIDC PKCE → `/auth/callback` sets httpOnly JWT cookie. No Supabase GoTrue.

**v2.0 removed components:** Kong, GoTrue, PostgREST, Supabase Realtime, Typesense.
Result: 8+ pods → 4 pods (web, worker, redis, stalwart).

**Images:** `ghcr.io/hanmahong5-arch/lurus-webmail-web` and `lurus-webmail-worker`.
GitOps: push to `main` → GitHub Actions builds → GHCR → ArgoCD auto-sync.

## BMAD

| Resource | Path |
|----------|------|
| PRD | `./_bmad-output/planning-artifacts/prd.md` |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` |
| Epics | `./_bmad-output/planning-artifacts/epics.md` |
| Product Brief | `./_bmad-output/planning-artifacts/product-brief.md` |
| Project Context | `./_bmad-output/planning-artifacts/project-context.md` |
| Gap Analysis | `./_bmad-output/planning-artifacts/bmad-gap-analysis.md` |
| Readiness Report | `./_bmad-output/planning-artifacts/readiness-report.md` |
