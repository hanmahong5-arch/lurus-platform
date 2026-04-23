# Test Coverage

Measured 2026-04-23, Go 1.23.

## Headline

| Scope | Coverage |
|-------|----------|
| Raw `go test ./...` (includes non-testable) | 81.2% |
| Testable Go code (ex vendored, generated, `cmd/core` wiring) | **88.7%** |
| Business logic (ex SDK-hard-blocked packages) | **90.1%** |

Session delta: 68.5% → 81.2% raw (+12.7pp) over the 2026-04-23 coverage sprint.

## Per-package breakdown

| Package | Coverage | Notes |
|---------|----------|-------|
| `internal/domain/entity` | 100.0% | |
| `internal/lifecycle` | 100.0% | |
| `internal/pkg/audit` | 100.0% | |
| `internal/pkg/email` | 100.0% | |
| `internal/pkg/event` | 100.0% | |
| `internal/pkg/idempotency` | 100.0% | |
| `internal/pkg/tenant` | 100.0% | new package this session |
| `internal/temporal/activities` | 98.9% | |
| `internal/pkg/sms` | 98.2% | |
| `internal/module` | 96.9% | |
| `internal/pkg/metrics` | 95.2% | |
| `internal/temporal/workflows` | 94.1% | |
| `internal/pkg/retry` | 92.9% | |
| `internal/pkg/zitadel` | 92.7% | |
| `internal/pkg/sanitize` | 92.6% | |
| `internal/pkg/slogctx` | 92.0% | |
| `internal/adapter/handler` | 90.1% | |
| `internal/app` | 90.1% | |
| `internal/pkg/lurusapi` | 89.6% | |
| `internal/adapter/handler/router` | 89.2% | |
| `internal/adapter/repo` | 88.7% | SQLite blocker on `service_key.go` (PG `text[]`) |
| `internal/pkg/ratelimit` | 87.5% | |
| `internal/adapter/grpc` | 85.9% | |
| `internal/pkg/tracing` | 85.7% | |
| `pkg/platformclient` | 85.7% | |
| `internal/pkg/config` | 85.5% | |
| `internal/pkg/cache` | 84.8% | |
| `internal/pkg/auth` | 84.1% | |
| `internal/adapter/payment` | **74.8%** | SDK hard blocker (see below) |
| `internal/adapter/nats` | **60.0%** | dep hard blocker (see below) |

## Hard blockers (not fixable without production refactor)

| Area | Lines blocked | Root cause | Fix |
|------|---------------|-----------|-----|
| `adapter/payment/alipay.go` | ~40 | `gopay.alipay.Client` is a concrete struct with no HTTP transport injection | Wrap in `alipayDoer` interface, inject `*http.Client` |
| `adapter/payment/wechat_pay.go` | ~50 | Same for `gopay.wechat.ClientV3`; also `AutoVerifySign` requires a live cert endpoint | Same pattern |
| `adapter/payment/stripe.go` | ~20 | `stripe-go/v76` uses a package-level `DefaultBackend` | Accept a custom `stripe.Backend` in constructor |
| `adapter/nats` (NewPublisher/Consumer) | ~15 | Constructors take `*nats.Conn`; `nats-server/v2` is not in `go.mod` | Accept an interface (`JetStreamContext`) OR add embedded server dep for tests |
| `adapter/repo/service_key.go` | all | `entity.StringList` = PG `text[]`; SQLite has no scanner | Tests tagged `//go:build integration`; real coverage via PG tests |
| `adapter/repo` referral `CreateRewardEvent` idempotent path | 1 branch | Checks PG error code `23505`, unreachable in SQLite | Same — integration-only |

## Excluded from denominator (non-testable)

- `cmd/core/main.go` — service wiring / main entry (500+ LOC; integration-only).
- `proto/gen/go/...` — generated gRPC stubs.
- `apps/admin/node_modules/flatted/golang/pkg/flatted/flatted.go` — a Go file accidentally shipped inside a Node dependency. Excluded via `go list | grep -v`.
- Root package `.` — only holds a dummy var for directive imports.

## Re-measuring

```bash
# Raw (what CI reports)
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1

# Testable scope (honest number)
PKGS=$(go list ./... | grep -v -E 'node_modules|proto/gen|cmd/core|^github.com/hanmahong5-arch/lurus-platform$|internal/temporal$')
go test -coverprofile=coverage-real.out $PKGS
go tool cover -func=coverage-real.out | tail -1

# Business logic only (exclude SDK-hard-blockers)
PKGS2=$(echo "$PKGS" | grep -v -E 'adapter/nats|adapter/payment')
go test -coverprofile=coverage-biz.out $PKGS2
go tool cover -func=coverage-biz.out | tail -1
```

## What would it take to push raw total over 90%

1. **Refactor payment providers to accept HTTP/backend interfaces** (~1 week) — `alipay`, `wechat_pay`, `stripe` each need a small interface wrap. Would lift payment to ~90% and raw total by ~4pp.
2. **Add `nats-server/v2` as a test dep** or refactor NATS constructors (~2 days). Would lift nats to ~90% and raw total by ~1pp.
3. **Integration tests against real PG** for `service_key.go` + `CreateRewardEvent` idempotent path (~1 day). Small raw-total lift but closes the SQLite gap.
4. **Main-entry smoke test** via `cmd/core` `TestMain` or using `httptest` against the wired engine (~half day). Would lift cmd/core from 0% to ~50%, raw total by ~2-3pp.

Cumulative: if all four completed, raw `go test ./...` would hit ~90-92%.
