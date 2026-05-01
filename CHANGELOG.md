# Changelog

All notable changes to **lurus-platform** are recorded here. The platform is the
identity / billing / wallet / messaging backplane consumed by every Lurus product
(`lutu`, `tally`, `lucrum`, `forge`, …). Downstream maintainers: scan the
relevant date for **breaking** / **env** / **security** flags before bumping
your `INTERNAL_API_KEY` user or rolling deps.

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
The platform is shipped continuously off `master`; releases are dated rather
than versioned. Image tag `ghcr.io/hanmahong5-arch/lurus-platform-core:main`
always tracks the rollout pin in `deploy/k8s/base/deployment.yaml`
(annotation `rollout.lurus.cn/revision` = short SHA).

---

## [Unreleased]

### Security

- Error responses sanitized across the highest-traffic paths (`P2-6` partial).
  `internal/pkg/auth/middleware.go` no longer leaks raw `err.Error()` from the
  Zitadel validator; `api_keys_admin_handler.go` generic 500s now route through
  `respondInternalError` (internal err logged, client sees `internal_error` +
  generic message). Remaining leak sites (`apps_admin_handler.go:231,331`,
  `account_admin_handler.go:128`, `refund.go:202`,
  `internal_api.go:712`) queued for next sweep.

### Changed

- **Rate-limit response shape** — `429` body changed from
  `{"error": "rate limit exceeded", "retry_after": N}` to
  `{"error": "rate_limited", "message": "...", "retry_after": N}` to match the
  platform-wide envelope. Clients that read the `error` field as a free-form
  string need to switch to a code lookup. Status code, header `Retry-After`,
  and `retry_after` body field are unchanged.
- **Auth middleware error shape** — 401/403 responses from
  `internal/pkg/auth/middleware.go` now use the canonical envelope
  `{"error": "<code>", "message": "<text>"}` (codes: `unauthorized`,
  `forbidden`). Previously emitted raw `{"error": "<text>"}`. The auth package
  duplicates the envelope shape locally (`abortAuthError`) rather than
  importing the handler package — same contract, no import cycle.
- **API keys admin handler envelope** — `Create` / `Rotate` / `Revoke` /
  `List` migrated to `respondError` / `respondNotFound` /
  `respondInternalError`; the bind-error path now goes through
  `handleBindError` for field-level validator feedback. The `hint` field on
  `ErrAPIKeyCreating` was folded into the standard `message`.
- **`/api/v1/account/me*` and `/admin/v1/accounts/*` envelope** — all error
  paths in `account.go` migrated. `:id` parsing now goes through the shared
  `parsePathInt64` helper for a unified `invalid_parameter` envelope.

---

## 2026-04-30 — Hardening sweep (P0 + P1 + P2 partial)

The `docs/平台硬化清单.md` enterprise-grade bar pass. All P0 closed, P1 1–7
closed, P1-10 partial, P2-4 partial. No request/response contract removed.

### Security

- **Server-side JWT revocation** (`P1-5`). New `auth.SessionRevoker` writes the
  SHA-256 of the bearer/cookie token to Redis with TTL = remaining JWT lifetime.
  `POST /api/v1/auth/logout` now revokes; new `handler.RequireSession`
  middleware checks revoke list on every protected request. Fail-open on Redis
  error to survive blips. Stolen tokens are invalidatable retroactively without
  reissue.
- **Per-user rate-limit on identity endpoints** (`P1-6`). `/api/v1/whoami` and
  `/api/v1/account/me/llm-token` were structurally outside the
  `v1.Use(RateLimit.PerUser())` group; per-user limits silently no-op'd. New
  middleware chain `PerIP → RequireSession → PerUser → handler` makes
  `account_id`-keyed limits actually fire.
- **Security headers** (`P2-4` partial, commit `fd59465`).
  `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: strict-origin-when-cross-origin`,
  `Permissions-Policy: camera=(), microphone=(), geolocation=()`,
  `Strict-Transport-Security` (prod only). CSP deferred until Vite inline-script
  nonce plan is ready.

### Reliability

- **Topup → NewAPI sync deduped** (`P0-1`). `OnTopupCompleted` now takes a
  JetStream `eventID`. Redis `SETNX` envelope-id key prevents double-credit on
  consumer redelivery. Without the key, a single redelivery doubled the user's
  NewAPI quota.
- **`WebhookDeduper` defaults fail-closed for money paths** (`P0-2`). New
  `WithFailClosed()` and `WithKeyPrefix()` options expose explicit
  `ErrRedisUnavailable` so callers can NAK rather than skip dedup when Redis
  is down. Money-handling callsites must opt in.
- **Checkin TOCTOU race fixed** (`P0-4`). `CheckinService.DoCheckin` now relies
  on `INSERT … ON CONFLICT DO NOTHING` and surfaces a sentinel
  `ErrCheckinAlreadyToday`; verified stable under 20× concurrent checkins.
- **NewAPI client retry + circuit breaker** (`P1-1`, `P1-2`). 3 attempts /
  100ms→1s exponential backoff / ±30% jitter; retries restricted to 5xx, 408,
  429, network errors. Circuit transitions Closed → Open (5 consecutive
  failures) → HalfOpen (after 15s) → Closed/Open on probe result.
- **Reconcile cron for orphan accounts** (`P1-4`). 5-minute tick; batch 100;
  reuses idempotent `OnAccountCreated` find-then-create; poison-pill safe;
  `newapi_sync_ops_total{op="reconcile_tick"}` distinguishes from event path.

### Observability

- **NewAPI sync metrics** (`P0-3`). `newapi_sync_ops_total{op,result}` exposes
  12 series; the `result="duplicate"` bucket directly counts double-credits the
  dedup logic blocked.
