package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisLimiter struct {
	client *redis.Client
}

// NewRedisLimiter returns a Limiter backed by Redis at addr (host:port).
func NewRedisLimiter(addr string) Limiter {
	return &redisLimiter{client: redis.NewClient(&redis.Options{Addr: addr})}
}

// Allow is the standard fixed-window counter pattern: INCR the key, and
// on the very first hit in a fresh window (count == 1, meaning this INCR
// is what created the key), set its expiry to window so it resets on its
// own — no separate cleanup process needed. INCR is atomic in Redis, so
// concurrent requests racing the same key still count correctly.
func (l *redisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("ratelimit: incr %s: %w", key, err)
	}
	if count == 1 {
		if err := l.client.Expire(ctx, key, window).Err(); err != nil {
			return false, fmt.Errorf("ratelimit: expire %s: %w", key, err)
		}
	}
	return count <= int64(limit), nil
}
