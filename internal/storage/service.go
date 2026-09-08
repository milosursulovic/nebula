package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// AgentDiskClient is the only capability Service needs to reach a node's
// agent — a package-local, narrow interface (matches
// internal/scheduler.NodeLister's exact pattern: no cross-domain import,
// whatever already has these methods satisfies it structurally).
type AgentDiskClient interface {
	CreateDiskFile(ctx context.Context, nodeID, diskID string, sizeGB int) (string, error)
	DeleteDiskFile(ctx context.Context, nodeID, diskID string) error
	ResizeDiskFile(ctx context.Context, nodeID, diskID string, newSizeGB int) error
}

// Service is the storage business logic surface consumed by pkg/api
// handlers and internal/provisioning's saga steps.
type Service interface {
	// CreateDisk calls the agent to create the real file, then inserts
	// the row — both the saga's automatic ROOT disk and standalone
	// HTTP-created DATA/BACKUP disks go through this one path, so a real
	// file and its DB row are never created separately.
	CreateDisk(ctx context.Context, tenantID, nodeID string, instanceID *string, diskType Type, sizeGB int) (Disk, error)

	Get(ctx context.Context, id string) (Disk, error)
	ListByTenant(ctx context.Context, tenantID string) ([]Disk, error)
	ListByInstance(ctx context.Context, instanceID string) ([]Disk, error)

	// AttachDisk only succeeds if the disk is detached and instanceNodeID
	// matches the disk's own NodeID (a local file can't jump hosts).
	AttachDisk(ctx context.Context, diskID, instanceNodeID, instanceID string) (Disk, error)

	// DetachDisk rejects ROOT disks outright (not detachable while
	// attached, same as a cloud root volume) and disks that aren't
	// currently attached.
	DetachDisk(ctx context.Context, diskID string) (Disk, error)

	// ResizeDisk is grow-only; calls the agent then updates SizeGB.
	ResizeDisk(ctx context.Context, diskID string, newSizeGB int) (Disk, error)

	// DeleteDisk only succeeds if the disk is detached; calls the agent
	// then removes the row.
	DeleteDisk(ctx context.Context, diskID string) error

	// DeleteForInstance deletes instanceID's ROOT disk (agent file + row),
	// best-effort — used by the saga's compensation and by instance
	// delete. A no-op, not an error, if the instance has no ROOT disk.
	DeleteForInstance(ctx context.Context, instanceID string) error
}

type service struct {
	repo  Repository
	agent AgentDiskClient
}

func NewService(repo Repository, agent AgentDiskClient) Service {
	return &service{repo: repo, agent: agent}
}

func (s *service) CreateDisk(ctx context.Context, tenantID, nodeID string, instanceID *string, diskType Type, sizeGB int) (Disk, error) {
	// The ID is generated here, before the row exists, so the agent's
	// file can be named by the same ID the row ends up with.
	id := uuid.NewString()

	path, err := s.agent.CreateDiskFile(ctx, nodeID, id, sizeGB)
	if err != nil {
		return Disk{}, fmt.Errorf("agent create disk: %w", err)
	}

	return s.repo.Create(ctx, Disk{
		ID:         id,
		TenantID:   tenantID,
		InstanceID: instanceID,
		NodeID:     nodeID,
		Type:       diskType,
		SizeGB:     sizeGB,
		FilePath:   path,
	})
}

func (s *service) Get(ctx context.Context, id string) (Disk, error) {
	d, err := s.repo.Get(ctx, id)
	if errors.Is(err, errNoRows) {
		return Disk{}, ErrNotFound
	}
	return d, err
}

func (s *service) ListByTenant(ctx context.Context, tenantID string) ([]Disk, error) {
	return s.repo.ListByTenant(ctx, tenantID)
}

func (s *service) ListByInstance(ctx context.Context, instanceID string) ([]Disk, error) {
	return s.repo.ListByInstance(ctx, instanceID)
}

func (s *service) AttachDisk(ctx context.Context, diskID, instanceNodeID, instanceID string) (Disk, error) {
	d, err := s.Get(ctx, diskID)
	if err != nil {
		return Disk{}, err
	}
	if d.InstanceID != nil {
		return Disk{}, ErrAlreadyAttached
	}
	if d.NodeID != instanceNodeID {
		return Disk{}, ErrWrongNode
	}

	updated, err := s.repo.Attach(ctx, diskID, instanceID)
	if errors.Is(err, errNoRows) {
		return Disk{}, ErrAlreadyAttached // raced with a concurrent attach
	}
	return updated, err
}

func (s *service) DetachDisk(ctx context.Context, diskID string) (Disk, error) {
	d, err := s.Get(ctx, diskID)
	if err != nil {
		return Disk{}, err
	}
	if d.Type == TypeRoot {
		return Disk{}, ErrRootDiskNotDetachable
	}
	if d.InstanceID == nil {
		return Disk{}, ErrNotAttached
	}

	updated, err := s.repo.Detach(ctx, diskID)
	if errors.Is(err, errNoRows) {
		return Disk{}, ErrNotAttached // raced with a concurrent detach
	}
	return updated, err
}

func (s *service) ResizeDisk(ctx context.Context, diskID string, newSizeGB int) (Disk, error) {
	d, err := s.Get(ctx, diskID)
	if err != nil {
		return Disk{}, err
	}
	if newSizeGB < d.SizeGB {
		return Disk{}, ErrShrinkNotAllowed
	}
	if newSizeGB == d.SizeGB {
		return d, nil
	}

	if err := s.agent.ResizeDiskFile(ctx, d.NodeID, d.ID, newSizeGB); err != nil {
		return Disk{}, fmt.Errorf("agent resize disk: %w", err)
	}

	updated, err := s.repo.Resize(ctx, diskID, newSizeGB)
	if errors.Is(err, errNoRows) {
		return Disk{}, ErrShrinkNotAllowed // raced with a concurrent resize past newSizeGB
	}
	return updated, err
}

func (s *service) DeleteDisk(ctx context.Context, diskID string) error {
	d, err := s.Get(ctx, diskID)
	if err != nil {
		return err
	}
	if d.InstanceID != nil {
		return ErrStillAttached
	}

	if err := s.agent.DeleteDiskFile(ctx, d.NodeID, d.ID); err != nil {
		return fmt.Errorf("agent delete disk: %w", err)
	}

	if err := s.repo.Delete(ctx, diskID); err != nil {
		if errors.Is(err, errNoRows) {
			return ErrStillAttached // raced with a concurrent attach
		}
		return err
	}
	return nil
}

func (s *service) DeleteForInstance(ctx context.Context, instanceID string) error {
	d, err := s.repo.GetRootByInstance(ctx, instanceID)
	if errors.Is(err, errNoRows) {
		return nil // nothing to do
	}
	if err != nil {
		return err
	}

	if err := s.agent.DeleteDiskFile(ctx, d.NodeID, d.ID); err != nil {
		return fmt.Errorf("agent delete disk: %w", err)
	}

	// A ROOT disk is always attached (it's meaningless without its
	// instance), so it can't go through the detached-only Delete guard —
	// remove it directly.
	if _, err := s.repo.Detach(ctx, d.ID); err != nil {
		return fmt.Errorf("detach root disk before delete: %w", err)
	}
	return s.repo.Delete(ctx, d.ID)
}
