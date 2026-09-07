package instance

import (
	"context"
	"errors"
	"testing"
)

func TestTransitionValidMove(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Transition(ctx, "tenant-1", created.ID, StatusProvisioning)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if updated.Status != StatusProvisioning {
		t.Errorf("Status = %q, want %q", updated.Status, StatusProvisioning)
	}

	updated, err = svc.Transition(ctx, "tenant-1", created.ID, StatusRunning)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if updated.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", updated.Status, StatusRunning)
	}
}

func TestTransitionRejectsInvalidMove(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Transition(ctx, "tenant-1", created.ID, StatusRunning)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Transition PENDING->RUNNING = %v, want ErrInvalidTransition", err)
	}
}

func TestTransitionFromWrongTenantNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Transition(ctx, "tenant-2", created.ID, StatusProvisioning)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Transition from wrong tenant = %v, want ErrNotFound", err)
	}
}

func TestSetNodeIDWhileProvisioning(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Transition(ctx, "tenant-1", created.ID, StatusProvisioning); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	updated, err := svc.SetNodeID(ctx, "tenant-1", created.ID, "node-1")
	if err != nil {
		t.Fatalf("SetNodeID: %v", err)
	}
	if updated.NodeID == nil || *updated.NodeID != "node-1" {
		t.Errorf("NodeID = %v, want node-1", updated.NodeID)
	}
}

func TestSetNodeIDRejectedOutsideProvisioning(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// Still PENDING - never transitioned to PROVISIONING.
	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.SetNodeID(ctx, "tenant-1", created.ID, "node-1")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("SetNodeID while PENDING = %v, want ErrInvalidTransition", err)
	}
}
