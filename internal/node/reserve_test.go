package node

import (
	"context"
	"errors"
	"testing"
)

func registerTestNode(t *testing.T, svc Service) Node {
	t.Helper()
	result, err := svc.Register(context.Background(), RegisterInput{
		Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return result.Node
}

func TestReserveSuccess(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	n := registerTestNode(t, svc)

	updated, err := svc.Reserve(ctx, n.ID, 4, 8192, 100)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if updated.AvailableCPU != 12 || updated.AvailableMemoryMB != 24576 || updated.AvailableDiskGB != 900 {
		t.Errorf("unexpected available resources after reserve: %+v", updated)
	}
	if updated.Version != n.Version+1 {
		t.Errorf("Version = %d, want %d", updated.Version, n.Version+1)
	}
}

func TestReserveInsufficientCapacity(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	n := registerTestNode(t, svc)

	_, err := svc.Reserve(ctx, n.ID, 100, 8192, 100)
	if !errors.Is(err, ErrInsufficientCapacity) {
		t.Fatalf("Reserve over capacity = %v, want ErrInsufficientCapacity", err)
	}
}

func TestReleaseReturnsCapacity(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	n := registerTestNode(t, svc)

	if _, err := svc.Reserve(ctx, n.ID, 4, 8192, 100); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	released, err := svc.Release(ctx, n.ID, 4, 8192, 100)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released.AvailableCPU != n.AvailableCPU || released.AvailableMemoryMB != n.AvailableMemoryMB || released.AvailableDiskGB != n.AvailableDiskGB {
		t.Errorf("expected released capacity to match original, got %+v", released)
	}
}

// flakyOnceRepository wraps a Repository and makes the first TryReserve
// call report a (simulated) version conflict, regardless of the version
// passed in, then delegates normally afterward — proving Service.Reserve's
// retry loop actually recovers from a concurrent-writer race.
type flakyOnceRepository struct {
	Repository
	reserveCalls int
}

func (f *flakyOnceRepository) TryReserve(ctx context.Context, id string, expectedVersion int64, cpu, memoryMB, diskGB int) (Node, bool, error) {
	f.reserveCalls++
	if f.reserveCalls == 1 {
		return Node{}, false, nil // simulate a lost race on the first attempt
	}
	return f.Repository.TryReserve(ctx, id, expectedVersion, cpu, memoryMB, diskGB)
}

func TestReserveRetriesOnConflict(t *testing.T) {
	base := newFakeRepository()
	flaky := &flakyOnceRepository{Repository: base}
	svc := NewService(flaky, testLogger())
	ctx := context.Background()
	n := registerTestNode(t, svc)

	updated, err := svc.Reserve(ctx, n.ID, 4, 8192, 100)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if flaky.reserveCalls < 2 {
		t.Fatalf("expected at least 2 TryReserve attempts (one simulated conflict + retry), got %d", flaky.reserveCalls)
	}
	if updated.AvailableCPU != 12 {
		t.Errorf("AvailableCPU = %d, want 12", updated.AvailableCPU)
	}
}
