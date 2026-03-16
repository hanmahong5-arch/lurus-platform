package cache

import (
	"context"
	"testing"
	"time"
)

func TestOverviewKey(t *testing.T) {
	tests := []struct {
		accountID int64
		productID string
		want      string
	}{
		{1, "llm-api", "identity:overview:1:llm-api"},
		{42, "", "identity:overview:42:"},
	}
	for _, tc := range tests {
		if got := overviewKey(tc.accountID, tc.productID); got != tc.want {
			t.Errorf("overviewKey(%d,%q)=%q, want %q", tc.accountID, tc.productID, got, tc.want)
		}
	}
}

func TestOverviewCache_Get_Miss(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewOverviewCache(rdb, 2*time.Minute)
	got, err := c.Get(context.Background(), 1, "llm-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on cache miss, got %v", got)
	}
}

func TestOverviewCache_SetAndGet(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewOverviewCache(rdb, 2*time.Minute)
	ctx := context.Background()

	data := []byte(`{"account":{"id":1},"vip":{"level":2}}`)
	if err := c.Set(ctx, 1, "llm-api", data); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := c.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestOverviewCache_Invalidate(t *testing.T) {
	_, rdb := newTestRedis(t)
	c := NewOverviewCache(rdb, 2*time.Minute)
	ctx := context.Background()

	_ = c.Set(ctx, 1, "llm-api", []byte("data"))
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

func TestOverviewCache_TTL_Expiry(t *testing.T) {
	mr, rdb := newTestRedis(t)
	c := NewOverviewCache(rdb, 1*time.Second)
	ctx := context.Background()

	_ = c.Set(ctx, 1, "llm-api", []byte("data"))
	mr.FastForward(2 * time.Second)

	got, err := c.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after TTL expiry, got %v", got)
	}
}
