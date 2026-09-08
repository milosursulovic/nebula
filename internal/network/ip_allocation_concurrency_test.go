package network

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
)

// TestAllocateConcurrent100Requests is the mandatory concurrency test from
// spec section 32: "Test: 100 goroutines -> allocate IP -> verify
// uniqueness." Exercises the real PostgreSQL SELECT ... FOR UPDATE SKIP
// LOCKED claim, not the in-memory fake — the same shape as
// internal/node/reserve_concurrency_test.go. Gated on
// NEBULA_DATABASE_URL so `go test ./...` still passes standalone without
// Docker; run `make compose-up` first to exercise it for real.
func TestAllocateConcurrent100Requests(t *testing.T) {
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
	svc := NewService(repo)

	const requests = 100
	// A /24 comfortably fits 100 concurrent allocations (253 usable
	// addresses) with room to spare.
	suffix := time.Now().UnixNano()
	_, _, err = svc.CreateNetwork(ctx, fmt.Sprintf("concurrency-test-%d", suffix), "10.77.0.0/24", "10.77.0.1")
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// ip_addresses.instance_id has a real FK to instances(id) (matches
	// jobs.instance_id's existing precedent) — synthetic non-UUID strings
	// would fail that constraint, so this test needs real instance rows
	// to allocate against, same as production callers always have.
	var tenantID string
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name) VALUES ($1) RETURNING id`,
		fmt.Sprintf("concurrency-test-tenant-%d", suffix)).Scan(&tenantID); err != nil {
		t.Fatalf("insert test tenant: %v", err)
	}
	t.Cleanup(func() {
		// ip_addresses rows still reference these instances (that's what
		// this test is proving), so unlink them before the instances (and
		// tenant, cascading) can be removed.
		pool.Exec(context.Background(), `UPDATE ip_addresses SET status = 'AVAILABLE', instance_id = NULL WHERE instance_id IN (SELECT id FROM instances WHERE tenant_id = $1)`, tenantID)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	instanceIDs := make([]string, requests)
	for i := 0; i < requests; i++ {
		err := pool.QueryRow(ctx, `
			INSERT INTO instances (tenant_id, name, status, cpu, memory_mb, disk_gb, image)
			VALUES ($1, $2, 'PROVISIONING', 1, 512, 10, 'test-image')
			RETURNING id`,
			tenantID, fmt.Sprintf("concurrency-inst-%d", i),
		).Scan(&instanceIDs[i])
		if err != nil {
			t.Fatalf("insert test instance %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	ips := make([]string, requests)
	errs := make([]error, requests)

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ips[i], errs[i] = svc.AllocateForInstance(ctx, instanceIDs[i])
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, requests)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: AllocateForInstance: %v", i, err)
		}
		if seen[ips[i]] {
			t.Fatalf("duplicate ip allocated: %q (request %d)", ips[i], i)
		}
		seen[ips[i]] = true
	}
	if len(seen) != requests {
		t.Fatalf("got %d unique ips, want %d", len(seen), requests)
	}
}
