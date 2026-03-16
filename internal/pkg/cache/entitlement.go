// Package cache provides Redis-backed caching for hot-path entitlement lookups.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// EntitlementCache caches account entitlements in Redis with a configurable TTL.
type EntitlementCache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewEntitlementCache creates a new cache backed by the given Redis client.
func NewEntitlementCache(rdb *redis.Client, ttl time.Duration) *EntitlementCache {
	return &EntitlementCache{rdb: rdb, ttl: ttl}
}

// EntitlementMap is a key→value map for a single account+product pair.
type EntitlementMap map[string]string

func cacheKey(accountID int64, productID string) string {
	return fmt.Sprintf("identity:entitlements:%d:%s", accountID, productID)
}

// Get retrieves the cached entitlement map. Returns nil if not cached.
func (c *EntitlementCache) Get(ctx context.Context, accountID int64, productID string) (map[string]string, error) {
	val, err := c.rdb.Get(ctx, cacheKey(accountID, productID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cache get: %w", err)
	}
	var em map[string]string
	if err := json.Unmarshal([]byte(val), &em); err != nil {
		return nil, fmt.Errorf("cache unmarshal: %w", err)
	}
	return em, nil
}

// Set stores the entitlement map with TTL.
func (c *EntitlementCache) Set(ctx context.Context, accountID int64, productID string, em map[string]string) error {
	b, err := json.Marshal(em)
	if err != nil {
		return fmt.Errorf("cache marshal: %w", err)
	}
	return c.rdb.Set(ctx, cacheKey(accountID, productID), b, c.ttl).Err()
}

// Invalidate removes the cached entry for a given account+product.
func (c *EntitlementCache) Invalidate(ctx context.Context, accountID int64, productID string) error {
	return c.rdb.Del(ctx, cacheKey(accountID, productID)).Err()
}

// InvalidateAccount removes all cached entries for a given account.
func (c *EntitlementCache) InvalidateAccount(ctx context.Context, accountID int64) error {
	pattern := fmt.Sprintf("identity:entitlements:%d:*", accountID)
	keys, err := c.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("cache keys scan: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}
