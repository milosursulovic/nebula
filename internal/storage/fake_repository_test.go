package storage

import (
	"context"
	"sync"
	"time"
)

type fakeRepository struct {
	mu    sync.Mutex
	disks map[string]Disk
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{disks: make(map[string]Disk)}
}

func (f *fakeRepository) Create(ctx context.Context, d Disk) (Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	d.CreatedAt, d.UpdatedAt = now, now
	f.disks[d.ID] = d
	return d, nil
}

func (f *fakeRepository) Get(ctx context.Context, id string) (Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.disks[id]
	if !ok {
		return Disk{}, errNoRows
	}
	return d, nil
}

func (f *fakeRepository) ListByTenant(ctx context.Context, tenantID string) ([]Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var disks []Disk
	for _, d := range f.disks {
		if d.TenantID == tenantID {
			disks = append(disks, d)
		}
	}
	return disks, nil
}

func (f *fakeRepository) ListByInstance(ctx context.Context, instanceID string) ([]Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var disks []Disk
	for _, d := range f.disks {
		if d.InstanceID != nil && *d.InstanceID == instanceID {
			disks = append(disks, d)
		}
	}
	return disks, nil
}

func (f *fakeRepository) GetRootByInstance(ctx context.Context, instanceID string) (Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, d := range f.disks {
		if d.InstanceID != nil && *d.InstanceID == instanceID && d.Type == TypeRoot {
			return d, nil
		}
	}
	return Disk{}, errNoRows
}

func (f *fakeRepository) Attach(ctx context.Context, id, instanceID string) (Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.disks[id]
	if !ok || d.InstanceID != nil {
		return Disk{}, errNoRows
	}
	d.InstanceID = &instanceID
	d.UpdatedAt = time.Now()
	f.disks[id] = d
	return d, nil
}

func (f *fakeRepository) Detach(ctx context.Context, id string) (Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.disks[id]
	if !ok || d.InstanceID == nil {
		return Disk{}, errNoRows
	}
	d.InstanceID = nil
	d.UpdatedAt = time.Now()
	f.disks[id] = d
	return d, nil
}

func (f *fakeRepository) Resize(ctx context.Context, id string, newSizeGB int) (Disk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.disks[id]
	if !ok || newSizeGB < d.SizeGB {
		return Disk{}, errNoRows
	}
	d.SizeGB = newSizeGB
	d.UpdatedAt = time.Now()
	f.disks[id] = d
	return d, nil
}

func (f *fakeRepository) Delete(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.disks[id]
	if !ok || d.InstanceID != nil {
		return errNoRows
	}
	delete(f.disks, id)
	return nil
}
