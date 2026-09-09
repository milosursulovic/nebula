// Package ratelimit implements spec section 37's rate limiting — per-user
// and per-tenant request limits, Redis-backed so the counters are shared
// state (not per-process, which would silently under-count if nebula-api
// ever ran more than one replica). Per-IP limiting is spec's own
// explicitly-named "Eventually" item — not built here.
package ratelimit

import (
	"context"
	"time"
)

// Limiter enforces a fixed-window request limit per key.
type Limiter interface {
	// Allow reports whether one more request under key is allowed within
	// the current window, incrementing key's counter either way (a
	// rejected request still counts — this is a hard cap, not a
	// leaky/token bucket).
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}