- **`/readyz` learns NewAPI** (`P0-5`). Soft-checker — surfaces a `degraded`
  field rather than flipping `ready=false`. 30s cache so `/readyz` doesn't
  hammer NewAPI on every probe.
- **OpenTelemetry on NewAPI calls** (`P1-7`). Span name
  `newapi.<METHOD path>`; attributes include `newapi.attempt`, error status
  code, and a `retry` tag.

### Maturity / DX

- **CORS allow-list is now env-driven** (`P1-3`). New `CORS_ALLOWED_ORIGINS`
  CSV env var. Empty value falls back to the historical 5-domain list, so
  existing deployments keep their behaviour. Adding a new product no longer
  requires a code change.
- **Unified error envelope (partial)** (`P1-10`). `internal_api.go` UPPERCASE
  `error_code` callsites now emit the standard `{error, message}` envelope.
  New `ErrCodeUpstreamFailed`. ~17 raw `gin.H{"error": …}` sites still pending
  migration; rich-vs-flat envelope coexistence pending an ADR.
- **CHANGELOG.md** (`P1-8`). This file. Going forward, every shipped commit
  with a user-visible behavior, contract, or env-var change must add a line
  here under `[Unreleased]` before merging. Pure refactors / test-only / docs
  edits are exempt.

### Notes

- Image rollout pin: `e377d09` → see `deploy/k8s/base/deployment.yaml`.
- No env vars removed. New optional env vars: `CORS_ALLOWED_ORIGINS`.
- All `optional: true` Secret keys remain backwards-compatible with deployments
  that haven't created the corresponding entries yet.

---

## 2026-04-29 — Identity drop-in contract + NewAPI sync wiring

Foundation for the simplification roadmap (`docs/简化路线图.md`): products
talk to one platform via cookie or Bearer, never re-implement auth.

### Added

- **`GET /api/v1/whoami`** — single source of truth for "who is the caller".
  Reads `lurus_session` cookie or `Authorization: Bearer …`. Returns
  `{account_id, phone, vip_tier, locale, …}`. Drop-in replacement for the
  bespoke `/me` endpoints each product was hand-rolling.
- **Parent-domain session cookie** (`COOKIE_DOMAIN=.lurus.cn`,
  `Secure; HttpOnly; SameSite=Lax`). Cookie set on login at `identity.lurus.cn`
  is automatically sent by the browser to all `*.lurus.cn` products.
- **`account.newapi_user_id`** column + entity field on the account
  aggregate (migration `017_account_newapi_user_id.sql`).
- **NewAPI account-created hook** auto-provisions a NewAPI user the moment a
  Lurus account exists (`OnAccountCreated`).
- **NewAPI topup sync** — `OnTopupCompleted` propagates wallet credits into
  NewAPI quota.
- **`GET /api/v1/account/me/llm-token`** — products that need to call NewAPI
  on behalf of a user fetch the per-user LLM token here instead of holding the
  shared admin token.
- **Memorus proxy** — `/api/v1/memorus/*` proxied through platform-core with
  `X-API-Key` injected server-side; APP only ever sees the Lurus JWT.
  New env vars: `MEMORUS_INTERNAL_URL`, `MEMORUS_API_KEY` (both optional).

### Changed

- Integration guide rewritten (`docs/接入指南.md`) around the new two-line
  onboarding model: one cookie, one `/whoami` call.

---

## 2026-04-28 — Custom login UI + Identity admin abstraction

### Added

- **Lurus phone-first login overlay** on top of Zitadel v4.14.0. MiniMax-style
  unified login/register, legal pages, China-domestic UX polish.
- **`identity_admin` package** — Lurus API key abstraction over Zitadel
  Service User + PAT. Products that need admin operations now call our
  internal API rather than learning Zitadel's admin surface directly.

### Fixed

- Login deploy: removed orphan `nodeSelector: lurus.cn/vpn=true` left over
  from the old k3s cluster; image flipped to the lurus custom build.

---

## 2026-04-27 — Phase 4: Ops catalog + delegate-op framework

### Added

- **`internal/module/ops/`** — first-class abstraction for privileged
  operations. Every op (whether destructive or not) registers with metadata
  `{type, description, risk_level, destructive, delegate}`.
- **`GET /admin/v1/ops`** — admin JWT only. Returns the live ops catalogue
  for the admin dashboard.
- **Delegate-op bundle**: `delete_oidc_app`, `delete_account`, `approve_refund`
  — all gated by QR-confirmed APP biometric step-up.
  Metric `qr_delegate_confirms_total{op,result}`.
- **SMS relay**: `POST /internal/v1/sms/relay` — bridges Zitadel SMS webhook
  to Aliyun Dysms. Env: `SMS_PROVIDER`, `SMS_ALIYUN_*` (all optional;
  unconfigured ⇒ noop sender).
- **Tally product seed** (free / pro / enterprise plans).

---

## 2026-04-25 — Phase 3: Delete + rotate primitives

### Added

- **App registry reconciler** — declarative `apps.yaml` → Zitadel OIDC client
  + K8s Secret. New product onboarding becomes a PR, not a 5-step manual
  dance. Tombstones in Redis (24h TTL).
- **QR primitive v2** (`/api/v2/qr/*`) — multi-action handshake:
  login + join_org + delegate. Foundation for boss-phone biometric approval
  of high-stakes operations.

---

## Maintenance contract

- Every PR that changes a public endpoint, env var, metric name, error code,
  cookie attribute, or breaking internal contract MUST add a line under
  `[Unreleased]` before merge.
- Pure refactors, test-only changes, doc edits, and dependency bumps without
  behavior change are exempt.
- On rollout-pin bump (`deploy/k8s/base/deployment.yaml`), promote the
  `[Unreleased]` block to a dated section if the bump represents a release.
