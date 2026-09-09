package api

import (
	"context"
	"sync"
	"time"
)

// fakeLimiter is a hand-rolled ratelimit.Limiter double for handler
// tests — in-memory, no real Redis. allowFn lets a test force a specific
// answer; the default (nil) always allows, matching every existing test
// that doesn't care about rate limiting.
type fakeLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	allowFn func(key string, limit int) bool
}

func newFakeLimiter() *fakeLimiter {
	return &fakeLimiter{counts: make(map[string]int)}
}

func (f *fakeLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[key]++
	if f.allowFn != nil {
		return f.allowFn(key, limit), nil
	}
	return f.counts[key] <= limit, nil
}
