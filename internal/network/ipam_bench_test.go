package network

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
)

// setupBenchIPAM creates a throwaway network + one throwaway instance row
// (ip_addresses.instance_id has a real FK, same as
// ip_allocation_concurrency_test.go) and returns a cleanup func. Allocation
// is idempotent-by-instance-ID (Phase 12) — reusing the SAME instance ID
// for every iteration works because each iteration releases before the
// next allocates, so there's never an existing allocation to short-circuit
// against; a fresh SELECT ... FOR UPDATE SKIP LOCKED claim happens every
// time, same as production's real allocate/release cycle.
func setupBenchIPAM(b *testing.B) (context.Context, Service, string, func()) {
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
	svc := NewService(repo)

	suffix := time.Now().UnixNano()
	if _, _, err := svc.CreateNetwork(ctx, fmt.Sprintf("bench-ipam-%d", suffix), "10.78.0.0/24", "10.78.0.1"); err != nil {
		pool.Close()
		cancel()
		b.Fatalf("CreateNetwork: %v", err)
	}

	var tenantID string
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name) VALUES ($1) RETURNING id`,
		fmt.Sprintf("bench-ipam-tenant-%d", suffix)).Scan(&tenantID); err != nil {
		pool.Close()
		cancel()
		b.Fatalf("insert bench tenant: %v", err)
	}

	var instanceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO instances (tenant_id, name, status, cpu, memory_mb, disk_gb, image)
		VALUES ($1, 'bench-ipam-instance', 'PROVISIONING', 1, 512, 10, 'test-image')
		RETURNING id`, tenantID,
	).Scan(&instanceID); err != nil {
		pool.Close()
		cancel()
		b.Fatalf("insert bench instance: %v", err)
	}

	cleanup := func() {
		bg := context.Background()
		pool.Exec(bg, `UPDATE ip_addresses SET status = 'AVAILABLE', instance_id = NULL WHERE instance_id = $1`, instanceID)
		pool.Exec(bg, `DELETE FROM tenants WHERE id = $1`, tenantID) // cascades the instance row
		pool.Close()
		cancel()
	}
	return ctx, svc, instanceID, cleanup
}

// BenchmarkAllocateReleaseForInstance times the real
// SELECT ... FOR UPDATE SKIP LOCKED claim (spec section 32's mandatory
// IPAM row-locking, internal/network/repository.go) sequentially.
func BenchmarkAllocateReleaseForInstance(b *testing.B) {
	ctx, svc, instanceID, cleanup := setupBenchIPAM(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.AllocateForInstance(ctx, instanceID); err != nil {
			b.Fatalf("AllocateForInstance: %v", err)
		}
		if err := svc.ReleaseForInstance(ctx, instanceID); err != nil {
			b.Fatalf("ReleaseForInstance: %v", err)
		}
	}
}
