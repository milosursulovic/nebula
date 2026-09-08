//go:build libvirt

package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// newTestLibvirtHypervisor connects to a real libvirtd (default
// qemu:///system, overridable via NEBULA_TEST_LIBVIRT_URI) and skips
// cleanly if none is reachable — same gating shape as
// internal/node/reserve_concurrency_test.go's NEBULA_DATABASE_URL check,
// so `go test -tags libvirt ./...` never breaks a machine without
// libvirtd.
func newTestLibvirtHypervisor(t *testing.T) *LibvirtHypervisor {
	t.Helper()

	uri := getEnv("NEBULA_TEST_LIBVIRT_URI", "qemu:///system")
	hv, err := newLibvirtHypervisor(uri)
	if err != nil {
		t.Skipf("libvirtd not reachable at %q, skipping: %v", uri, err)
	}
	return hv.(*LibvirtHypervisor)
}

// TestLibvirtHypervisorLifecycle proves the real create -> start -> status
// -> stop -> delete lifecycle against actual KVM (spec section 65: "start
// with a single local Linux machine") — not just the mock.
func TestLibvirtHypervisorLifecycle(t *testing.T) {
	h := newTestLibvirtHypervisor(t)
	ctx := context.Background()
	instanceID := "nebula-test-" + uuid.NewString()

	t.Cleanup(func() {
		_ = h.DeleteVM(ctx, instanceID)
	})

	if err := h.CreateVM(ctx, VMSpec{InstanceID: instanceID, CPU: 1, MemoryMB: 128, DiskGB: 5, Image: "test-image"}); err != nil {
		t.Fatalf("CreateVM: %v", err)
	}

	vm, err := h.GetVMStatus(ctx, instanceID)
	if err != nil {
		t.Fatalf("GetVMStatus after create: %v", err)
	}
	if vm.Status != VMStatusStopped {
		t.Errorf("status after create = %q, want STOPPED", vm.Status)
	}
	if vm.DiskGB != 5 || vm.Image != "test-image" {
		t.Errorf("metadata round-trip: DiskGB=%d Image=%q, want 5, \"test-image\"", vm.DiskGB, vm.Image)
	}

	if err := h.StartVM(ctx, instanceID); err != nil {
		t.Fatalf("StartVM: %v", err)
	}

	vm, err = h.GetVMStatus(ctx, instanceID)
	if err != nil {
		t.Fatalf("GetVMStatus after start: %v", err)
	}
	if vm.Status != VMStatusRunning {
		t.Errorf("status after start = %q, want RUNNING", vm.Status)
	}
	if vm.CPU != 1 {
		t.Errorf("CPU = %d, want 1", vm.CPU)
	}
	if vm.MemoryMB != 128 {
		t.Errorf("MemoryMB = %d, want 128", vm.MemoryMB)
	}

	count, err := h.CountVMs(ctx)
	if err != nil {
		t.Fatalf("CountVMs: %v", err)
	}
	if count < 1 {
		t.Errorf("CountVMs = %d, want at least 1", count)
	}

	if err := h.StopVM(ctx, instanceID); err != nil {
		t.Fatalf("StopVM: %v", err)
	}

	vm, err = h.GetVMStatus(ctx, instanceID)
	if err != nil {
		t.Fatalf("GetVMStatus after stop: %v", err)
	}
	if vm.Status != VMStatusStopped {
		t.Errorf("status after stop = %q, want STOPPED", vm.Status)
	}

	if err := h.DeleteVM(ctx, instanceID); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}

	if _, err := h.GetVMStatus(ctx, instanceID); err != ErrVMNotFound {
		t.Errorf("GetVMStatus after delete: err = %v, want ErrVMNotFound", err)
	}
}

func TestLibvirtHypervisorDeleteUnknownIsNoop(t *testing.T) {
	h := newTestLibvirtHypervisor(t)

	if err := h.DeleteVM(context.Background(), "nebula-test-never-existed"); err != nil {
		t.Fatalf("DeleteVM on unknown domain: %v", err)
	}
}

func TestLibvirtHypervisorGetVMStatusNotFound(t *testing.T) {
	h := newTestLibvirtHypervisor(t)

	if _, err := h.GetVMStatus(context.Background(), "nebula-test-never-existed"); err != ErrVMNotFound {
		t.Errorf("err = %v, want ErrVMNotFound", err)
	}
}
