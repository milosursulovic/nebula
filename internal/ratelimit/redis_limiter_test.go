package ratelimit

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestRedisLimiterEnforcesLimit exercises the real Redis INCR/EXPIRE path
// — gated on NEBULA_REDIS_ADDR so `go test ./...` still passes standalone
// without Docker, same convention as the DB/Kafka-gated tests elsewhere
// (internal/node/reserve_concurrency_test.go et al.).
func TestRedisLimiterEnforcesLimit(t *testing.T) {
	addr := os.Getenv("NEBULA_REDIS_ADDR")
	if addr == "" {
		t.Skip("NEBULA_REDIS_ADDR not set; run `make compose-up` and re-run with it set (e.g. localhost:6379) to exercise this test")
	}

	l := NewRedisLimiter(addr)
	ctx := context.Background()
	key := fmt.Sprintf("test:%d", time.Now().UnixNano())

	for i := 1; i <= 3; i++ {
		ok, err := l.Allow(ctx, key, 3, time.Minute)
		if err != nil {
			t.Fatalf("Allow (request %d): %v", i, err)
		}
		if !ok {
			t.Fatalf("Allow (request %d): got false, want true (within limit)", i)
		}
	}

	ok, err := l.Allow(ctx, key, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow (4th request): %v", err)
	}
	if ok {
		t.Fatal("Allow (4th request): got true, want false (over limit)")
	}
}
