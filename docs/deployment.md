# Deployment Guide

This document covers what you need to run `lurus-platform` (the Core binary)
in production, dev, or self-hosted. For the architectural overview see
`architecture.md`; for per-dependency minimums and rationale see
`infra-requirements.md`.

## 1. Minimum infrastructure

| Dependency  | Minimum version | Required? | Notes |
|-------------|-----------------|-----------|-------|
| PostgreSQL  | 15              | Yes       | Row-Level Security is mandatory — 15+ features used by migrations |
| Redis       | 6.0             | Yes       | Keyspace: DB 3 (platform) + DB 4 (notification module) |
| NATS        | 2.10            | Recommended | Event publish/consume — core degrades gracefully if unreachable |
| Zitadel     | 2.60+           | Yes       | OIDC issuer + JWKS source for JWT validation |
| Go          | 1.25            | Build only | `CGO_ENABLED=0` required for static scratch/alpine images |
| Temporal    | 1.22+           | Optional  | Without it, subscription workflows run in-process (no durability) |

Platform-core exposes:

- HTTP API on `18104` (default)
- gRPC API on `18105` (default)
- Metrics scrape endpoint on the same HTTP port at `/metrics`

## 2. Required environment variables

Missing any of these at boot causes `panic` (`requireEnv`) or `Validate()`
rejection — **fast-fail is intentional**, don't try to paper over it.

| Name | Format | Constraint |
|------|--------|-----------|
| `DATABASE_DSN` | `host=... user=... dbname=... sslmode=...` (pgx/GORM) | Must reach a Postgres with the `identity` + `billing` schemas migrated to head **016** |
| `ZITADEL_ISSUER` | `https://auth.example.com` | Exact issuer string in JWT `iss` claim |
| `ZITADEL_JWKS_URL` | `https://auth.example.com/oauth/v2/keys` | Must be fetchable at boot; cached for 1h thereafter |
| `INTERNAL_API_KEY` | Arbitrary opaque string, ≥ 32 chars | Used as legacy shared secret for `/internal/v1/*`; scoped `ServiceKeyStore` supersedes per-service |
| `SESSION_SECRET` | base64 or raw, **decoded length ≥ 32 bytes** | Signs `lurus` session JWTs; generate with `openssl rand -base64 32` |

## 3. Optional environment variables

These widen the deployment surface; defaults match the "platform only, no
external integrations" mode.

| Name | Default | Purpose |
|------|---------|---------|
| `PORT` | `18104` | HTTP listen port |
| `GRPC_PORT` | `18105` | gRPC listen port |
| `ENV` | `production` | `production` enables JSON log handler; anything else uses text |
| `REDIS_ADDR` | `redis.messaging.svc:6379` | Redis endpoint |
| `REDIS_PASSWORD` | (empty) | Redis auth |
| `REDIS_DB` | `3` | Keyspace index (default is DB 3 per lurus.yaml allocation) |
| `NATS_ADDR` | `nats://nats.messaging.svc:4222` | Event broker |
| `ZITADEL_AUDIENCE` | (empty) | Optional `aud` claim enforcement |
| `ZITADEL_ADMIN_ROLE` | `admin` | Zitadel role mapped to `/admin/v1/*` access |
| `QR_SIGNING_KEYS` | (empty) | HMAC keyring for QR v2 payloads: `kid:hex32[,kid:hex32...]`. Empty → fall back to single-key `SESSION_SECRET` |
| `TRUSTED_PROXIES` | `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1/32,100.64.0.0/10` | CIDRs allowed to set `X-Forwarded-For`. Covers K8s / Docker / CGNAT |
| `RATE_LIMIT_IP_PER_MINUTE` | `120` | Per-IP rate limit |
| `RATE_LIMIT_USER_PER_MINUTE` | `300` | Per-account rate limit |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown ceiling |
| `TEMPORAL_HOST_PORT` | (empty) | Enable Temporal worker for durable workflows |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | OTel collector — omit to disable tracing |
| `STALWART_ADMIN_URL` | (empty) | Enable mail module |
| `STALWART_ADMIN_PASSWORD` | (empty) | Required whenever `STALWART_ADMIN_URL` is set |

