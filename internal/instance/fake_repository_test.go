package instance

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type fakeRepository struct {
	mu        sync.Mutex
	nextID    int
	instances map[string]Instance // by ID
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{instances: make(map[string]Instance)}
}

func (f *fakeRepository) newID() string {
	f.nextID++
	return fmt.Sprintf("instance-%d", f.nextID)
}

func (f *fakeRepository) CreateWithJob(ctx context.Context, in Instance) (Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.instances {
		if existing.TenantID == in.TenantID && existing.Name == in.Name {
			return Instance{}, ErrNameTaken
		}
	}

	in.ID = f.newID()
	now := time.Now()
	in.CreatedAt, in.UpdatedAt = now, now
	f.instances[in.ID] = in
	return in, nil
}

func (f *fakeRepository) List(ctx context.Context, tenantID string) ([]Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var result []Instance
	for _, i := range f.instances {
		if i.TenantID == tenantID {
			result = append(result, i)
		}
	}
	return result, nil
}

func (f *fakeRepository) Get(ctx context.Context, tenantID, id string) (Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i, ok := f.instances[id]
	if !ok || i.TenantID != tenantID {
		return Instance{}, errNoRows
	}
	return i, nil
}

func (f *fakeRepository) TransitionState(ctx context.Context, tenantID, id string, from, to Status) (Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i, ok := f.instances[id]
	if !ok || i.TenantID != tenantID || i.Status != from {
		return Instance{}, errNoRows
	}

	i.Status = to
	i.UpdatedAt = time.Now()
	f.instances[id] = i
	return i, nil
}
