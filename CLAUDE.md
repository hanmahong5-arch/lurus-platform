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

## SMS Relay — Zitadel Webhook → Aliyun SMS (2026-04-25)

Endpoint: `POST /internal/v1/sms/relay` (requires `INTERNAL_API_KEY` bearer auth)

**Flow**: Zitadel fires an SMS webhook → platform relays to Aliyun Dysms API.

### Zitadel Webhook Payload

```json
{
  "contextInfo": {
    "eventType": "user.notification.otp.sms",
    "recipient": "+8613800138000"
  },
  "templateData": {
    "code": "382910",
    "minutes": "5"
  },
  "messageContent": "您的验证码是382910，5分钟内有效。"
}
```

One-time prereq: register the webhook in Zitadel with `returnCode: true` to inspect the live payload format and confirm field names match the struct in `sms_relay_handler.go`.

### Configuration

All environment variables are read from K8s secret `platform-core-secrets`:

| Env Var | Description |
|---------|-------------|
| `SMS_PROVIDER` | `aliyun` (or empty = noop) |
| `SMS_ALIYUN_ACCESS_KEY_ID` | Aliyun AccessKey ID |
| `SMS_ALIYUN_ACCESS_KEY_SECRET` | Aliyun AccessKey Secret |
| `SMS_ALIYUN_SIGN_NAME` | Aliyun SMS sign name (审核通过的签名) |
| `SMS_ALIYUN_TEMPLATE_CODE_VERIFY` | Template code for OTP (e.g. SMS_xxxxxxxx) |
| `SMS_ALIYUN_TEMPLATE_CODE_RESET` | Template code for password reset |

### Response codes

| Code | Meaning |
|------|---------|
| 200 | SMS sent (or accepted by noop sender) |
| 400 | Missing recipient, missing code, or invalid E.164 phone |
| 401 | Missing/invalid INTERNAL_API_KEY |
| 429 | Aliyun rate limit — retry after `Retry-After: 60` seconds |
| 500 | Transient provider failure after 3 retries |

### Key files

- `internal/app/sms/usecase.go` — `SMSRelayUsecase`: phone validation, retry policy (max 3), rate-limit detection
- `internal/adapter/handler/sms_relay_handler.go` — Gin handler
- `cmd/sms-test/main.go` — one-shot CLI to verify SMS delivery from the command line

### E2E verification (needs real phone)

```bash
# Run locally with prod creds loaded
go run ./cmd/sms-test -phone +86<your_number> -code 654321

# Or from inside the cluster
kubectl exec -n lurus-platform deploy/platform-core -- \
  /bin/sh -c 'SMS_PROVIDER=aliyun SMS_ALIYUN_ACCESS_KEY_ID=$SMS_ALIYUN_ACCESS_KEY_ID \
  SMS_ALIYUN_ACCESS_KEY_SECRET=$SMS_ALIYUN_ACCESS_KEY_SECRET \
  SMS_ALIYUN_SIGN_NAME=$SMS_ALIYUN_SIGN_NAME \
  SMS_ALIYUN_TEMPLATE_CODE_VERIFY=$SMS_ALIYUN_TEMPLATE_CODE_VERIFY \
  /sms-test -phone +86<your_number> -code 654321'

# In-cluster HTTP test
curl -s -H "Authorization: Bearer $INTERNAL_API_KEY" \
  -H "Content-Type: application/json" \
  -X POST http://platform-core.lurus-platform.svc:18104/internal/v1/sms/relay \
  -d '{"contextInfo":{"recipient":"+86<your_number>"},"templateData":{"code":"123456"}}'
```

Status: code complete — needs E2E SMS verification with a real test phone number.

## Internal Subscription Checkout (2026-03-21)

`POST /internal/v1/subscriptions/checkout` (scope: `checkout`)
Body: `{ account_id, product_id, plan_code, billing_cycle, payment_method, return_url }`
- Wallet: debit + activate subscription immediately
- External: create order, return `{ order_no, pay_url }`

`POST /internal/v1/accounts/:id/wallet/transactions` (scope: `wallet:read`)
Query: `page`, `page_size`
Response: `{ data: Transaction[], total: int }`

## Ops Catalog (Phase 4 / 2026-04-25)

Privileged operations the platform exposes are enumerated through `internal/module/ops/`.

`GET /admin/v1/ops` (admin JWT required) returns the full catalogue:
```json
{"ops": [
  {"type": "approve_refund",  "description": "...", "risk_level": "warn",        "destructive": false, "delegate": true},
  {"type": "delete_account",  "description": "...", "risk_level": "destructive", "destructive": true,  "delegate": true},
  {"type": "delete_oidc_app", "description": "...", "risk_level": "destructive", "destructive": true,  "delegate": true},
  {"type": "rotate_secret",   "description": "...", "risk_level": "warn",        "destructive": false, "delegate": false}
]}
```

`delegate: true` ops run on the QR-confirmed APP path; `delegate: false` ops are direct admin actions.

### Adding a new delegate op (≤200 LOC end-to-end)
1. New executor implementing `QRDelegateExecutor` (ExecuteDelegate + SupportedOps) AND four metadata methods (`Type`/`Description`/`RiskLevel`/`IsDestructive`).
2. Add `var _ ops.DelegateOp = (*MyExec)(nil)` compile-time assertion.
3. Add op constant to `qr_handler.go` + extend `QRDelegateParams.Validate()` switch.
4. Add a mint endpoint that calls `qr.CreateDelegateSessionWithParams(ctx, callerID, params)`.
5. In `cmd/core/main.go`: `qrH = qrH.WithDelegateExecutor(exec); opsRegistry.MustRegister(exec)`.

### Adding a non-delegate op (admin direct action, no QR)
Just `opsRegistry.MustRegister(ops.Info{OpType: "x", OpDescription: "...", OpRisk: ops.RiskWarn})`. Catalog will mark `delegate: false`.

### Metrics
- `qr_delegate_confirms_total{op,result}` — per-op success/failed counter, drives ops dashboards
- `qr_confirmed_total{action}` — pre-existing, action ∈ {login, join_org, delegate}
