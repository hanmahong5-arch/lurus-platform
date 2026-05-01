# lurusplatformclient

Typed Go client for [lurus-platform](../../README.md)'s `/internal/v1/*`
and selected `/api/v1/*` endpoints. Drop into any downstream Lurus service
that needs to talk to platform-core and stop hand-rolling HTTP boilerplate.

- **Stdlib-only**. No `resty`, no `go-resty`, no transitive surprise.
- **Lives inside lurus-platform**. Import via
  `github.com/hanmahong5-arch/lurus-platform/pkg/lurusplatformclient`.
  Wire-stable on `master`; breaking changes will bump to
  `pkg/lurusplatformclient/v2/...`.
- **Public surface**. Does NOT depend on any `internal/*` package from
  this repo, so consuming services pull a clean tree.

## Install

```bash
go get github.com/hanmahong5-arch/lurus-platform/pkg/lurusplatformclient
```

## Authentication matrix

Exactly one of `WithInternalKey`, `WithBearerToken`, or `WithCookieToken`
should be set per `Client` instance.

| Method | When to use | Configure |
|--------|-------------|-----------|
| InternalKey + scope | service-to-service (lucrum / tally / kova / forge → platform). Sent as `Authorization: Bearer <key>`. | `WithInternalKey(os.Getenv("LURUS_PLATFORM_INTERNAL_KEY"))` |
| Bearer JWT | end-user → service → platform proxy mode (forward the user's token). Sent as `Authorization: Bearer <jwt>`. | `WithBearerToken(userJWT)` |
| Cookie | browser-style flows where caller has the `lurus_session` cookie. | `WithCookieToken(sessionToken)` |

## Examples

### 1. Service-to-service — lookup an account by ID

```go
import "github.com/hanmahong5-arch/lurus-platform/pkg/lurusplatformclient"

c := lurusplatformclient.New("https://identity.lurus.cn").
    WithInternalKey(os.Getenv("LURUS_PLATFORM_INTERNAL_KEY"))

acc, err := c.GetAccountByID(ctx, 42)
if err != nil {
    if pe, ok := lurusplatformclient.AsPlatformError(err); ok && pe.IsNotFound() {
        // graceful "user gone" path
    }
    return err
}
log.Printf("welcome %s (%s)", acc.DisplayName, acc.LurusID)
```

### 2. End-user proxy — forward the user's bearer to /whoami

```go
// e.g. lucrum-backend forwarding the frontend's Bearer to platform
c := lurusplatformclient.New("https://identity.lurus.cn").
    WithBearerToken(bearerFromIncomingRequest)

w, err := c.Whoami(ctx)
if err != nil {
    if pe, ok := lurusplatformclient.AsPlatformError(err); ok && pe.IsUnauthorized() {
        // token expired — clear and re-login
    }
    return err
}
log.Printf("acting as account_id=%d", w.AccountID)
```

### 3. LLM token fetch — drop-in for any service that needs a NewAPI bearer

```go
c := lurusplatformclient.New("https://identity.lurus.cn").
    WithBearerToken(userJWT)

tok, err := c.GetLLMToken(ctx, &lurusplatformclient.LLMTokenOptions{
    Name: "lucrum", // optional; "" = platform-default
})
if err != nil {
    return err
}
// Now point your OpenAI SDK at tok.BaseURL with tok.Key as the bearer.
```

## Error handling

Every non-2xx response is decoded into a `*PlatformError`. Use the
`errors.As` pattern (or the convenience `AsPlatformError` helper) to
inspect it:

```go
acc, err := c.GetAccountByID(ctx, id)
if err != nil {
    var pe *lurusplatformclient.PlatformError
    if errors.As(err, &pe) {
        switch {
        case pe.IsNotFound():       // 404 — show "user not found"
        case pe.IsUnauthorized():   // 401 — re-auth
        case pe.IsForbidden():      // 403 — scope missing
        case pe.IsInsufficient():   // 400/insufficient_balance — top up
        case pe.IsRateLimited():    // 429 — backoff
        case pe.IsUpstreamFailed(): // 502/upstream_failed — paired rollback already happened, safe to retry
        }
        log.Printf("platform err: %s", pe) // includes Code+Status+Message
    } else {
        // network / DNS / context-cancel — not a *PlatformError
    }
}
```

`pe.RawBody` retains up to 4 KiB of the raw response for diagnostics —
log it on bug reports, but don't branch on its contents.

## Retry policy

The client does **NOT** retry by default. Caller-controlled. Use
`PlatformError.IsRetriable()` as the canonical predicate to feed into a
retry loop:

```go
op := func() (*Account, error) { return c.GetAccountByID(ctx, id) }
for attempt := 0; attempt < 3; attempt++ {
    a, err := op()
    if err == nil {
        return a, nil
    }
    var pe *lurusplatformclient.PlatformError
    if errors.As(err, &pe) && !pe.IsRetriable() {
        return nil, err
    }
    time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
}
```

`IsRetriable()` returns true for: 5xx, 429, and `upstream_failed`. False
for everything else (400/401/403/404/409). Network errors fall outside
the `*PlatformError` envelope; treat as retriable at your discretion.

## Methods

| Method | Endpoint | Auth |
|--------|----------|------|
| `Whoami` | `GET /api/v1/whoami` | Bearer / Cookie |
| `GetLLMToken` | `GET /api/v1/account/me/llm-token` | Bearer / Cookie |
| `GetAccountByID` | `GET /internal/v1/accounts/:id` | InternalKey + `account:read` |
| `GetAccountByEmail` | `GET /internal/v1/accounts/by-email/:email` | InternalKey + `account:read` |
| `GetWalletBalance` | `GET /internal/v1/accounts/:id/wallet/balance` | InternalKey + `wallet:read` |
| `DebitWallet` | `POST /internal/v1/accounts/:id/wallet/debit` | InternalKey + `wallet:debit` |
| `GetEntitlements` | `GET /internal/v1/accounts/:id/entitlements/:product_id` | InternalKey + `entitlement` |

## Versioning

- Track the lurus-platform `CHANGELOG.md` `[Unreleased]` block for
  additions.
- Method signatures and struct field names are **wire-stable on
  master** — additive changes only.
- Removing a method or renaming a field bumps the package to
  `pkg/lurusplatformclient/v2/`. Until then, downstream callers can
  `go get -u` safely.
