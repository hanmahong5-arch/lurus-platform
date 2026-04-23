# Platform Dependency Matrix

What env vars / external services each feature requires, and how the platform
degrades when they are missing. The service **boots** iff the "Required at
boot" column is fully satisfied; every other feature is either available or
degraded as noted.

## Required at boot (missing → `log.Fatal`)

| Env var | Source | Used for |
|---------|--------|----------|
| `DATABASE_DSN` | k8s secret `platform-core-secrets` | everything |
| `INTERNAL_API_KEY` | k8s secret | `/internal/v1/*` bearer auth |
| `ZITADEL_ISSUER` | k8s secret | JWT aud + OAuth token endpoint |
| `ZITADEL_JWKS_URL` | k8s secret | JWT signature verification |
| `SESSION_SECRET` (≥ 32 B decoded) | k8s secret | HS256 session tokens |

Validation: `config.Load()` calls `Validate()` which refuses to return a
Config unless all of the above are present and well-formed.

## Optional features (missing → feature degraded, platform still boots)

| Env var(s) | Feature | Degradation |
|-----------|---------|-------------|
| `ZITADEL_SERVICE_ACCOUNT_PAT` | Custom login proxy (`POST /api/v1/auth/login`) | Endpoint returns `503 {"error":"login_unavailable"}` — rest of platform unaffected. Users must use OIDC redirect. |
| `STRIPE_SECRET_KEY` + `STRIPE_WEBHOOK_SECRET` | Stripe checkout + webhook | `/api/v1/wallet/topup` with `payment_method=stripe` returns "Unsupported payment method"; other methods still work. |
| `ALIPAY_APP_ID` + private/public keys | Alipay direct checkout | Falls back to `epay_alipay` via circuit breaker if enabled; otherwise method not listed in `GET /api/v1/wallet/topup/info`. |
| `WECHAT_PAY_MCH_ID` + APIv3 key + private key | WeChat Pay direct | Same fallback/skip pattern as Alipay. |
| `CREEM_API_KEY` + `CREEM_WEBHOOK_SECRET` | Creem checkout | Method hidden from `/wallet/topup/info`. |
| `EPAY_PARTNER_ID` + `EPAY_KEY` | Epay (易支付) | Disabled (it's the fallback provider, so when disabled primary-provider failures cannot auto-recover). |
| `WORLDFIRST_CLIENT_ID` + keys | WorldFirst (万里汇) cross-border | Method hidden. |
| `TEMPORAL_HOST_PORT` | Durable workflow execution | Workflows run on the direct path (no retries, no visibility). Reconciliation still runs but on a goroutine timer. |
| `SMS_PROVIDER` + Aliyun/Tencent creds | SMS verification codes | Phone-based registration / password reset returns error. |
| `EMAIL_SMTP_HOST` + credentials | Email notifications | Welcome / reset-password emails not sent. |
| `WECHAT_SERVER_ADDRESS` + `WECHAT_SERVER_TOKEN` | WeChat OAuth login | `/api/v1/auth/wechat/*` routes return error; other login flows work. |
| `NEWAPI_INTERNAL_URL` + admin token | Quota sync to `newapi` | User entitlements are stored locally but not pushed to the AI gateway. |
| `LURUS_API_INTERNAL_URL` + key | Cross-service calls to `2b-svc-api` | User-service fanout calls skipped. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry tracing | No trace export. |

All optional features log a clear `config: <NAME> not set — <feature> disabled`
line at boot so the deployment's current capability is visible in the first
few lines of pod logs.

## Hard infrastructure dependencies

Some components are not env-gated but assumed to be reachable on the cluster:

| Component | Reachability | Failure mode |
|-----------|--------------|--------------|
| PostgreSQL | `lurus-pg-rw.database.svc:5432` | Startup panic at first DB call. Pod crash-loops until reachable. |
| Redis | `redis.messaging.svc:6379` | Rate limit + dedup disabled on connection error (soft-fail), each request logs. |
| NATS | `nats.messaging.svc:4222` | Cross-service event publishing skipped. |
| Zitadel | `auth.lurus.cn` (public DNS) | JWT verification fails → 401 on authenticated endpoints. Custom login also down. |
| Stalwart (mail) | `stalwart.mail.svc:8080` (optional) | Mail module disabled if `MODULES_MAIL_ENABLED != "true"`. |

## Operational checklist before first production deploy

```bash
# 1. All required secrets set
for k in DATABASE_DSN INTERNAL_API_KEY ZITADEL_ISSUER ZITADEL_JWKS_URL SESSION_SECRET; do
  ssh root@100.98.57.55 "kubectl get secret platform-core-secrets -n lurus-platform \
    -o jsonpath='{.data.$k}' | base64 -d | wc -c" | xargs -I{} echo "$k: {} bytes"
done

# 2. SESSION_SECRET must be ≥ 32 bytes decoded
# 3. All migrations in doc/coord/migration-ledger.md marked APPLIED
# 4. PostgreSQL + Redis + NATS pods Running
# 5. Zitadel responding to JWKS url
curl -sSf https://auth.lurus.cn/oauth/v2/keys | jq '.keys | length'
```

Any WARN line in the first 20 lines of `kubectl logs deploy/platform-core`
should be a deliberate degradation, not a surprise.
