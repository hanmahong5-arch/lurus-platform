# Lurus Platform Integration Guide

How to integrate your product with the lurus-platform identity & billing service.

## Quick Start (5 minutes)

### 1. Import the SDK

```go
import "github.com/hanmahong5-arch/lurus-platform/pkg/platformclient"
```

### 2. Initialize the Client

```go
client := platformclient.New(
    os.Getenv("IDENTITY_SERVICE_URL"),  // e.g. "http://platform-core.lurus-platform.svc:18104"
    os.Getenv("INTERNAL_API_KEY"),
)
```

### 3. Authenticate a User

```go
// Option A: From Zitadel JWT (the user's OIDC sub claim)
account, err := client.GetAccountByZitadelSub(ctx, jwtClaims.Sub)

// Option B: From session token (cookie-based auth)
account, err := client.ValidateSession(ctx, sessionToken)

// Option C: From email/phone lookup
account, err := client.GetAccountByEmail(ctx, "user@example.com")
```

### 4. Check Permissions

```go
ent, err := client.GetEntitlements(ctx, account.ID, "your-product-id")
if ent["plan_code"] == "free" {
    // Free tier — limit features
}
if ent["max_requests"] != "" {
    maxReq, _ := strconv.Atoi(ent["max_requests"])
    // Enforce quota
}
```

### 5. Charge the User

```go
// Direct wallet debit (for small, immediate charges)
result, err := client.DebitWallet(ctx, account.ID, 0.50, "api_call", "GPT-4 inference", "your-product")
if errors.Is(err, platformclient.ErrInsufficientBalance) {
    // Wallet empty — create a checkout session to redirect user to payment
    checkout, _ := client.CreateCheckoutSession(ctx, platformclient.CheckoutRequest{
        AccountID:     account.ID,
        AmountCNY:     50.0,
        PaymentMethod: "stripe",
        SourceService: "your-product",
    })
    // Redirect user to checkout.PayURL
    return redirect(checkout.PayURL)
}
```

---

## Billing Patterns

### Pay-per-use (API calls)

```
User makes request → PreAuthorize (freeze estimate) → Process → SettlePreAuth (actual cost)
```

```go
// 1. Freeze estimated cost before processing
pa, err := client.PreAuthorize(ctx, accountID, platformclient.PreAuthRequest{
    Amount:      1.0,  // estimated max cost
    ProductID:   "your-product",
    ReferenceID: requestID,
    Description: "API call estimate",
    TTLSeconds:  600,  // 10 min
})
if errors.Is(err, platformclient.ErrInsufficientBalance) {
    return errors.New("insufficient balance")
}

// 2. Process the request (LLM call, computation, etc.)
actualCost := doWork()

// 3. Settle with actual cost (always less than or equal to frozen amount)
err = client.SettlePreAuth(ctx, pa.PreAuthID, actualCost)

// OR: Release if no charge needed (e.g. request failed)
err = client.ReleasePreAuth(ctx, pa.PreAuthID)
```

### Subscription

```
User selects plan → Wallet pay (instant) or External pay (redirect to payment page)
```

Subscription management is handled by the platform's public API (`/api/v1/subscriptions/checkout`). Your product only needs to check entitlements.

### Top-up redirect

When a user has insufficient balance, create a checkout session:

```go
checkout, _ := client.CreateCheckoutSession(ctx, platformclient.CheckoutRequest{
    AccountID:      accountID,
    AmountCNY:      amount,
    PaymentMethod:  "stripe",  // or "epay_alipay", "epay_wechat", "creem"
    SourceService:  "your-product",
    IdempotencyKey: uniqueKey,  // prevents duplicate charges on retry
})
// checkout.PayURL → redirect user
// checkout.OrderNo → poll for completion via GetCheckoutStatus()
```

---

## Event Subscription (NATS)

Subscribe to platform events to react to billing/identity changes:

```go
import "github.com/nats-io/nats.go"

nc, _ := nats.Connect(os.Getenv("NATS_ADDR"))
js, _ := nc.JetStream()

// Invalidate entitlement cache when permissions change
js.QueueSubscribe("identity.entitlement.updated", "your-product-consumer", func(msg *nats.Msg) {
    var event struct {
        AccountID int64  `json:"account_id"`
        ProductID string `json:"product_id"`
    }
    json.Unmarshal(msg.Data, &event)

    if event.ProductID == "your-product" {
        cache.Delete(event.AccountID)
    }
    msg.Ack()
})
```

### Available Events

| Subject | When | Payload |
|---------|------|---------|
| `identity.account.created` | New user registered | `{account_id, lurus_id, email}` |
| `identity.subscription.activated` | Plan purchased | `{account_id, product_id, plan_code, expires_at}` |
| `identity.subscription.expired` | Plan expired | `{account_id, product_id}` |
| `identity.topup.completed` | Wallet funded | `{account_id, amount_cny, order_no}` |
| `identity.entitlement.updated` | Permissions changed | `{account_id, product_id, entitlements}` |
| `identity.vip.level_changed` | VIP tier changed | `{account_id, old_level, new_level}` |

---

## Error Handling

The SDK returns well-known errors you can check with `errors.Is()`:

```go
switch {
case errors.Is(err, platformclient.ErrNotFound):
    // User not found — prompt registration
case errors.Is(err, platformclient.ErrInsufficientBalance):
    // Wallet empty — redirect to top-up
case errors.Is(err, platformclient.ErrUnauthorized):
    // Bad API key — check configuration
case errors.Is(err, platformclient.ErrRateLimited):
    // Too many calls — implement backoff
default:
    // Unknown error — log and retry
    slog.Error("platform call failed", "err", err)
}
```

---

## Environment Variables

| Variable | Required | Example |
|----------|----------|---------|
| `IDENTITY_SERVICE_URL` | Yes | `http://platform-core.lurus-platform.svc:18104` |
| `INTERNAL_API_KEY` | Yes | (get from ops team) |
| `NATS_ADDR` | Optional | `nats://nats.messaging.svc:4222` |

---

## Checklist Before Production

- [ ] SDK client initialized with correct URL and API key
- [ ] Account lookup works (test with a known user)
- [ ] Entitlement check returns expected plan_code
- [ ] Wallet debit succeeds and returns balance_after
- [ ] ErrInsufficientBalance handled with checkout redirect
- [ ] NATS events subscribed (if needed for cache invalidation)
- [ ] Timeout/retry configured for platform calls
- [ ] Monitoring: alert on platform call latency > 1s or error rate > 1%
