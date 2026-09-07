package node

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type fakeRepository struct {
	mu     sync.Mutex
	nextID int
	nodes  map[string]Node // by ID
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nodes: make(map[string]Node)}
}

func (f *fakeRepository) newID() string {
	f.nextID++
	return fmt.Sprintf("node-%d", f.nextID)
}

func (f *fakeRepository) Create(ctx context.Context, n Node) (Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.nodes {
		if existing.Hostname == n.Hostname {
			return Node{}, ErrHostnameTaken
		}
	}

	n.ID = f.newID()
	now := time.Now()
	n.CreatedAt, n.UpdatedAt = now, now
	f.nodes[n.ID] = n
	return n, nil
}

func (f *fakeRepository) List(ctx context.Context) ([]Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	nodes := make([]Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (f *fakeRepository) Get(ctx context.Context, id string) (Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n, ok := f.nodes[id]
	if !ok {
		return Node{}, errNoRows
	}
	return n, nil
}

func (f *fakeRepository) FindByTokenHash(ctx context.Context, tokenHash string) (Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, n := range f.nodes {
		if n.TokenHash == tokenHash {
			return n, nil
		}
	}
	return Node{}, errNoRows
}

func (f *fakeRepository) UpdateHeartbeat(ctx context.Context, id string, loadAverage float64, runningInstances int, status Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	n, ok := f.nodes[id]
	if !ok {
		return errNoRows
	}
	now := time.Now()
	n.LoadAverage = loadAverage
	n.RunningInstances = runningInstances
	n.Status = status
	n.LastHeartbeatAt = &now
	n.UpdatedAt = now
	f.nodes[id] = n
	return nil
}

func (f *fakeRepository) UpdateStatus(ctx context.Context, id string, status Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	n, ok := f.nodes[id]
	if !ok {
		return errNoRows
	}
	n.Status = status
	n.UpdatedAt = time.Now()
	f.nodes[id] = n
	return nil
}
