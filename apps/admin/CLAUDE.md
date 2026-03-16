# lurus-admin

Lurus platform administration dashboard.
Namespace: `lurus-admin` | Port: `3000` | Domain: `admin.lurus.cn`

## Tech Stack

| Layer | Choice |
|-------|--------|
| Framework | Next.js 15 (App Router, standalone output) |
| UI | Tailwind CSS 4, shadcn/ui components, Lucide icons |
| Charts | Recharts |
| Tables | @tanstack/react-table |
| Auth | next-auth v5 + Zitadel OIDC (admin role required) |
| Runtime | Bun (dev), Node.js 22 (production) |

## Commands

```bash
bun install
bun run dev          # dev server (port 3000)
bun run build        # production build
bun run lint
bun run typecheck    # tsc --noEmit
```

## Pages

| Route | Description |
|-------|-------------|
| `/users` | Account search, list, detail, wallet adjustment |
| `/finance` | Financial report with date range, chart, CSV |
| `/subscriptions` | Invoice listing |
| `/strategies` | Strategy marketplace audit (placeholder) |
| `/notifications` | Notification template CRUD |
| `/system` | Monitoring links, embedded Grafana |

## Backend Dependencies

- **lurus-identity** `/admin/v1/*` — accounts, wallet, products, invoices, refunds, settings, organizations
- **lurus-notification** `/admin/v1/templates` — template management

## BMAD

| Resource | Path |
|----------|------|
| PRD | `./_bmad-output/planning-artifacts/prd.md` |
| Epics | `./_bmad-output/planning-artifacts/epics.md` |
