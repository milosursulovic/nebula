package job

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type fakeRepository struct {
	mu     sync.Mutex
	nextID int
	jobs   map[string]Job
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{jobs: make(map[string]Job)}
}

func (f *fakeRepository) newID() string {
	f.nextID++
	return fmt.Sprintf("job-%d", f.nextID)
}

func (f *fakeRepository) Create(ctx context.Context, j Job) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	j.ID = f.newID()
	j.CreatedAt = time.Now()
	j.NextAttemptAt = time.Now()
	if j.MaxAttempts == 0 {
		j.MaxAttempts = DefaultMaxAttempts
	}
	f.jobs[j.ID] = j
	return j, nil
}

func (f *fakeRepository) List(ctx context.Context, status *Status) ([]Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var result []Job
	for _, j := range f.jobs {
		if status == nil || j.Status == *status {
			result = append(result, j)
		}
	}
	return result, nil
}

func (f *fakeRepository) Get(ctx context.Context, id string) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	j, ok := f.jobs[id]
	if !ok {
		return Job{}, errNoRows
	}
	return j, nil
}

func (f *fakeRepository) ClaimNextDue(ctx context.Context) (Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	var best *Job
	for id, j := range f.jobs {
		if j.Status != StatusQueued || j.NextAttemptAt.After(now) {
			continue
		}
		if best == nil || j.NextAttemptAt.Before(best.NextAttemptAt) {
			jCopy := f.jobs[id]
			best = &jCopy
		}
	}
	if best == nil {
		return Job{}, false, nil
	}

	best.Status = StatusRunning
	started := now
	best.StartedAt = &started
	f.jobs[best.ID] = *best
	return *best, true, nil
}

func (f *fakeRepository) MarkSuccess(ctx context.Context, id string) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	j, ok := f.jobs[id]
	if !ok {
		return Job{}, errNoRows
	}
	j.Status = StatusSuccess
	now := time.Now()
	j.FinishedAt = &now
	j.Error = nil
	f.jobs[id] = j
	return j, nil
}

func (f *fakeRepository) MarkQueuedForRetry(ctx context.Context, id string, attempts int, errMsg string, nextAttemptAt time.Time) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	j, ok := f.jobs[id]
	if !ok {
		return Job{}, errNoRows
	}
	j.Status = StatusQueued
	j.Attempts = attempts
	j.Error = &errMsg
	j.NextAttemptAt = nextAttemptAt
	f.jobs[id] = j
	return j, nil
}

func (f *fakeRepository) MarkFailed(ctx context.Context, id string, attempts int, errMsg string) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	j, ok := f.jobs[id]
	if !ok {
		return Job{}, errNoRows
	}
	j.Status = StatusFailed
	j.Attempts = attempts
	j.Error = &errMsg
	now := time.Now()
	j.FinishedAt = &now
	f.jobs[id] = j
	return j, nil
}

func (f *fakeRepository) RequeueOrphanedRunning(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var count int64
	for id, j := range f.jobs {
		if j.Status == StatusRunning {
			j.Status = StatusQueued
			f.jobs[id] = j
			count++
		}
	}
	return count, nil
}

func (f *fakeRepository) ResetForRetry(ctx context.Context, id string) (Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	j, ok := f.jobs[id]
	if !ok || j.Status != StatusFailed {
		return Job{}, false, nil
	}
	j.Status = StatusQueued
	j.Attempts = 0
	j.Error = nil
	j.NextAttemptAt = time.Now()
	j.StartedAt = nil
	j.FinishedAt = nil
	f.jobs[id] = j
	return j, true, nil
}
