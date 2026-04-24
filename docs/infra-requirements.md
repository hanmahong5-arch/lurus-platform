# Infrastructure Requirements

Minimum versions and the reason each line exists. Paired with
`deployment.md` (how to wire them).

## Dependency minimums

| Component  | Minimum | Why this specific version |
|------------|---------|---------------------------|
| PostgreSQL | **15**  | Migrations `012_*` onward use RLS policies with `CREATE POLICY ... WITH CHECK` combined with `FORCE ROW LEVEL SECURITY` on the schema owner — both required for the tenant-isolation story and both only reliable from PG15. Also uses `MERGE ... WHEN NOT MATCHED BY SOURCE` in reconciliation queries. |
| Redis      | **6.0** | `SET key value EX n XX` used in rate-limiter + QR session TTL refresh; Lua scripts rely on `cjson` (shipped default since 6.0). |
| NATS       | **2.10** | JetStream consumer config uses `MaxAckPending` + `AckWait` semantics stabilised in 2.10. Without JetStream, event replay for `IDENTITY_EVENTS` is impossible. |
| Zitadel    | **2.60+** | Issuer metadata at `.well-known/openid-configuration` plus PAT-based Service Account API (`/management/v1/*`) used by the registration service. |
| Go         | **1.25** | Build toolchain; `slog` + generic `errors.Is`. Production images use `CGO_ENABLED=0 GOOS=linux` for static scratch builds. |
| Temporal   | **1.22+** | Optional. If wired, uses `workflow.Mutex` + schedule API (`scheduleHandle`) both stable as of 1.22. |

Omit any optional component (NATS, Temporal) and the Core still boots;
the affected features degrade rather than failing the pod. Missing a
required component causes `panic` at startup — **intentional**.

## Network diagram

```
              ┌─────────────────────┐
              │     Ingress         │
              │  (traefik / ALB)    │
              └──────────┬──────────┘
                         │ HTTPS (443)
          ┌──────────────┴──────────────────┐
          │                                 │
          ▼                                 ▼
  ┌─────────────────┐              ┌────────────────┐
  │  platform-core  │─── HTTPS ──▶│    Zitadel     │
  │  :18104 (HTTP)  │              │  auth.lurus.cn │
  │  :18105 (gRPC)  │◀── JWKS ────│  (OIDC + IDP)  │
  └───┬──┬──┬──┬──┬─┘              └────────────────┘
      │  │  │  │  │
      │  │  │  │  └──── HTTPS ──▶  GHCR
      │  │  │  │                   ghcr.io/hanmahong5-arch/*
      │  │  │  │                   (image pull only)
      │  │  │  │
      │  │  │  └────── NATS ────▶  nats.messaging.svc:4222
      │  │  │                       IDENTITY_EVENTS stream
      │  │  │
      │  │  └──────── TCP ──────▶  redis.messaging.svc:6379
      │  │                          DB 3 (platform)
      │  │
      │  └─────────── TCP ──────▶  lurus-pg-rw.database.svc:5432
      │                             schemas: identity, billing
      │
      └──────────── HTTP ────────▶  Temporal frontend (optional)
                                    :7233
```

All platform-core → infra traffic stays on the cluster's private
overlay. External egress is limited to Zitadel (auth) and GHCR (image
pull) — both over HTTPS. Webhook inbound (payment providers) terminates
on the Ingress and routes to `/webhook/*`.
