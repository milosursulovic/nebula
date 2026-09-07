package instance

import (
	"context"
	"errors"
)

// Service is the instance business logic surface consumed by pkg/api handlers.
type Service interface {
	Create(ctx context.Context, tenantID string, in CreateInput) (Instance, error)
	List(ctx context.Context, tenantID string) ([]Instance, error)
	Get(ctx context.Context, tenantID, id string) (Instance, error)
	Delete(ctx context.Context, tenantID, id string) (Instance, error)

	// Transition moves an instance to the given status if the state
	// machine allows it from its current status, returning
	// ErrInvalidTransition otherwise. This is the primitive the job
	// worker uses to drive PENDING -> PROVISIONING -> RUNNING.
	Transition(ctx context.Context, tenantID, id string, to Status) (Instance, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, tenantID string, in CreateInput) (Instance, error) {
	created, err := s.repo.Create(ctx, Instance{
		TenantID: tenantID,
		Name:     in.Name,
		Status:   StatusPending,
		CPU:      in.CPU,
		MemoryMB: in.MemoryMB,
		DiskGB:   in.DiskGB,
		Image:    in.Image,
	})
	if err != nil {
		return Instance{}, err // may be ErrNameTaken
	}
	return created, nil
}

func (s *service) List(ctx context.Context, tenantID string) ([]Instance, error) {
	return s.repo.List(ctx, tenantID)
}

func (s *service) Get(ctx context.Context, tenantID, id string) (Instance, error) {
	i, err := s.repo.Get(ctx, tenantID, id)
	if errors.Is(err, errNoRows) {
		return Instance{}, ErrNotFound
	}
	return i, err
}

// Delete soft-deletes an instance: PENDING/RUNNING/STOPPED/ERROR -> DELETING
// -> DELETED, applied synchronously (there's no job type for it yet — spec
// section 60's job system covers instance creation, not deletion).
func (s *service) Delete(ctx context.Context, tenantID, id string) (Instance, error) {
	if _, err := s.Transition(ctx, tenantID, id, StatusDeleting); err != nil {
		return Instance{}, err
	}
	return s.Transition(ctx, tenantID, id, StatusDeleted)
}

func (s *service) Transition(ctx context.Context, tenantID, id string, to Status) (Instance, error) {
	current, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return Instance{}, ErrNotFound
		}
		return Instance{}, err
	}

	if !CanTransition(current.Status, to) {
		return Instance{}, ErrInvalidTransition
	}

	updated, err := s.repo.TransitionState(ctx, tenantID, id, current.Status, to)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return Instance{}, ErrInvalidTransition // status changed concurrently
		}
		return Instance{}, err
	}

	return updated, nil
}
