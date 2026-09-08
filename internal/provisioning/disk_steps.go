package provisioning

import (
	"context"
	"fmt"

	"github.com/milosursulovic/nebula/internal/storage"
)

// DiskSteps implements the disk saga step by creating/removing a real
// ROOT disk via internal/storage (Phase 13) — replacing the mock
// CreateDisk/DeleteDisk Phase 8 left in mocks.go (now removed; every saga
// step is real as of this phase). No real libvirt <disk> device
// attachment happens (see the Phase 13 plan's scope boundary note) — this
// creates the real sparse file and its DB row, nothing more.
type DiskSteps struct {
	disks storage.Service
}

func NewDiskSteps(disks storage.Service) *DiskSteps {
	return &DiskSteps{disks: disks}
}

func (d *DiskSteps) CreateDisk(ctx context.Context, instanceID, tenantID, nodeID string, diskGB int) error {
	instanceIDCopy := instanceID
	if _, err := d.disks.CreateDisk(ctx, tenantID, nodeID, &instanceIDCopy, storage.TypeRoot, diskGB); err != nil {
		return fmt.Errorf("create root disk: %w", err)
	}
	return nil
}

func (d *DiskSteps) DeleteDisk(ctx context.Context, instanceID string) error {
	if err := d.disks.DeleteForInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("delete root disk: %w", err)
	}
	return nil
}
