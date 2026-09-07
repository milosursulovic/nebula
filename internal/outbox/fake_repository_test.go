package outbox

import (
	"context"
	"sync"
)

type fakeRepository struct {
	mu     sync.Mutex
	events map[string]Event
	order  []string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{events: make(map[string]Event)}
}

func (f *fakeRepository) add(e Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events[e.ID] = e
	f.order = append(f.order, e.ID)
}

func (f *fakeRepository) ListUnpublished(ctx context.Context, limit int) ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var result []Event
	for _, id := range f.order {
		if len(result) >= limit {
			break
		}
		e := f.events[id]
		if e.PublishedAt == nil {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeRepository) MarkPublished(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	e, ok := f.events[id]
	if !ok {
		return nil
	}
	now := e.CreatedAt // any non-nil marker is enough for tests
	e.PublishedAt = &now
	f.events[id] = e
	return nil
}