Payment providers (Alipay, WeChat Pay, Stripe, Creem, WorldFirst) and
SMS providers all follow the same "leave empty to disable" pattern —
see `configuration.md` for the full set.

## 4. Deployment modes

### 4.1 Docker Compose (dev / small prod)

```bash
# Core only — no mail, no notification
docker-compose -f deploy/docker-compose.core.yml up -d

# Full — bundles Stalwart + notification service
docker-compose -f deploy/docker-compose.yml up -d
```

Compose files live in `deploy/`. They expect an `.env` file in the same
directory with the required env vars set. Use `.env.example` (if present)
as a starting point.

### 4.2 Kubernetes with Kustomize

Manifests live under `deploy/k8s/`:

```
deploy/k8s/
  base/                     # Core only (minimal deployment)
  overlays/with-mail/       # Core + Stalwart mail module
  overlays/with-notification/
  overlays/full/            # Core + mail + notification
```

Apply an overlay:

```bash
kubectl apply -k deploy/k8s/overlays/full
```

Secrets (`platform-core-secrets`) are managed out-of-band — the manifests
reference keys but never ship values. Populate them with `kubectl create
secret generic ...` or SealedSecrets/ExternalSecrets before first apply.

## 5. Minimum resource recommendation

Per-pod baseline (from `deploy/k8s/base/deployment.yaml`):

```
requests:  memory 128Mi, cpu 50m
limits:    memory 512Mi, cpu 300m
```

These sizes support ~500 concurrent QR login sessions per pod. Scale
horizontally (bump `replicas` or let the HPA do it) before bumping
limits — Go's scheduler and GORM's connection pool are already sized
for the request shape, and a vertical scale past 1Gi gives diminishing
returns.

Database connection pool is capped at 25 open / 5 idle; budget the
Postgres `max_connections` accordingly when running many replicas.

## 6. Observability

- **Liveness**: `GET /health` → always `200 {"status":"ok"}` while the
  process is serving. Does not hit any dependency — it is explicitly a
  "the Go binary is up" answer. Wire Kubernetes `livenessProbe` here.
- **Readiness**: `GET /readyz` → `200 {"ready": true}` when Redis,
  Postgres, and NATS all respond within 2 s. On failure responds
  `503 {"ready": false, "failures": [{"name":"redis","err":"..."}]}`.
  Wire Kubernetes `readinessProbe` here so dependency outages pull
  the pod out of the Service endpoints without restarting it.
- **Metrics**: `GET /metrics` → Prometheus exposition. ServiceMonitor
  example in `deploy/k8s/base/servicemonitor.yaml`.
- **Traces**: set `OTEL_EXPORTER_OTLP_ENDPOINT`; spans ship via OTLP.
  Gin requests are auto-instrumented by `otelgin`.

Example Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: lurus-platform
    metrics_path: /metrics
    static_configs:
      - targets: ["platform-core.lurus-platform.svc:18104"]
