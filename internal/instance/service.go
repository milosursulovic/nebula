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
// -> DELETED, applied synchronously since no job worker exists yet to do it
// asynchronously (spec section 58: instances are virtual records this phase).
func (s *service) Delete(ctx context.Context, tenantID, id string) (Instance, error) {
	current, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return Instance{}, ErrNotFound
		}
		return Instance{}, err
	}

	if !CanTransition(current.Status, StatusDeleting) {
		return Instance{}, ErrInvalidTransition
	}

	if _, err := s.repo.TransitionState(ctx, tenantID, id, current.Status, StatusDeleting); err != nil {
		if errors.Is(err, errNoRows) {
			return Instance{}, ErrInvalidTransition // status changed concurrently
		}
		return Instance{}, err
	}

	deleted, err := s.repo.TransitionState(ctx, tenantID, id, StatusDeleting, StatusDeleted)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return Instance{}, ErrInvalidTransition
		}
		return Instance{}, err
	}

	return deleted, nil
}
