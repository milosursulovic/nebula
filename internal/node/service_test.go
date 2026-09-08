package node

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService() Service {
	return NewService(newFakeRepository(), testLogger())
}

func TestRegisterNode(t *testing.T) {
	svc := newTestService()

	result, err := svc.Register(context.Background(), RegisterInput{
		Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if result.NodeToken == "" {
		t.Fatal("expected a non-empty node token")
	}
	if result.Node.Status != StatusOnline {
		t.Errorf("Status = %q, want %q", result.Node.Status, StatusOnline)
	}
	if result.Node.AvailableCPU != 16 || result.Node.AvailableMemoryMB != 32768 || result.Node.AvailableDiskGB != 1000 {
		t.Errorf("expected available resources to equal totals at registration, got %+v", result.Node)
	}
	if result.Node.LastHeartbeatAt == nil {
		t.Error("expected LastHeartbeatAt to be set at registration")
	}
}

func TestRegisterDuplicateHostname(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	in := RegisterInput{Hostname: "compute-01", IP: "10.0.0.11", CPU: 4, MemoryMB: 8192, DiskGB: 100}

	if _, err := svc.Register(ctx, in); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err := svc.Register(ctx, in)
	if !errors.Is(err, ErrHostnameTaken) {
		t.Fatalf("Register duplicate = %v, want ErrHostnameTaken", err)
	}
}

func TestGetNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.Get(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestListReturnsRegisteredNodes(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, RegisterInput{Hostname: "compute-01", IP: "10.0.0.11", CPU: 4, MemoryMB: 8192, DiskGB: 100}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Hostname: "compute-02", IP: "10.0.0.12", CPU: 4, MemoryMB: 8192, DiskGB: 100}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	nodes, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("List returned %d nodes, want 2", len(nodes))
	}
}

func TestHeartbeatUpdatesNode(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterInput{Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.Heartbeat(ctx, result.Node.ID, HeartbeatInput{
		CPUUsage: 34.2, MemoryUsedMB: 12400, DiskUsedGB: 420, LoadAverage: 2.13, RunningInstances: 4,
	}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	updated, err := svc.Get(ctx, result.Node.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.LoadAverage != 2.13 || updated.RunningInstances != 4 {
		t.Errorf("unexpected node state after heartbeat: %+v", updated)
	}
	if updated.Status != StatusOnline {
		t.Errorf("Status = %q, want %q", updated.Status, StatusOnline)
	}
}

func TestDrainMarksNodeDraining(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterInput{Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	drained, err := svc.Drain(ctx, result.Node.ID)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if drained.Status != StatusDraining {
		t.Errorf("Status = %q, want %q", drained.Status, StatusDraining)
	}
}

func TestDrainNotFound(t *testing.T) {
	svc := newTestService()

	if _, err := svc.Drain(context.Background(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Drain unknown node: err = %v, want ErrNotFound", err)
	}
}

// TestHeartbeatPreservesDraining is the regression test for the bug found
// while implementing node drain (Phase 15): a DRAINING node's own agent
// keeps heartbeating every few seconds, and Heartbeat must not silently flip
// it back to ONLINE — that would make drain a no-op lie.
func TestHeartbeatPreservesDraining(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterInput{Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.Drain(ctx, result.Node.ID); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if err := svc.Heartbeat(ctx, result.Node.ID, HeartbeatInput{
		CPUUsage: 10, MemoryUsedMB: 1000, DiskUsedGB: 10, LoadAverage: 0.5, RunningInstances: 0,
	}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	updated, err := svc.Get(ctx, result.Node.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != StatusDraining {
		t.Errorf("Status after heartbeat = %q, want unchanged %q", updated.Status, StatusDraining)
	}
}

func TestAuthenticateNodeToken(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	result, err := svc.Register(ctx, RegisterInput{Hostname: "compute-01", IP: "10.0.0.11", CPU: 4, MemoryMB: 8192, DiskGB: 100})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.AuthenticateNodeToken(ctx, result.Node.ID, result.NodeToken); err != nil {
		t.Errorf("AuthenticateNodeToken with correct id/token: %v", err)
	}

	if err := svc.AuthenticateNodeToken(ctx, result.Node.ID, "wrong-token"); !errors.Is(err, ErrInvalidNodeToken) {
		t.Errorf("AuthenticateNodeToken with wrong token = %v, want ErrInvalidNodeToken", err)
	}

	if err := svc.AuthenticateNodeToken(ctx, "some-other-node-id", result.NodeToken); !errors.Is(err, ErrInvalidNodeToken) {
		t.Errorf("AuthenticateNodeToken with mismatched id = %v, want ErrInvalidNodeToken", err)
	}
}