```

## 7. Image tags + emergency rollback

### Image tag strategy

CI (see `.github/workflows/core.yaml`) pushes two tags for every master push:

| Tag | Mutable? | Intended consumer | Notes |
|-----|----------|-------------------|-------|
| `:main` | **Yes** | ArgoCD auto-sync | Always points at the newest master build. `deploy/k8s/base/deployment.yaml` references this tag. |
| `:main-<sha7>` | **No** | Emergency rollback / forensic debugging | The immutable anchor (Track L / RL2.2). Pin this to lock a specific revision. |

At container start the binary logs its own build SHA via the
`internal/pkg/buildinfo` package:

```
{"time":"...","level":"INFO","msg":"lurus-platform build","sha":"a3f8c12","built_at":"2026-04-24T07:22:11Z"}
```

Use this log line to verify which revision actually booted, independent of
the ArgoCD-applied tag.

### Rolling back

If a freshly deployed revision is misbehaving:

1. Identify the last-known-good SHA from Git history or from the
   `lurus-platform build` log line in Loki/Grafana.
2. Update the Kustomize image tag (never use `kubectl set image` — ArgoCD
   auto-sync overwrites direct changes):
   ```bash
   cd deploy/k8s/overlays/full
   kustomize edit set image ghcr.io/hanmahong5-arch/lurus-platform-core=ghcr.io/hanmahong5-arch/lurus-platform-core:main-<last-good-sha7>
   git commit -am "rollback: pin platform-core to <sha7>"
   git push
   ```
3. Trigger ArgoCD sync (or wait for auto-sync).
4. Once the production fire is out, revert the pin back to `:main` in a
   follow-up PR after a fix-forward has been verified in staging.

Never force-push to master.

### Verifying a build artefact

```bash
docker pull ghcr.io/hanmahong5-arch/lurus-platform-core:main-<sha7>
docker inspect ghcr.io/hanmahong5-arch/lurus-platform-core:main-<sha7> | jq '.[0].RepoDigests'
```

Two tags pointing at the same digest are byte-identical. Pin by digest
(`@sha256:...`) in the manifest for absolute paranoia.

## 8. Common gotchas

**`TRUSTED_PROXIES` and `X-Forwarded-For`** — Gin ignores
`X-Forwarded-For` headers unless the peer IP falls inside a trusted
CIDR. The default list covers K8s (10/8), Docker bridge (172.16/12),
private VLANs (192.168/16), loopback, and Tailscale CGNAT (100.64/10).
Running platform-core behind a proxy outside those ranges (e.g. a
custom cloud NAT) means `c.ClientIP()` will report the *proxy* IP, not
the client, and per-IP rate limiting will bucket every request into one
noisy neighbour.

**QR signing key rotation** — `QR_SIGNING_KEYS` takes an ordered list
(`kid:hex32[,...]`). Signing uses the highest kid; verification accepts
any key in the ring. Rotation playbook:

1. Append a new entry (e.g. `new2026:<hex32>`) after the existing keys.
2. Deploy. New QRs are now signed with `new2026`.
3. Wait ≥ `qrDefaultTTL` (5 min) for any outstanding QRs signed with
   the old key to drain.
4. Remove the old entry. Deploy. Rotation complete.

Skipping step 3 will invalidate in-flight login sessions and produce a
spike of `invalid_signature` errors for the next five minutes.

**`platform-core-secrets` ArgoCD accident** — `ignoreDifferences` does
NOT prevent ArgoCD from overwriting a Secret during sync. If you must
edit `platform-core-secrets` outside git (bootstrap, key rotation),
immediately reconcile the change into the Secret manifest in the repo
or the next sync will wipe it. There was an incident where
`DATABASE_DSN / INTERNAL_API_KEY / SESSION_SECRET` were all zeroed and
new pods panicked — recovery required `crictl inspect` on a live
container to extract the env. See `.claude/skills/infra-ops/SKILL.md`
§6.26 for the full postmortem.

**Migrations pinned at 025** — new deployments must run migrations
(`migrate -path migrations -database $DATABASE_DSN up`) before the
first pod starts, or GORM will fail on missing columns. The deployment
`initContainer` does this automatically under Kustomize; bare Compose
users must run it manually.

Phase 4 critical migrations:
- `024_account_purges.sql` — required by Sprint 1A `delete_account` flow.
  Without it, `POST /admin/v1/accounts/:id/delete-request` will 500 on
  the first call (table missing). Idempotent — safe to re-run.
- `025_seed_tally_product.sql` — seeds the lurus-tally product so
  `POST /internal/v1/subscriptions/checkout` resolves Tally plans. Only
  needed if you plan to onboard Tally (`2b-svc-psi`); harmless otherwise.
