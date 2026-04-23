# QR Primitive (v2)

A platform-level QR-code handshake for binding two devices in a single flow:
Web/desktop "initiator" generates a QR; the APP "confirmer" (authenticated)
scans it; the initiator polls for the result. Same primitive serves multiple
actions (login today; join-org and delegate in a later phase).

> Status — **Phase 1 shipped**: `action=login` only.
> `action=join_org` and `action=delegate` return 501 `action_not_supported_yet`
> by design until they are wired with authenticated create endpoints.

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
lurus://qr?v=1&id=<64-hex>&a=<action>&h=<16-hex>
```

| Field | Meaning |
|-------|--------|
| `v`   | Payload version. Reserved for shape evolution. |
| `id`  | 256-bit random session id (hex-encoded). |
| `a`   | Action name (`login` / `join_org` / `delegate`). |
| `h`   | First 16 hex chars of `HMAC-SHA256(session_secret, id \|\| 0x00 \|\| action)`. Defeats id substitution attacks on the scanner side. |

The scanner app MUST send `h` back as `sig` in the confirm request; the server
rejects mismatches with `invalid_signature`.

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
| 200 | `{ id, action, qr_payload, expires_in }` |
| 400 | `invalid_request` — malformed JSON body |
| 400 | `invalid_action` — unknown action |
| 501 | `action_not_supported_yet` — valid but not-yet-wired action |
| 500 | `id_generation_failed` / `store_failed` — infra failure |

The `qr_payload` is the URI to encode into the QR image on the web side.
`expires_in` is always 300 (seconds).

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

{ "sig": "<16-hex sig from scanned payload>" }
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
| Payload tamper-proof | HMAC-SHA256 over `id‖0x00‖action` with `SESSION_SECRET` |
| Confirm requires auth | `/api/v2` group is behind `JWT.Auth()` |
| One-shot token delivery | CAS `confirmed→consumed` in Lua; second poller gets 410 |
| No replay past TTL | Redis key TTL 300s; Redis-enforced |
| Constant-time sig compare | `hmac.Equal` on server |
| PerIP rate limit on create | Shared `deps.RateLimit.PerIP()` middleware |
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
- **Metrics to add later**: `qr_sessions_created_total{action}`,
  `qr_sessions_confirmed_total{action}`, `qr_sessions_expired_total`,
  `qr_confirm_latency_seconds`.
