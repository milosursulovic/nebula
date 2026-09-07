package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
)

// TestReserveConcurrent100Requests is the mandatory concurrency test from
// spec section 19: 100 concurrent requests against a node with limited
// resources must never drive available_* negative, and total reserved
// must never exceed total capacity. Run with `go test -race`.
//
// This exercises the real PostgreSQL compare-and-swap (compute_nodes
// .version), not the in-memory fake — a bug in the actual SQL wouldn't be
// caught by the fake-repository-backed unit tests above. Gated on
// NEBULA_DATABASE_URL so `go test ./...` still passes standalone without
// Docker; run `make compose-up` first to exercise it for real.
func TestReserveConcurrent100Requests(t *testing.T) {
	dsn := os.Getenv("NEBULA_DATABASE_URL")
	if dsn == "" {
		t.Skip("NEBULA_DATABASE_URL not set; run `make compose-up` and re-run with it set to exercise this test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := common.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool)
	svc := NewService(repo, testLogger())

	const (
		totalCPU      = 16
		totalMemoryMB = 32768
		totalDiskGB   = 1000

		requestCPU      = 1
		requestMemoryMB = 512
		requestDiskGB   = 10

		concurrentRequests = 100
	)

	result, err := svc.Register(ctx, RegisterInput{
		Hostname: fmt.Sprintf("concurrency-test-%d", time.Now().UnixNano()),
		IP:       "10.0.0.99",
		CPU:      totalCPU,
		MemoryMB: totalMemoryMB,
		DiskGB:   totalDiskGB,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	nodeID := result.Node.ID
	defer func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM compute_nodes WHERE id = $1`, nodeID); err != nil {
			t.Logf("cleanup: failed to delete test node %s: %v", nodeID, err)
		}
	}()

	var (
		successes int64
		rejected  int64
		otherErrs int64
	)

	var wg sync.WaitGroup
	wg.Add(concurrentRequests)
	for i := 0; i < concurrentRequests; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Reserve(ctx, nodeID, requestCPU, requestMemoryMB, requestDiskGB)
			switch {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case errors.Is(err, ErrInsufficientCapacity):
				atomic.AddInt64(&rejected, 1)
			default:
				atomic.AddInt64(&otherErrs, 1)
				t.Logf("unexpected Reserve error: %v", err)
			}
		}()
	}
	wg.Wait()

	if otherErrs != 0 {
		t.Errorf("got %d unexpected errors (want only success or ErrInsufficientCapacity); see log above", otherErrs)
	}
	if successes+rejected+otherErrs != concurrentRequests {
		t.Errorf("successes(%d)+rejected(%d)+otherErrs(%d) = %d, want %d",
			successes, rejected, otherErrs, successes+rejected+otherErrs, concurrentRequests)
	}

	final, err := svc.Get(ctx, nodeID)
	if err != nil {
		t.Fatalf("Get final node state: %v", err)
	}

	if final.AvailableCPU < 0 || final.AvailableMemoryMB < 0 || final.AvailableDiskGB < 0 {
		t.Fatalf("negative available resources after concurrent reservations: %+v", final)
	}

	wantAvailableCPU := totalCPU - int(successes)*requestCPU
	wantAvailableMemory := totalMemoryMB - int(successes)*requestMemoryMB
	wantAvailableDisk := totalDiskGB - int(successes)*requestDiskGB

	if final.AvailableCPU != wantAvailableCPU {
		t.Errorf("AvailableCPU = %d, want %d (total %d - %d successes * %d)", final.AvailableCPU, wantAvailableCPU, totalCPU, successes, requestCPU)
	}
	if final.AvailableMemoryMB != wantAvailableMemory {
		t.Errorf("AvailableMemoryMB = %d, want %d", final.AvailableMemoryMB, wantAvailableMemory)
	}
	if final.AvailableDiskGB != wantAvailableDisk {
		t.Errorf("AvailableDiskGB = %d, want %d", final.AvailableDiskGB, wantAvailableDisk)
	}
	if int(successes)*requestCPU > totalCPU {
		t.Errorf("successes (%d) allowed more CPU reserved than total capacity (%d)", successes, totalCPU)
	}

	t.Logf("concurrent reservations: %d succeeded, %d rejected (insufficient capacity), final available_cpu=%d",
		successes, rejected, final.AvailableCPU)
}
