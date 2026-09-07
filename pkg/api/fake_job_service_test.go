package api

import (
	"context"

	"github.com/milosursulovic/nebula/internal/job"
)

// fakeJobService is a hand-rolled job.Service double for handler tests.
type fakeJobService struct {
	enqueueFn func(ctx context.Context, jobType job.Type, tenantID, instanceID *string) (job.Job, error)
	listFn    func(ctx context.Context, status *job.Status) ([]job.Job, error)
	getFn     func(ctx context.Context, id string) (job.Job, error)
	retryFn   func(ctx context.Context, id string) (job.Job, error)
}

func (f fakeJobService) Enqueue(ctx context.Context, jobType job.Type, tenantID, instanceID *string) (job.Job, error) {
	if f.enqueueFn == nil {
		return job.Job{}, nil
	}
	return f.enqueueFn(ctx, jobType, tenantID, instanceID)
}

func (f fakeJobService) List(ctx context.Context, status *job.Status) ([]job.Job, error) {
	return f.listFn(ctx, status)
}

func (f fakeJobService) Get(ctx context.Context, id string) (job.Job, error) {
	return f.getFn(ctx, id)
}

func (f fakeJobService) Retry(ctx context.Context, id string) (job.Job, error) {
	return f.retryFn(ctx, id)
}
