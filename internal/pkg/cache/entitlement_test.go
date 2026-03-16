package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		accountID int64
		productID string
		want      string
	}{
		{1, "llm-api", "identity:entitlements:1:llm-api"},
		{42, "quant-trading", "identity:entitlements:42:quant-trading"},
		{9999, "webmail", "identity:entitlements:9999:webmail"},
	}
	for _, tc := range tests {
		got := cacheKey(tc.accountID, tc.productID)
		want := fmt.Sprintf("identity:entitlements:%d:%s", tc.accountID, tc.productID)
		if got != want {
			t.Errorf("cacheKey(%d,%q)=%q, want %q", tc.accountID, tc.productID, got, want)
		}
		if got != tc.want {
			t.Errorf("cacheKey(%d,%q)=%q, want %q", tc.accountID, tc.productID, got, tc.want)
		}
	}
}

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return mr, rdb
}

func TestEntitlementCache_Get_Miss(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewEntitlementCache(rdb, 5*time.Minute)
	got, err := c.Get(context.Background(), 1, "llm-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on cache miss, got %v", got)
	}
}

func TestEntitlementCache_SetAndGet(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewEntitlementCache(rdb, 5*time.Minute)
	ctx := context.Background()

	em := map[string]string{"plan_code": "pro", "max_models": "10"}
	if err := c.Set(ctx, 1, "llm-api", em); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := c.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["plan_code"] != "pro" || got["max_models"] != "10" {
		t.Errorf("got %v, want plan_code=pro, max_models=10", got)
	}
}

func TestEntitlementCache_Invalidate(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewEntitlementCache(rdb, 5*time.Minute)
	ctx := context.Background()

	_ = c.Set(ctx, 1, "llm-api", map[string]string{"plan_code": "pro"})
	if err := c.Invalidate(ctx, 1, "llm-api"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	got, err := c.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get after invalidate: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after invalidate, got %v", got)
	}
}

func TestEntitlementCache_InvalidateAccount(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewEntitlementCache(rdb, 5*time.Minute)
	ctx := context.Background()

	_ = c.Set(ctx, 1, "llm-api", map[string]string{"plan_code": "pro"})
	_ = c.Set(ctx, 1, "webmail", map[string]string{"plan_code": "free"})
	_ = c.Set(ctx, 2, "llm-api", map[string]string{"plan_code": "free"})

	if err := c.InvalidateAccount(ctx, 1); err != nil {
		t.Fatalf("InvalidateAccount: %v", err)
	}

	// account 1 entries should be gone
	for _, pid := range []string{"llm-api", "webmail"} {
		got, _ := c.Get(ctx, 1, pid)
		if got != nil {
			t.Errorf("account=1 product=%s should be nil after InvalidateAccount, got %v", pid, got)
		}
	}
	// account 2 should remain
	got, _ := c.Get(ctx, 2, "llm-api")
	if got == nil || got["plan_code"] != "free" {
		t.Errorf("account=2 should remain, got %v", got)
	}
}

func TestEntitlementCache_InvalidateAccount_NoKeys(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewEntitlementCache(rdb, 5*time.Minute)
	// should not error when no keys match
	if err := c.InvalidateAccount(context.Background(), 999); err != nil {
		t.Fatalf("InvalidateAccount with no keys: %v", err)
	}
}

func TestEntitlementCache_TTL_Expiry(t *testing.T) {
	mr, rdb := newTestRedis(t)
	c := NewEntitlementCache(rdb, 1*time.Second)
	ctx := context.Background()

	_ = c.Set(ctx, 1, "llm-api", map[string]string{"plan_code": "pro"})
	mr.FastForward(2 * time.Second)

	got, err := c.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after TTL expiry, got %v", got)
	}
}
