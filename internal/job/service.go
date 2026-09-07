package job

import (
	"context"
	"errors"
)

// Service is the job business logic surface consumed by pkg/api handlers
// and by whatever enqueues jobs (currently instance creation).
type Service interface {
	Enqueue(ctx context.Context, jobType Type, tenantID, instanceID *string) (Job, error)
	List(ctx context.Context, status *Status) ([]Job, error)
	Get(ctx context.Context, id string) (Job, error)

	// Retry moves a FAILED job back to QUEUED with a fresh attempt budget
	// (spec section 25's admin retry). ErrNotFailed if it isn't FAILED.
	Retry(ctx context.Context, id string) (Job, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Enqueue(ctx context.Context, jobType Type, tenantID, instanceID *string) (Job, error) {
	return s.repo.Create(ctx, Job{
		Type:        jobType,
		Status:      StatusQueued,
		TenantID:    tenantID,
		InstanceID:  instanceID,
		MaxAttempts: DefaultMaxAttempts,
	})
}

func (s *service) List(ctx context.Context, status *Status) ([]Job, error) {
	return s.repo.List(ctx, status)
}

func (s *service) Get(ctx context.Context, id string) (Job, error) {
	j, err := s.repo.Get(ctx, id)
	if errors.Is(err, errNoRows) {
		return Job{}, ErrNotFound
	}
	return j, err
}

func (s *service) Retry(ctx context.Context, id string) (Job, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if current.Status != StatusFailed {
		return Job{}, ErrNotFailed
	}

	retried, ok, err := s.repo.ResetForRetry(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if !ok {
		return Job{}, ErrNotFailed // status changed concurrently since the Get above
	}
	return retried, nil
}
