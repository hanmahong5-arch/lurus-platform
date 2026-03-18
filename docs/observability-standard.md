# Lurus Observability Standard / Lurus 可观测性规范

Cross-language observability conventions for all Lurus services.
跨语言可观测性约定，适用于所有 Lurus 服务。

## 1. Metric Naming / 指标命名

All Prometheus metrics MUST follow the pattern `lurus_{service}_{metric_name}`.

| Service | Namespace Prefix | Status |
|---------|-----------------|--------|
| platform (2l-svc-platform) | `lurus_platform_` | Phase 3 migration from `lurus_identity_` |
| api (2b-svc-api) | `lurus_gateway_` | Active |
| notification (notification module) | `lurus_notification_` | Active |
| kova (2b-svc-kova) | `lurus_kova_` | Pending |
| memorus (2b-svc-memorus) | `lurus_memorus_` | Pending |
| lucrum (2c-svc-lucrum) | `lurus_lucrum_` | Pending |

## 2. Standard RED Metrics / 标准 RED 指标

Every service exposing HTTP or gRPC endpoints MUST export these two metrics:

```
lurus_{svc}_http_requests_total{method, route, status}       # Counter
lurus_{svc}_http_request_duration_seconds{method, route}      # Histogram
```

For gRPC services, the `otelgrpc` / `opentelemetry-instrumentation-grpc` interceptors
provide equivalent metrics automatically.

## 3. Trace Conventions / 追踪约定

### Span Naming

Span names follow `{domain}.{operation}` format:

```
wallet.topup
wallet.debit
subscription.activate
refund.request
entitlement.get
webhook.process_order
```

### Standard Span Attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `account.id` | int64 | Lurus account ID |
| `order.no` | string | Payment order number (LO prefix) |
| `refund.no` | string | Refund number (LR prefix) |
| `product.id` | string | Product identifier |
| `payment.provider` | string | epay / stripe / creem |
| `amount.cny` | float64 | Transaction amount in CNY |
| `tx.type` | string | Transaction type (topup / debit / credit) |

### Language SDKs

| Language | Tracing | Metrics |
|----------|---------|---------|
| Go | `go.opentelemetry.io/otel` + `otelgrpc` + `otelgin` | `prometheus/client_golang` |
| Rust | `tracing` + `opentelemetry-rust` + `tracing-opentelemetry` | `metrics` crate + `metrics-exporter-prometheus` |
| Python | `opentelemetry-python` + `opentelemetry-instrumentation-fastapi` | `prometheus_client` |

## 4. Structured Logging / 结构化日志

All services use structured JSON logging with the pattern:

```
slog.Info("domain/operation", "key1", val1, "key2", val2)
```

Key rules:
- Log message = `{domain}/{operation}` (e.g. `wallet/topup`, `refund/approve`)
- Always include: `account_id` (when available), `err` (on failure)
- Financial operations: include `amount_cny`, `order_no`, `balance_after`
- State transitions: include `from_status`, `to_status`

## 5. Alert Conventions / 告警约定

Alert names follow `{Service}{Description}` pattern (PascalCase):

```
PlatformHTTPErrorRateHigh
PlatformPaymentWebhookFailures
APIRelayErrorRateHigh
```

Severity levels:
- `critical`: requires immediate attention (payment failures, SLO breach, service down)
- `warning`: investigate within hours (elevated error rates, resource pressure)
- `info`: informational trends (churn tracking, usage spikes)

## 6. Cardinality Budget / 基数预算

All metric labels MUST be bounded. Unbounded labels (user IDs, request IDs, free-form strings)
are NEVER used as Prometheus labels — they belong in traces and logs.

| Label | Max Cardinality | Source |
|-------|----------------|--------|
| method | 5 (GET/POST/PUT/PATCH/DELETE) | HTTP verb |
| status | ~20 (200/201/400/.../503) | HTTP status code |
| route | ~50 | Registered API routes |
| provider | 3 (epay/stripe/creem) | Payment provider |
| operation | ~5 (topup/debit/credit/freeze/unfreeze) | Wallet operation |
| result | 2-4 (success/error/duplicate/...) | Operation outcome |
| from_status / to_status | ~10 combinations | Order/subscription state machine |

Estimated time series per service: 80-150. Well within Prometheus capacity.
