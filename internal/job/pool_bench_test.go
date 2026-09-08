package job

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
)

// BenchmarkJobCycle times the real row-locking claim primitive workers
// depend on (Repository.ClaimNextDue's SELECT ... FOR UPDATE SKIP LOCKED,
// spec section 21's worker pool) — one create+claim+mark-success cycle
// per iteration against real Postgres. Gated on NEBULA_DATABASE_URL, same
// pattern as internal/node/reserve_concurrency_test.go.
func BenchmarkJobCycle(b *testing.B) {
	dsn := os.Getenv("NEBULA_DATABASE_URL")
	if dsn == "" {
		b.Skip("NEBULA_DATABASE_URL not set; run `make compose-up` and re-run with it set to exercise this benchmark")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := common.NewPool(ctx, dsn)
	if err != nil {
		b.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		created, err := repo.Create(ctx, Job{
			Type: TypeCreateInstance, Status: StatusQueued, MaxAttempts: DefaultMaxAttempts,
		})
		if err != nil {
			b.Fatalf("Create: %v", err)
		}
		claimed, ok, err := repo.ClaimNextDue(ctx)
		if err != nil {
			b.Fatalf("ClaimNextDue: %v", err)
		}
		if !ok || claimed.ID != created.ID {
			b.Fatalf("ClaimNextDue: ok=%v claimed=%q, want the job just created (%q)", ok, claimed.ID, created.ID)
		}
		if _, err := repo.MarkSuccess(ctx, claimed.ID); err != nil {
			b.Fatalf("MarkSuccess: %v", err)
		}
	}
}
