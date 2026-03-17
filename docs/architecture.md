# Architecture / 架构

## Overview / 概览

Lurus Platform is a modular identity, billing, and communication platform.
The core provides authentication (via Zitadel OIDC) and a full billing engine (wallet, subscriptions, VIP tiers, entitlements).
Optional modules add enterprise email (Stalwart) and multi-channel notifications (WebSocket/Email/FCM).

```
                         Consumers
                    (lurus-api, gushen, ...)
                           |
              REST :18104  |  gRPC :18105
                    +------+------+
                    |   Core      |
                    |  (Go/Gin)   |
                    +------+------+
                           |
         +---------+-------+--------+---------+
         |         |                |         |
     PostgreSQL  Redis           NATS     Zitadel
    (identity+   (cache,        (events)   (OIDC)
     billing)    entitlements)
         |
  +------+------+
  |             |
  Mail Module   Notification Module
  (Stalwart +   (WebSocket, Email,
   Webmail)      FCM)
```

## Core / 核心

The core binary (`cmd/core/`) provides:

| Feature | Description |
|---------|-------------|
| Account CRUD | Create, read, update accounts with Zitadel sub binding |
| Billing Engine | Wallet (topup/debit), subscriptions, products & plans |
| VIP Tiers | Silver/Gold/Diamond with discount rates |
| Entitlements | Redis-cached feature flags per account (5min TTL) |
| Payment Providers | Stripe, Creem, Epay with webhook signature verification |
| gRPC API | High-performance service-to-service communication |
| REST API | Public, internal, admin, and webhook endpoints |
| Temporal Workflows | Subscription lifecycle, auto-renewal, payment completion |

### Layered Architecture / 分层架构

```
cmd/core/main.go          Entry point, DI wiring
internal/
  domain/entity/           Domain structs (Account, Subscription, Wallet, ...)
  app/                     Use-case services (business logic)
  adapter/
    handler/               REST handlers (Gin)
    repo/                  GORM repositories (PostgreSQL)
    grpc/                  gRPC server implementation
    nats/                  NATS event publisher
    payment/               Payment provider adapters
  module/                  Pluggable module registry
  lifecycle/               App startup/shutdown lifecycle
  pkg/                     Shared utilities (auth, cache, config, ...)
  temporal/                Temporal workflows and activities
```

### Temporal Workflows / 工作流

All recurring operations use Temporal instead of cron jobs:

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| SubscriptionLifecycle | New subscription created | Manages reminders, expiry, grace period |
| SubscriptionRenewal | Auto-renew enabled at expiry | Saga: debit wallet, activate, compensate on failure |
| PaymentCompletion | Webhook from payment provider | Idempotent order fulfillment |
| ExpiryScanner | Temporal Cron (hourly) | Catches pre-Temporal subscriptions |

## Module System / 模块系统

Modules are pluggable via the `ModuleRegistry` in `internal/module/`:

```go
registry := module.NewRegistry()

if cfg.Modules.Mail.Enabled {
    mailMod := module.NewMailModule(cfg.Modules.Mail)
    registry.OnAccountCreated(mailMod.OnAccountCreated)
}
```

- Disabled modules = zero overhead (hooks not registered)
- Hook failures are logged but don't block core operations (graceful degradation)

### Mail Module

- **Stalwart** — JMAP/IMAP/SMTP mail server with RocksDB storage
- **Webmail** — Next.js frontend + Nitro API worker
- Hooks: account created → provision mailbox, plan changed → adjust quota

### Notification Module

- Independent Go binary (`modules/notification/`)
- Consumes NATS events from 3 streams (IDENTITY/GUSHEN/LLM_EVENTS)
- Dispatches via WebSocket (real-time), Email (SMTP), FCM (mobile push)

## Data Model / 数据模型

### PostgreSQL Schemas

| Schema | Tables | Owner |
|--------|--------|-------|
| `identity` | accounts, oauth_bindings, organizations, org_members, org_api_keys, admin_settings | Core |
| `billing` | wallets, transactions, subscriptions, products, product_plans, topup_orders, invoices, refunds, redeem_codes, referrals, checkin_logs, org_wallets | Core |
| `notification` | notifications, templates, preferences, device_tokens | Notification Module |
| `webmail` | user_settings, notification_subscriptions, audit_log | Webmail |

### Redis DB Allocation

| DB | Service |
|----|---------|
| 0 | lurus-api |
| 1 | gushen |
| 2 | rate limiting |
| 3 | lurus-platform (entitlements cache) |
| 4 | notification |

## Security / 安全

- **JWKS JWT verification** — Zitadel-issued tokens validated via JWKS endpoint
- **Internal API** — Bearer token (`INTERNAL_API_KEY`) for service-to-service
- **Webhook signatures** — Stripe (HMAC-SHA256), Creem (HMAC-SHA256), Epay (MD5 sign)
- **Rate limiting** — Configurable per-endpoint via Redis sliding window
- **Idempotency** — Payment webhooks deduplicated via Redis + Temporal workflow ID

## Deployment / 部署

### Container Images

| Component | Image |
|-----------|-------|
| Core | `ghcr.io/hanmahong5-arch/lurus-platform-core` |
| Notification | `ghcr.io/hanmahong5-arch/lurus-platform-notification` |
| Login UI | `ghcr.io/hanmahong5-arch/lurus-platform-login` |
| Admin | `ghcr.io/hanmahong5-arch/lurus-platform-admin` |
| Webmail Web | `ghcr.io/hanmahong5-arch/lurus-platform-webmail-web` |
| Webmail Worker | `ghcr.io/hanmahong5-arch/lurus-platform-webmail-worker` |

### CI/CD Pipeline

```
Push to master → GitHub Actions (lint + test + build) → GHCR → ArgoCD auto-sync → K8s
```

Each component has its own workflow with path-based triggers — only changed components rebuild.
