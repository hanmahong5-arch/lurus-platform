package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// OverviewCache caches aggregated account overview data in Redis with a configurable TTL.
type OverviewCache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewOverviewCache creates a new overview cache backed by the given Redis client.
func NewOverviewCache(rdb *redis.Client, ttl time.Duration) *OverviewCache {
	return &OverviewCache{rdb: rdb, ttl: ttl}
}

func overviewKey(accountID int64, productID string) string {
	return fmt.Sprintf("identity:overview:%d:%s", accountID, productID)
}

// Get retrieves the cached overview bytes. Returns nil, nil on a cache miss.
func (c *OverviewCache) Get(ctx context.Context, accountID int64, productID string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, overviewKey(accountID, productID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("overview cache get: %w", err)
	}
	return val, nil
}

// Set stores serialized overview bytes with TTL.
func (c *OverviewCache) Set(ctx context.Context, accountID int64, productID string, data []byte) error {
	return c.rdb.Set(ctx, overviewKey(accountID, productID), data, c.ttl).Err()
}

// Invalidate removes the cached entry for the given account + product pair.
func (c *OverviewCache) Invalidate(ctx context.Context, accountID int64, productID string) error {
	return c.rdb.Del(ctx, overviewKey(accountID, productID)).Err()
}
