package api

import (
	"context"

	"github.com/milosursulovic/nebula/internal/storage"
)

// fakeStorageService is a hand-rolled storage.Service double for handler tests.
type fakeStorageService struct {
	createDiskFn        func(ctx context.Context, tenantID, nodeID string, instanceID *string, diskType storage.Type, sizeGB int) (storage.Disk, error)
	getFn               func(ctx context.Context, id string) (storage.Disk, error)
	listByTenantFn      func(ctx context.Context, tenantID string) ([]storage.Disk, error)
	listByInstanceFn    func(ctx context.Context, instanceID string) ([]storage.Disk, error)
	attachDiskFn        func(ctx context.Context, diskID, instanceNodeID, instanceID string) (storage.Disk, error)
	detachDiskFn        func(ctx context.Context, diskID string) (storage.Disk, error)
	resizeDiskFn        func(ctx context.Context, diskID string, newSizeGB int) (storage.Disk, error)
	deleteDiskFn        func(ctx context.Context, diskID string) error
	deleteForInstanceFn func(ctx context.Context, instanceID string) error
}

func (f fakeStorageService) CreateDisk(ctx context.Context, tenantID, nodeID string, instanceID *string, diskType storage.Type, sizeGB int) (storage.Disk, error) {
	return f.createDiskFn(ctx, tenantID, nodeID, instanceID, diskType, sizeGB)
}

func (f fakeStorageService) Get(ctx context.Context, id string) (storage.Disk, error) {
	return f.getFn(ctx, id)
}

func (f fakeStorageService) ListByTenant(ctx context.Context, tenantID string) ([]storage.Disk, error) {
	return f.listByTenantFn(ctx, tenantID)
}

func (f fakeStorageService) ListByInstance(ctx context.Context, instanceID string) ([]storage.Disk, error) {
	return f.listByInstanceFn(ctx, instanceID)
}

func (f fakeStorageService) AttachDisk(ctx context.Context, diskID, instanceNodeID, instanceID string) (storage.Disk, error) {
	return f.attachDiskFn(ctx, diskID, instanceNodeID, instanceID)
}

func (f fakeStorageService) DetachDisk(ctx context.Context, diskID string) (storage.Disk, error) {
	return f.detachDiskFn(ctx, diskID)
}

func (f fakeStorageService) ResizeDisk(ctx context.Context, diskID string, newSizeGB int) (storage.Disk, error) {
	return f.resizeDiskFn(ctx, diskID, newSizeGB)
}

func (f fakeStorageService) DeleteDisk(ctx context.Context, diskID string) error {
	return f.deleteDiskFn(ctx, diskID)
}

func (f fakeStorageService) DeleteForInstance(ctx context.Context, instanceID string) error {
	if f.deleteForInstanceFn != nil {
		return f.deleteForInstanceFn(ctx, instanceID)
	}
	return nil
}
