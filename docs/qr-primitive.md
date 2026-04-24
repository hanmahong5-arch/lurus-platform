# QR Primitive (v2)

A platform-level QR-code handshake for binding two devices in a single flow:
Web/desktop "initiator" generates a QR; the APP "confirmer" (authenticated)
scans it; the initiator polls for the result. Same primitive serves multiple
actions (login today; join-org and delegate in a later phase).

> Status — **Phase 2 shipped**: `action=login` + `action=join_org` are wired.
> `action=delegate` still returns 501 `action_not_supported_yet`.
> `join_org` sessions must be created via `POST /api/v2/qr/session/authed`
> (JWT-protected, caller must be `owner`/`admin` of the target org).

### Action catalog

| Action | Create endpoint | Side effect | Confirm response |
|--------|----------------|-------------|------------------|
| `login` | `POST /api/v2/qr/session` (unauthed) | None on Confirm; token issued to Web poller on `/status` | `{confirmed, action}` (token delivered separately via status poll) |
| `join_org` | `POST /api/v2/qr/session/authed` (JWT; caller = org owner/admin) | `OrganizationService.AddMember(orgID, initiator, scanner, role)` inline on Confirm | `{confirmed, action, org_id, role, joined_at}` |
| `delegate` | *not implemented* | *n/a* | 501 |

### Why actions split the Confirm / PollStatus path

- **login**: the Web initiator is the token recipient, so the side effect
  (issuing a session token) runs on the `/status` poll path inside
  `writeConfirmResult`. The APP `POST /confirm` just flips Redis state.
- **join_org**: the APP scanner IS the new member; there is no Web poller
  that needs a result. The side effect (`AddMember` + NATS event) runs
  inline on `POST /confirm` inside `confirmJoinOrg` and the enriched
  response goes back to the APP immediately.

---

## State machine

```
┌──────────┐  create   ┌──────────┐  confirm   ┌───────────┐  status poll  ┌──────────┐
│ (no key) │ ────────► │ pending  │ ─────────► │ confirmed │ ────────────► │ consumed │
└──────────┘           └──────────┘            └───────────┘               └──────────┘
                        │                         │                         │
                        ├─ TTL 5 min ─────────────┤                         │
                        │                                                   │
                        └─ TTL 5 min expiry ─► (key deleted) ◄──────────────┘
                                                                            TTL 60s
```

All transitions happen via Redis Lua scripts (CAS), so concurrent pollers /
confirmers can't race into an inconsistent state.

---

## Payload format

```
lurus://qr?v=1&id=<64-hex>&a=<action>&t=<unix>&h=<16-hex>
```

| Field | Meaning |
|-------|--------|
| `v`   | Payload version. Reserved for shape evolution. |
| `id`  | 256-bit random session id (hex-encoded). |
| `a`   | Action name (`login` / `join_org` / `delegate`). |
| `t`   | Server-issued creation timestamp (unix seconds). Part of the MAC — rebuilding the URL with a different `t` invalidates `h`. |
| `h`   | First **24** hex chars of `HMAC-SHA256(signing_key, 0x01 \|\| id \|\| 0x00 \|\| action \|\| 0x00 \|\| t)`. The leading `0x01` is a domain-separator byte isolating QR HMACs from any other uses of the same master secret. 24 hex = 96 bits = brute force unreachable under rate-limited 5-min TTL. |

The scanner app MUST send `h` back as `sig` **and** `t` back as `t` in the
confirm request; the server rejects mismatches with `invalid_signature`.

**Signing key selection (key rotation, R1.2)**: the server maintains a
**keyring** of one or more (kid, secret) entries. Signing always uses the
highest-kid key; verification accepts any key in the ring, which lets
operators rotate without dropping in-flight sessions:

```text
1. Add new key → QR_SIGNING_KEYS="1:<old>,2:<new>" (both valid, 2 signs)
2. Wait ≥ 5 min (TTL)  → no more QRs signed with kid=1 exist in the wild
3. Remove old key     → QR_SIGNING_KEYS="2:<new>"
```

When `QR_SIGNING_KEYS` is unset, `SESSION_SECRET` becomes the single implicit
key with kid `"default"` (preserves the single-secret deploy mode).

**Backward compatibility (window: until 2026-06-01)**: pre-B5 APP builds may
send `sig` without `t` in the confirm body. The server falls back to a
legacy `HMAC-SHA256(signing_key, id || 0x00 || action)` check **truncated
to 16 hex chars** (the pre-2026-04-24 length) and emits a
`qr.legacy_payload_signature` warn log. The legacy path is scheduled for
removal on 2026-06-01; after that, omitting `t` yields 400 `invalid_signature`.

