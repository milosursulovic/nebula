package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
)

// setupBenchNode registers a throwaway node with plenty of headroom (each
// benchmark iteration reserves then releases the same amount, so net
// consumption is zero — headroom just needs to exceed one request, not
// scale with b.N) and returns a cleanup func. Same real-Postgres pattern
// as reserve_concurrency_test.go, gated the same way.
func setupBenchNode(b *testing.B) (context.Context, Service, string, func()) {
	b.Helper()
	dsn := os.Getenv("NEBULA_DATABASE_URL")
	if dsn == "" {
		b.Skip("NEBULA_DATABASE_URL not set; run `make compose-up` and re-run with it set to exercise this benchmark")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	pool, err := common.NewPool(ctx, dsn)
	if err != nil {
		cancel()
		b.Fatalf("connect to database: %v", err)
	}

	repo := NewRepository(pool)
	svc := NewService(repo, testLogger())

	result, err := svc.Register(ctx, RegisterInput{
		Hostname: fmt.Sprintf("bench-reserve-%d", time.Now().UnixNano()),
		IP:       "10.0.0.98",
		CPU:      64, MemoryMB: 131072, DiskGB: 5000,
	})
	if err != nil {
		pool.Close()
		cancel()
		b.Fatalf("Register: %v", err)
	}
	nodeID := result.Node.ID

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM compute_nodes WHERE id = $1`, nodeID); err != nil {
			b.Logf("cleanup: failed to delete bench node %s: %v", nodeID, err)
		}
		pool.Close()
		cancel()
	}
	return ctx, svc, nodeID, cleanup
}

// BenchmarkReserveRelease times the real optimistic-concurrency CAS loop
// (node.Repository's version-column compare-and-swap, spec section 17)
// sequentially — one reserve+release cycle per iteration, no contention.
func BenchmarkReserveRelease(b *testing.B) {
	ctx, svc, nodeID, cleanup := setupBenchNode(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Reserve(ctx, nodeID, 1, 512, 10); err != nil {
			b.Fatalf("Reserve: %v", err)
		}
		if _, err := svc.Release(ctx, nodeID, 1, 512, 10); err != nil {
			b.Fatalf("Release: %v", err)
		}
	}
}

// BenchmarkReserveReleaseParallel is the same cycle under real contention
// (multiple goroutines racing the same node's version column, continuously
// for the whole benchmark run — a harsher, sustained version of the
// mandatory concurrency test's single 100-goroutine burst,
// internal/node/reserve_concurrency_test.go) — measures how the CAS retry
// loop (internal/node/service.go's maxReservationRetries=50) holds up
// under load, not just single-threaded throughput. Under this level of
// sustained single-row contention, Reserve/Release's own internal 50-
// attempt budget can occasionally exhaust and return
// ErrReservationConflict — a legitimate outcome (the same error real
// callers already handle), not a benchmark bug, so this retries at the
// outer level and reports how often that outer retry was needed rather
// than failing the run.
func BenchmarkReserveReleaseParallel(b *testing.B) {
	ctx, svc, nodeID, cleanup := setupBenchNode(b)
	defer cleanup()

	var outerRetries int64

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for {
				if _, err := svc.Reserve(ctx, nodeID, 1, 512, 10); err != nil {
					if errors.Is(err, ErrReservationConflict) {
						atomic.AddInt64(&outerRetries, 1)
						continue
					}
					b.Fatalf("Reserve: %v", err)
				}
				break
			}
			for {
				if _, err := svc.Release(ctx, nodeID, 1, 512, 10); err != nil {
					if errors.Is(err, ErrReservationConflict) {
						atomic.AddInt64(&outerRetries, 1)
						continue
					}
					b.Fatalf("Release: %v", err)
				}
				break
			}
		}
	})
	b.ReportMetric(float64(atomic.LoadInt64(&outerRetries))/float64(b.N), "exhausted-cas-budget/op")
}
