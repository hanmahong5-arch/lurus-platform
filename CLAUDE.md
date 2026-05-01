# lurus-platform

Enterprise Identity, Billing & Communication Platform.
企业身份认证、计费与通信平台。可插拔模块化架构，支持私有化部署。

## Architecture

```
Core (required)          Modules (pluggable)           Apps (optional frontend)
+-----------------+      +-------------------+         +------------------+
| Auth (Zitadel)  |      | Mail (Stalwart)   |         | Login UI         |
| Account CRUD    |<---->| Webmail Frontend   |         | Admin Dashboard  |
| Billing Engine  |      +-------------------+         | Console (future) |
| Wallet / VIP    |      | Notification       |         +------------------+
| Entitlements    |<---->| (WS/Email/FCM)     |
| gRPC + REST API |      +-------------------+
+-----------------+
```

## Tech Stack

| Layer | Choice |
|-------|--------|
| Core Backend | Go (Gin), gRPC, GORM (pgx) |
| Core Frontend | React 18 + TypeScript (embedded SPA) |
| Login UI | Next.js 15 (Zitadel custom login) |
| Admin Dashboard | Next.js 15 + shadcn/ui |
| Webmail | Next.js + Nitro + Stalwart (JMAP) |
| Notification | Go (WebSocket, SMTP, FCM) |
| DB | PostgreSQL (identity + billing schema) |
| Cache / Queue | Redis, NATS JetStream |
| Payment | Stripe, Creem, Epay |
| Auth | Zitadel OIDC + JWKS |
| Deploy | Docker Compose / Kustomize / Helm |

## Directory Structure

```
lurus-platform/
  cmd/
    core/                  # Core binary entry point
  internal/                # Core business logic
    domain/entity/         # Account, Subscription, Wallet, VIP, Product, Invoice, ...
    app/                   # Use-case services
    adapter/               # Handlers, repos, gRPC, NATS, payment providers
    module/                # Module integration layer (mail, notification hooks)
    lifecycle/
    pkg/                   # auth, cache, config, ratelimit, tracing, ...
  migrations/              # SQL migrations (identity + billing schema)
  web/                     # Embedded self-service console (React SPA)
  proto/                   # gRPC contract definitions + generated code
    proto/identity/v1/     # .proto source files
    gen/go/identity/v1/    # Generated Go code
    buf.yaml / buf.gen.yaml
  apps/
    login/                 # Zitadel Login UI (Next.js frontend)
    admin/                 # Admin Dashboard (Next.js frontend)
  modules/
    mail/                  # Email module
      stalwart/            # Stalwart deployment config
      webmail/             # Webmail frontend + worker
    notification/          # Notification module (Go backend)
  deploy/
    docker-compose.yml     # Full deployment
    docker-compose.core.yml # Core only
    k8s/base/              # Core K8s manifests
    k8s/overlays/          # with-mail, with-notification, full
  docs/                    # Product documentation
```

## Commands

```bash
# Build core
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o app ./cmd/core

# Test
go test -v -race ./...

# Proto (requires buf CLI)
cd proto && buf generate && buf lint

# Deploy (core only)
docker-compose -f deploy/docker-compose.core.yml up -d

# Deploy (full)
docker-compose -f deploy/docker-compose.yml up -d
```

## Module Configuration

```yaml
# Core config — modules section
modules:
  mail:
    enabled: true                    # false = mail hooks not registered
    stalwart_admin_url: "http://stalwart:8080"
  notification:
    enabled: true                    # false = notification hooks not registered
    service_url: "http://notification:18900"
```

## Proto Import Path

```go
import identityv1 "github.com/hanmahong5-arch/lurus-platform/proto/gen/go/identity/v1"
```

## BMAD

| Resource | Path |
|----------|------|
| Architecture | `../plans/platform-architecture-v1.md` |

## SMS Relay — Zitadel Webhook → Aliyun SMS

`POST /internal/v1/sms/relay` (bearer `INTERNAL_API_KEY`). Zitadel SMS webhook → Aliyun Dysms.