---

## Endpoints

### 1. Create session

```
POST /api/v2/qr/session
Content-Type: application/json

{ "action": "login" }
```

**Public** (unauth). Per-IP rate-limited via `deps.RateLimit.PerIP()`.

**Responses:**

| Code | Body |
|------|------|
| 200 | `{ id, action, qr_payload, expires_in, expires_at }` |
| 400 | `invalid_request` — malformed JSON body |
| 400 | `invalid_action` — unknown action |
| 501 | `action_not_supported_yet` — valid but not-yet-wired action |
| 500 | `id_generation_failed` / `store_failed` — infra failure |

The `qr_payload` is the URI to encode into the QR image on the web side.
`expires_in` is always 300 (seconds). `expires_at` is an RFC3339 UTC
timestamp (`issued_at + 300s`) convenient for APP countdown UIs.

### 2. Poll status

```
GET /api/v2/qr/:id/status?timeout=30
```

**Public** (unauth). Long-polls for up to `timeout` seconds (max 30s).

**Responses:**

| State | Code | Body |
|-------|------|------|
| Still pending at deadline | 200 | `{ "status": "pending" }` |
| Confirmed → consumed (login) | 200 | `{ "status": "confirmed", "action": "login", "token": "<jwt>", "expires_in": 604800 }` |
| Already consumed | 410 | `session_consumed` |
| Session expired / never existed | 404 | `session_not_found` |

The confirmed→consumed transition is **atomic and one-shot**. A second poller
that arrives after the first one claimed the token will see 410. Client code
must therefore treat any 200-with-token as its exclusive login grant.

### 3. Confirm

```
POST /api/v2/qr/:id/confirm
Authorization: Bearer <session-token or Zitadel JWT>
Content-Type: application/json

{ "sig": "<16-hex sig from scanned payload>", "t": <unix seconds from scanned payload> }
```

**Authenticated** (`/api/v2` group → `JWT.Auth()` → `tenant.Middleware()` →
`RateLimit.PerUser()`).

**Responses:**

