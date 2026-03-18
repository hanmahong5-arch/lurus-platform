# lurus-notification

Unified multi-channel notification hub. Consumes NATS events from identity/lucrum/LLM streams and dispatches via in-app (WebSocket), email (SMTP), and mobile push (FCM placeholder).

Namespace: `lurus-platform` | Port: `18900` | DB schema: `notification`

## Tech Stack

| Layer | Choice |
|-------|--------|
| Backend | Go 1.25, Gin, GORM (pgx driver) |
| DB | PostgreSQL (`search_path=notification,public`), Redis DB 4 |
| Messaging | NATS JetStream (consumes: IDENTITY_EVENTS, LUCRUM_EVENTS, LLM_EVENTS) |
| Email | Stalwart SMTP (`stalwart.mail.svc:587`) |
| Push | FCM (placeholder, activated in Sprint D) |
| Real-time | WebSocket (gorilla/websocket) |

## Directory Structure

```
lurus-notification/
├── cmd/server/main.go          # Entry point, DI wiring, graceful shutdown
├── internal/
│   ├── domain/entity/          # Notification, Template, Preference, DeviceToken
│   ├── app/                    # NotificationService (send/dispatch/template resolution), TemplateService (CRUD)
│   ├── adapter/
│   │   ├── handler/            # Gin handlers + router (BuildRouter), internal notify endpoint
│   │   ├── repo/               # GORM repositories (notification, template, preference)
│   │   ├── nats/               # JetStream consumer (7 subjects across 3 streams)
│   │   └── sender/             # Sender interface + EmailSender, WSSender/Hub, FCMSender (placeholder)
│   └── pkg/
│       ├── config/             # Env-var loader (fast-fail on missing required vars)
│       └── event/              # NATS event type definitions and payload structs
├── migrations/                 # SQL migrations (001 schema, 002 seed templates)
├── deploy/k8s/                 # Kustomize: deployment, configmap, secrets, service, servicemonitor
└── Dockerfile                  # Multi-stage: golang:1.25-alpine -> scratch
```

## API Routes

| Group | Auth | Base Path |
|-------|------|-----------|
| User notifications | `X-Account-ID` header (temp; JWT planned) | `/api/v1/notifications` |
| Internal service-to-service | Bearer `INTERNAL_API_KEY` | `/internal/v1/` |
| Admin templates | Bearer `INTERNAL_API_KEY` | `/admin/v1/` |
| Health | None | `GET /health` |
| Metrics | None | `GET /metrics` |

### User API (`/api/v1/notifications`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/notifications` | List paginated notifications (`?limit=20&offset=0`) |
| GET | `/api/v1/notifications/unread` | Get unread count |
| POST | `/api/v1/notifications/:id/read` | Mark single notification as read |
| POST | `/api/v1/notifications/read-all` | Mark all notifications as read |
| GET | `/api/v1/notifications/ws` | WebSocket upgrade for real-time push |

### Internal API (`/internal/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/internal/v1/notify` | Send notification (body: `account_id`, `event_type`, `channels`, `vars`, `email_addr`) |

### Admin API (`/admin/v1/`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/v1/templates` | List all notification templates |
| POST | `/admin/v1/templates` | Create or update a template (upsert by event_type+channel) |
| DELETE | `/admin/v1/templates/:id` | Delete a template |

### Infrastructure

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check (returns `{"status":"ok"}`) |
| GET | `/metrics` | Prometheus metrics (via `promhttp.Handler()`) |

## NATS Consumers

7 subjects consumed via JetStream queue subscriptions (durable, explicit ack, max 5 retries):

| Stream | Subject | Handler | Channels |
|--------|---------|---------|----------|
| IDENTITY_EVENTS | `identity.account.created` | handleAccountCreated | in_app, email |
| IDENTITY_EVENTS | `identity.subscription.activated` | handleSubscriptionActivated | in_app, email |
| IDENTITY_EVENTS | `identity.subscription.expired` | handleSubscriptionExpired | in_app, email |
| IDENTITY_EVENTS | `identity.topup.completed` | handleTopupCompleted | in_app |
| LUCRUM_EVENTS | `lucrum.strategy.triggered` | handleStrategyTriggered | in_app, fcm |
| LUCRUM_EVENTS | `lucrum.risk.alert` | handleRiskAlert | in_app, email, fcm |
| LLM_EVENTS | `llm.quota.threshold` | handleQuotaThreshold | in_app |

## Environment Variables

### Required (service exits on startup if missing)

| Variable | Description |
|----------|-------------|
| `DATABASE_DSN` | PostgreSQL DSN (`search_path=notification,public`) |
| `INTERNAL_API_KEY` | Bearer token for `/internal/v1/*` and `/admin/v1/*` calls |

### Optional (defaults shown)

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `18900` | HTTP listen port |
| `ENV` | `production` | `production` = JSON log, other = text log |
| `REDIS_ADDR` | `redis.messaging.svc:6379` | Redis address |
| `REDIS_PASSWORD` | _(empty)_ | Redis password |
| `REDIS_DB` | `4` | Redis database number |
| `NATS_ADDR` | `nats://nats.messaging.svc:4222` | NATS JetStream server |
| `SMTP_HOST` | _(empty)_ | SMTP host (empty = email sender disabled, noop fallback) |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USER` | _(empty)_ | SMTP auth username |
| `SMTP_PASS` | _(empty)_ | SMTP auth password |
| `EMAIL_FROM` | `noreply@lurus.cn` | Sender email address |
| `FCM_CREDENTIALS_PATH` | _(empty)_ | Path to FCM service account JSON (empty = FCM disabled) |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown deadline |

## Observability

| Aspect | Status | Details |
|--------|--------|---------|
| Prometheus metrics | Enabled | `GET /metrics` via `promhttp.Handler()`, ServiceMonitor in `deploy/k8s/servicemonitor.yaml` (30s scrape) |
| OpenTelemetry tracing | Not implemented | No OTel dependency or setup; planned for future sprint |
| Structured logging | Enabled | `log/slog` JSON handler in production mode |

## Commands

```bash
go run ./cmd/server
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o app ./cmd/server
go test -v -race ./...

# Migrations
psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/001_notification_schema.sql
psql $DATABASE_DSN -v ON_ERROR_STOP=1 -f migrations/002_seed_templates.sql
```

## BMAD

| Resource | Path |
|----------|------|
| Plan | `../plans/sprint-a-notification.md` |
