package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// fakeAgentDiskClient is an in-memory AgentDiskClient double — proves
// Service's business rules without needing a real agent/gRPC connection.
type fakeAgentDiskClient struct {
	mu    sync.Mutex
	files map[string]int // diskID -> sizeGB
}

func newFakeAgentDiskClient() *fakeAgentDiskClient {
	return &fakeAgentDiskClient{files: make(map[string]int)}
}

func (f *fakeAgentDiskClient) CreateDiskFile(ctx context.Context, nodeID, diskID string, sizeGB int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[diskID] = sizeGB
	return "/var/lib/nebula/disks/" + diskID + ".img", nil
}

func (f *fakeAgentDiskClient) DeleteDiskFile(ctx context.Context, nodeID, diskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, diskID)
	return nil
}

func (f *fakeAgentDiskClient) ResizeDiskFile(ctx context.Context, nodeID, diskID string, newSizeGB int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[diskID]; !ok {
		return fmt.Errorf("no such disk file: %s", diskID)
	}
	f.files[diskID] = newSizeGB
	return nil
}

func newTestService() (Service, *fakeAgentDiskClient) {
	agent := newFakeAgentDiskClient()
	return NewService(newFakeRepository(), agent), agent
}

func TestCreateDiskAttachedToInstance(t *testing.T) {
	svc, agent := newTestService()
	instanceID := "inst-1"

	d, err := svc.CreateDisk(context.Background(), "tenant-1", "node-1", &instanceID, TypeRoot, 50)
	if err != nil {
		t.Fatalf("CreateDisk: %v", err)
	}
	if d.InstanceID == nil || *d.InstanceID != instanceID {
		t.Errorf("InstanceID = %v, want %q", d.InstanceID, instanceID)
	}
	if _, ok := agent.files[d.ID]; !ok {
		t.Error("expected agent.CreateDiskFile to have been called")
	}
}

func TestCreateDiskDetached(t *testing.T) {
	svc, _ := newTestService()

	d, err := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)
	if err != nil {
		t.Fatalf("CreateDisk: %v", err)
	}
	if d.InstanceID != nil {
		t.Errorf("InstanceID = %v, want nil (detached)", d.InstanceID)
	}
}

func TestAttachDetachRoundTrip(t *testing.T) {
	svc, _ := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)

	attached, err := svc.AttachDisk(context.Background(), d.ID, "node-1", "inst-1")
	if err != nil {
		t.Fatalf("AttachDisk: %v", err)
	}
	if attached.InstanceID == nil || *attached.InstanceID != "inst-1" {
		t.Errorf("unexpected attach result: %+v", attached)
	}

	detached, err := svc.DetachDisk(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("DetachDisk: %v", err)
	}
	if detached.InstanceID != nil {
		t.Errorf("expected InstanceID nil after detach, got %v", detached.InstanceID)
	}
}

func TestAttachAlreadyAttached(t *testing.T) {
	svc, _ := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)
	svc.AttachDisk(context.Background(), d.ID, "node-1", "inst-1")

	if _, err := svc.AttachDisk(context.Background(), d.ID, "node-1", "inst-2"); !errors.Is(err, ErrAlreadyAttached) {
		t.Errorf("err = %v, want ErrAlreadyAttached", err)
	}
}

func TestAttachWrongNode(t *testing.T) {
	svc, _ := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)

	if _, err := svc.AttachDisk(context.Background(), d.ID, "node-2", "inst-1"); !errors.Is(err, ErrWrongNode) {
		t.Errorf("err = %v, want ErrWrongNode", err)
	}
}

func TestDetachNotAttached(t *testing.T) {
	svc, _ := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)

	if _, err := svc.DetachDisk(context.Background(), d.ID); !errors.Is(err, ErrNotAttached) {
		t.Errorf("err = %v, want ErrNotAttached", err)
	}
}

func TestDetachRootDiskRejected(t *testing.T) {
	svc, _ := newTestService()
	instanceID := "inst-1"
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", &instanceID, TypeRoot, 50)

	if _, err := svc.DetachDisk(context.Background(), d.ID); !errors.Is(err, ErrRootDiskNotDetachable) {
		t.Errorf("err = %v, want ErrRootDiskNotDetachable", err)
	}
}

func TestResizeGrow(t *testing.T) {
	svc, agent := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)

	resized, err := svc.ResizeDisk(context.Background(), d.ID, 30)
	if err != nil {
		t.Fatalf("ResizeDisk: %v", err)
	}
	if resized.SizeGB != 30 {
		t.Errorf("SizeGB = %d, want 30", resized.SizeGB)
	}
	if agent.files[d.ID] != 30 {
		t.Errorf("agent file size = %d, want 30", agent.files[d.ID])
	}
}

func TestResizeShrinkRejected(t *testing.T) {
	svc, _ := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)

	if _, err := svc.ResizeDisk(context.Background(), d.ID, 10); !errors.Is(err, ErrShrinkNotAllowed) {
		t.Errorf("err = %v, want ErrShrinkNotAllowed", err)
	}
}

func TestDeleteDetachedDisk(t *testing.T) {
	svc, agent := newTestService()
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", nil, TypeData, 20)

	if err := svc.DeleteDisk(context.Background(), d.ID); err != nil {
		t.Fatalf("DeleteDisk: %v", err)
	}
	if _, ok := agent.files[d.ID]; ok {
		t.Error("expected agent.DeleteDiskFile to have been called")
	}
	if _, err := svc.Get(context.Background(), d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteAttachedDiskRejected(t *testing.T) {
	svc, _ := newTestService()
	instanceID := "inst-1"
	d, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", &instanceID, TypeData, 20)

	if err := svc.DeleteDisk(context.Background(), d.ID); !errors.Is(err, ErrStillAttached) {
		t.Errorf("err = %v, want ErrStillAttached", err)
	}
}

func TestDeleteForInstanceRemovesRootDisk(t *testing.T) {
	svc, agent := newTestService()
	instanceID := "inst-1"
	root, _ := svc.CreateDisk(context.Background(), "tenant-1", "node-1", &instanceID, TypeRoot, 50)

	if err := svc.DeleteForInstance(context.Background(), instanceID); err != nil {
		t.Fatalf("DeleteForInstance: %v", err)
	}
	if _, ok := agent.files[root.ID]; ok {
		t.Error("expected the root disk's agent file to be deleted")
	}
	if _, err := svc.Get(context.Background(), root.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after DeleteForInstance: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteForInstanceNoRootDiskIsNoop(t *testing.T) {
	svc, _ := newTestService()

	if err := svc.DeleteForInstance(context.Background(), "never-had-a-disk"); err != nil {
		t.Fatalf("DeleteForInstance with no root disk: %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	svc, _ := newTestService()

	if _, err := svc.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
