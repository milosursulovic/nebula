package agent

import (
	"context"
	"errors"
	"testing"
)

func TestMockHypervisorCreateAndGetVMStatus(t *testing.T) {
	h := NewMockHypervisor(NewStore())
	ctx := context.Background()

	if err := h.CreateVM(ctx, VMSpec{InstanceID: "inst-1", CPU: 2, MemoryMB: 4096, DiskGB: 50, Image: "img"}); err != nil {
		t.Fatalf("CreateVM: %v", err)
	}

	vm, err := h.GetVMStatus(ctx, "inst-1")
	if err != nil {
		t.Fatalf("GetVMStatus: %v", err)
	}
	if vm.Status != VMStatusStopped || vm.CPU != 2 || vm.MemoryMB != 4096 || vm.DiskGB != 50 || vm.Image != "img" {
		t.Errorf("unexpected VM: %+v", vm)
	}
}

func TestMockHypervisorGetVMStatusNotFound(t *testing.T) {
	h := NewMockHypervisor(NewStore())

	if _, err := h.GetVMStatus(context.Background(), "missing"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("err = %v, want ErrVMNotFound", err)
	}
}

func TestMockHypervisorStartStop(t *testing.T) {
	h := NewMockHypervisor(NewStore())
	ctx := context.Background()
	h.CreateVM(ctx, VMSpec{InstanceID: "inst-1", CPU: 1, MemoryMB: 1024, DiskGB: 10, Image: "img"})

	if err := h.StartVM(ctx, "inst-1"); err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	vm, _ := h.GetVMStatus(ctx, "inst-1")
	if vm.Status != VMStatusRunning {
		t.Errorf("status after start = %q, want RUNNING", vm.Status)
	}

	if err := h.StopVM(ctx, "inst-1"); err != nil {
		t.Fatalf("StopVM: %v", err)
	}
	vm, _ = h.GetVMStatus(ctx, "inst-1")
	if vm.Status != VMStatusStopped {
		t.Errorf("status after stop = %q, want STOPPED", vm.Status)
	}
}

func TestMockHypervisorStartNotFound(t *testing.T) {
	h := NewMockHypervisor(NewStore())

	if err := h.StartVM(context.Background(), "missing"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("err = %v, want ErrVMNotFound", err)
	}
}

func TestMockHypervisorDeleteVM(t *testing.T) {
	h := NewMockHypervisor(NewStore())
	ctx := context.Background()
	h.CreateVM(ctx, VMSpec{InstanceID: "inst-1", CPU: 1, MemoryMB: 1024, DiskGB: 10, Image: "img"})

	if err := h.DeleteVM(ctx, "inst-1"); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := h.GetVMStatus(ctx, "inst-1"); !errors.Is(err, ErrVMNotFound) {
		t.Error("expected vm to be gone after delete")
	}
}

func TestMockHypervisorDeleteVMUnknownIsNoop(t *testing.T) {
	h := NewMockHypervisor(NewStore())

	if err := h.DeleteVM(context.Background(), "missing"); err != nil {
		t.Fatalf("DeleteVM on unknown id: %v", err)
	}
}

func TestMockHypervisorCountVMs(t *testing.T) {
	h := NewMockHypervisor(NewStore())
	ctx := context.Background()

	count, err := h.CountVMs(ctx)
	if err != nil || count != 0 {
		t.Fatalf("initial CountVMs = %d, %v; want 0, nil", count, err)
	}

	h.CreateVM(ctx, VMSpec{InstanceID: "inst-1", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	h.CreateVM(ctx, VMSpec{InstanceID: "inst-2", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})

	count, err = h.CountVMs(ctx)
	if err != nil || count != 2 {
		t.Fatalf("CountVMs after 2 creates = %d, %v; want 2, nil", count, err)
	}
}