| Code | When |
|------|------|
| 200 `{ "confirmed": true, "action": "login" }` | Success (pending → confirmed). |
| 400 `invalid_request` | Missing `sig`. |
| 400 `invalid_signature` | HMAC mismatch (payload wasn't minted by this platform, or was tampered). |
| 401 | No `account_id` in context (caller not authenticated). |
| 404 `session_not_found` | Session expired or never existed. |
| 409 `invalid_state` | Already confirmed / consumed / racing confirm. |

---

## End-to-end: QR login flow

```
   Web (browser)                  Platform                 APP (scanner)
        │                           │                           │
        │ 1. POST /api/v2/qr/session│                           │
        │───────────────────────────►                           │
        │     { action: "login" }   │                           │
        │                           │                           │
        │◄─── id, qr_payload, ──────│                           │
        │     expires_in=300        │                           │
        │                           │                           │
        │ 2. render QR              │                           │
        │                           │                           │
        │                           │        scan QR            │
        │                           │◄──────────────────────────│
        │                           │                           │
        │ 3. GET /status?timeout=30 │                           │
        │───────────────────────────►                           │
        │       (long-poll)         │                           │
        │                           │                           │
        │                           │ 4. POST /confirm          │
        │                           │◄──────────────────────────│
        │                           │ { sig, Authorization:... }│
        │                           │                           │
        │◄── 200 { status:confirmed,│                           │
        │        token: <jwt> }     │                           │
        │                           │                           │
        │ 5. store token,           │                           │
        │    user is logged in      │                           │
```

---

## Security properties

| Property | Mechanism |
|----------|-----------|
| Session id unguessable | 256-bit `crypto/rand` |
| Payload tamper-proof | HMAC-SHA256 over `0x01‖id‖0x00‖action‖0x00‖t`, truncated to **96 bits**. Domain-separator byte isolates QR HMACs from other HMACs that might share the master secret. |
| Key rotation without downtime | Keyring (`QR_SIGNING_KEYS=kid:hex32[,kid:hex32...]`) — signing uses highest kid, verification accepts any active key |
| Confirm requires auth | `/api/v2` group is behind `JWT.Auth()` |
| One-shot token delivery | CAS `confirmed→consumed` in Lua; second poller gets 410 |
| No replay past TTL | Redis key TTL 300s (Redis-enforced) **and** signed-timestamp window `t ± (TTL + 30s skew)` (MAC-enforced — screenshot QRs can't outlive their window even if Redis record persists) |
| Constant-time sig compare | `hmac.Equal` on server (tried per active key in keyring) |
| PerIP rate limit on create | Shared `deps.RateLimit.PerIP()` middleware — `TRUSTED_PROXIES` CIDR list controls which upstreams can set `X-Forwarded-For`, so per-IP limits cannot be bypassed by XFF spoofing |
| PerUser rate limit on confirm | Shared `deps.RateLimit.PerUser()` middleware |

**Non-goals (by design):**

- The primitive does NOT bind the QR to a specific scanner device. Any
  authenticated account can confirm (policy is applied at the action layer,
  not at the QR primitive layer).
- The primitive does NOT offer push notifications. It's a polling handshake.
- The primitive does NOT encrypt the confirm payload. `sig` is a MAC, not an
  envelope secret — the channel is TLS.

---

## Why there are two QR implementations

`internal/adapter/handler/qr_login_handler.go` (v1) predates this primitive
and only supports login. It is kept wired at `/api/v1/public/qr-login/*` for
backward compatibility with any existing Web client that already targets it.
The v2 primitive at `/api/v2/qr/*` is the one all new integrations should use;
v1 will be retired once nothing calls it.

---

## Adding a new action

1. Add the const to `entity.QRAction` and to `IsValidQRAction`.
2. If the initiator must be authenticated (likely for `join_org`, `delegate`),
   add a second create endpoint under the `/api/v2` (auth) group and populate
   `session.AccountID` with the initiator's id at create-time. Don't loosen
   auth on `POST /session`.
3. Add a case to `QRHandler.writeConfirmResult` returning the action-specific
   payload on poll.
4. In `Confirm`, add action-specific validation / side-effects (e.g. for
   `join_org`: verify `session.AccountID` is an org admin, then call
   `OrganizationService.AddMember`).
5. Add tests covering happy path + authorization failure + rollback.

---

## Operational notes

- **Redis dependency**: QR sessions live only in Redis. A Redis outage takes
  QR handshake down; all existing auth methods (password, OIDC, wechat) keep
  working. Document on the status page accordingly.
- **TTL economics**: each pending session is ~200 bytes. 10 K concurrent
  sessions ≈ 2 MB Redis.

---

## Metrics

All metrics are emitted from `internal/pkg/metrics/metrics.go` under the
`lurus_platform_` namespace and exposed via `/metrics`.

| Metric | Type | Labels | Incremented on |
|--------|------|--------|----------------|
| `lurus_platform_qr_sessions_created_total` | Counter | `action` | Successful `POST /api/v2/qr/session` |
| `lurus_platform_qr_confirmed_total` | Counter | `action` | Successful `POST /api/v2/qr/:id/confirm` (pending → confirmed CAS win) |
| `lurus_platform_qr_expired_total` | Counter | — | 404 from status or confirm (TTL expired or id never existed) |
| `lurus_platform_qr_signature_rejected_total` | Counter | — | Confirm rejected for invalid HMAC or stale timestamp |
| `lurus_platform_qr_confirm_latency_seconds` | Histogram | `action` | Observed on every confirm handler exit |
| `lurus_platform_qr_legacy_signatures_total` | Counter | — | Confirm took the pre-B5 timestamp-less HMAC path. Tracks the deprecation window — near 2026-06-01, if the legacy share exceeds ~1%, delay the cutover. |
| `lurus_platform_qr_polls_inflight` | Gauge | — | Active `/status` long-poll goroutines currently holding a concurrency-semaphore slot. |
| `lurus_platform_qr_polls_rejected_overload_total` | Counter | — | `/status` requests shed with 503 because `QR_MAX_INFLIGHT_POLLS` was saturated. |

`action` is one of `login` / `join_org` / `delegate` / `unknown` (the last
only for confirm latency when the session read failed before the action was known).

## Grafana

Dashboard JSON: [`deploy/grafana/dashboards/qr-primitive.json`](../deploy/grafana/dashboards/qr-primitive.json).

Import into Grafana (datasource must be named `prometheus`). Four panels:
session-lifecycle rate, confirm-latency p50/p95/p99, signature-rejection
rate (with a 0.1/sec alert line), and a 1h action-breakdown pie.