Env (all in `platform-core-secrets`): `SMS_PROVIDER=aliyun` · `SMS_ALIYUN_{ACCESS_KEY_ID,ACCESS_KEY_SECRET,SIGN_NAME,TEMPLATE_CODE_VERIFY,TEMPLATE_CODE_RESET}`. 空 SMS_PROVIDER → noop sender.

Response codes: 200 sent · 400 invalid recipient/code/E.164 · 401 missing key · 429 Aliyun rate limit (Retry-After: 60s) · 500 retry-exhausted.

Key files: `internal/app/sms/usecase.go` (validation + 3-retry policy), `internal/adapter/handler/sms_relay_handler.go`, `cmd/sms-test/main.go` (E2E CLI).

Status: code complete, 待真实手机号 E2E。

## Internal Subscription Checkout (2026-03-21)

`POST /internal/v1/subscriptions/checkout` (scope: `checkout`)
Body: `{ account_id, product_id, plan_code, billing_cycle, payment_method, return_url }`
- Wallet: debit + activate subscription immediately
- External: create order, return `{ order_no, pay_url }`

`POST /internal/v1/accounts/:id/wallet/transactions` (scope: `wallet:read`)
Query: `page`, `page_size`
Response: `{ data: Transaction[], total: int }`

## Kova Provisioning Bridge (F2 revenue path, 2026-04-30)

Platform→Kova provisioning + per-run usage ingestion. Closes audit-2026-04-30.md F2 H-severity.

**5 endpoints (all live, R6 server stub still TODO)**:

- `POST /api/v1/organizations` (existing) — create org
- `POST /api/v1/subscriptions/checkout` (existing) — kova SKU support reuses the existing wallet/external payment paths
- `POST /internal/v1/orgs/:id/services/kova-tester` (scope `org:provision`) — provision an R6 tester; returns admin key ONCE
- `GET /api/v1/orgs/:id/services/kova` — tenant view; never returns raw key
- `POST /internal/v1/usage/report/kova` (scope `usage:report`) — append-only worker callback

**Dev mode** (`KOVA_PROVISION_BASE_URL` unset):

```bash
# Provision a kova workspace for org 1 — returns synthetic admin_key
curl -X POST http://localhost:18104/internal/v1/orgs/1/services/kova-tester \
  -H "Authorization: Bearer $INTERNAL_API_KEY"
# {"admin_key": "sk-kova-...", "base_url": "http://kova-mock.local", "mock_mode": true, ...}
```

**Prod mode** requires:

- `KOVA_PROVISION_BASE_URL=http://100.122.83.20:9999` (Tailscale)
- `KOVA_PROVISION_API_KEY=<bearer for R6 sidecar>`
- R6 sidecar implementing `POST /internal/provision` (kova repo follow-up — see `doc/coord/contracts.md`)

Schema: `billing.org_services` + `billing.usage_events` (migration 029).

Key files:

- `internal/adapter/kovaprov/client.go` — HTTP client + mock fallback
- `internal/app/kova_provisioning_service.go` — orchestration (idempotent, failure-preserves-key)
- `internal/adapter/handler/kova_provisioning.go` — 3 handlers
- `internal/adapter/repo/org_service.go` — Upsert-on-conflict persistence

## Ops Catalog

Privileged ops enumerated via `internal/module/ops/`. `GET /admin/v1/ops` (admin JWT) returns catalogue: `{type, description, risk_level, destructive, delegate}`. 当前 ops: `approve_refund` / `delete_account` / `delete_oidc_app` (delegate=true via QR-confirmed APP) · `rotate_secret` (delegate=false direct admin).

**Adding a new delegate op** (≤200 LOC e2e): 实装 `QRDelegateExecutor` + 4 metadata methods → 加 `var _ ops.DelegateOp = (*X)(nil)` 编译断言 → 扩 `qr_handler.go` 的 op constants + `QRDelegateParams.Validate()` switch → 新 mint endpoint 调 `qr.CreateDelegateSessionWithParams` → main.go 注册 `qrH.WithDelegateExecutor(exec)` + `opsRegistry.MustRegister(exec)`.

**Non-delegate op**: 仅 `opsRegistry.MustRegister(ops.Info{...})`。

Metrics: `qr_delegate_confirms_total{op,result}` · `qr_confirmed_total{action}` (login/join_org/delegate).
