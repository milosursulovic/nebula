package audit

import (
	"context"
	"sync"
)

// fakeRepository mimics the real ON CONFLICT (event_id) DO NOTHING
// behavior in memory.
type fakeRepository struct {
	mu      sync.Mutex
	byEvent map[string]Record
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{byEvent: make(map[string]Record)}
}

func (f *fakeRepository) InsertIdempotent(ctx context.Context, r Record) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.byEvent[r.EventID]; exists {
		return false, nil
	}
	f.byEvent[r.EventID] = r
	return true, nil
}

func (f *fakeRepository) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byEvent)
}
