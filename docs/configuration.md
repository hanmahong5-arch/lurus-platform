# Configuration / 配置参考

All configuration is via environment variables. The core binary validates required variables at startup and fast-fails if any are missing.

## Core / 核心

### Required / 必填

| Variable | Description |
|----------|-------------|
| `DATABASE_DSN` | PostgreSQL connection string (`search_path=identity,billing,public`) |
| `INTERNAL_API_KEY` | Bearer token for `/internal/v1/*` and `/admin/v1/*` endpoints |
| `ZITADEL_ISSUER` | Zitadel issuer URL (e.g. `https://auth.lurus.cn`) |
| `ZITADEL_JWKS_URL` | Zitadel JWKS endpoint for JWT verification |

### Optional / 可选

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `18104` | HTTP listen port |
| `GRPC_PORT` | `18105` | gRPC listen port |
| `ENV` | `production` | `production` for JSON logs, other for text |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | _(empty)_ | Redis auth password |
| `REDIS_DB` | `3` | Redis database number |
| `NATS_URL` | `nats://localhost:4222` | NATS JetStream server |
| `GRACE_PERIOD_DAYS` | `3` | Days of grace after subscription expiry |
| `SESSION_SECRET` | _(auto-generated)_ | HMAC key for session tokens |

### Zitadel / 认证

| Variable | Default | Description |
|----------|---------|-------------|
| `ZITADEL_AUDIENCE` | _(empty)_ | Expected JWT audience claim |
| `ZITADEL_SERVICE_ACCOUNT_PAT` | _(empty)_ | Service account PAT for admin operations |

### Payment Providers / 支付

| Variable | Description |
|----------|-------------|
| `STRIPE_SECRET_KEY` | Stripe API secret key |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook signing secret |
| `CREEM_API_KEY` | Creem API key |
| `CREEM_WEBHOOK_SECRET` | Creem webhook signing secret |
| `EPAY_PID` | Epay merchant ID |
| `EPAY_KEY` | Epay signing key |
| `EPAY_API_URL` | Epay gateway URL |

### Email / 邮件

| Variable | Default | Description |
|----------|---------|-------------|
| `EMAIL_SMTP_HOST` | _(empty)_ | SMTP host (empty = noop sender) |
| `EMAIL_SMTP_PORT` | `587` | SMTP port |
| `EMAIL_SMTP_USER` | _(empty)_ | SMTP auth username |
| `EMAIL_SMTP_PASS` | _(empty)_ | SMTP auth password |
| `EMAIL_FROM` | `noreply@lurus.cn` | Sender address |

### Temporal / 工作流

| Variable | Default | Description |
|----------|---------|-------------|
| `TEMPORAL_HOST_PORT` | `localhost:7233` | Temporal server address |
| `TEMPORAL_NAMESPACE` | `default` | Temporal namespace |

### Observability / 可观测性

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | OTel collector endpoint |

### Module Toggles / 模块开关

| Variable | Default | Description |
|----------|---------|-------------|
| `MODULES_MAIL_ENABLED` | `false` | Enable mail module (Stalwart integration) |
| `MODULES_MAIL_STALWART_ADMIN_URL` | _(empty)_ | Stalwart admin REST API URL |
| `MODULES_NOTIFICATION_ENABLED` | `false` | Enable notification module hooks |
| `MODULES_NOTIFICATION_SERVICE_URL` | _(empty)_ | Notification service base URL |

### WeChat / 微信登录

| Variable | Description |
|----------|-------------|
| `WECHAT_SERVER_ADDRESS` | WeChat OAuth callback server |
| `WECHAT_SERVER_TOKEN` | WeChat API token |

### Rate Limiting / 限流

| Variable | Default | Description |
|----------|---------|-------------|
| `RATE_LIMIT_ENABLED` | `true` | Enable Redis-based rate limiting |
| `RATE_LIMIT_DEFAULT_RPM` | `60` | Default requests per minute per IP |

## Notification Module / 通知模块

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_DSN` | _(required)_ | PostgreSQL DSN (`search_path=notification,public`) |
| `INTERNAL_API_KEY` | _(required)_ | Bearer token for internal/admin endpoints |
| `PORT` | `18900` | HTTP listen port |
| `REDIS_ADDR` | `redis.messaging.svc:6379` | Redis address |
| `REDIS_DB` | `4` | Redis database number |
| `NATS_ADDR` | `nats://nats.messaging.svc:4222` | NATS server |
| `SMTP_HOST` | _(empty)_ | SMTP host (empty = email disabled) |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USER` | _(empty)_ | SMTP username |
| `SMTP_PASS` | _(empty)_ | SMTP password |
| `FCM_CREDENTIALS_PATH` | _(empty)_ | FCM service account JSON path |

## Login UI / 登录界面

| Variable | Description |
|----------|-------------|
| `ZITADEL_API_URL` | Zitadel instance URL |
| `ZITADEL_SERVICE_USER_ID` | Service account user ID |
| `ZITADEL_SERVICE_USER_TOKEN` | Service account PAT |

## Admin Dashboard / 管理后台

| Variable | Description |
|----------|-------------|
| `NEXTAUTH_URL` | Public URL of the admin dashboard |
| `NEXTAUTH_SECRET` | NextAuth session encryption secret |
| `ZITADEL_CLIENT_ID` | OIDC client ID for admin app |
| `ZITADEL_CLIENT_SECRET` | OIDC client secret |
| `IDENTITY_API_URL` | Core REST API base URL |
| `IDENTITY_INTERNAL_KEY` | Core internal API key |
