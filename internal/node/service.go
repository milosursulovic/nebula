package node

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
)

// RegisterResult is what Register hands back: the created node plus the
// raw node token (shown exactly once, only its hash is persisted).
type RegisterResult struct {
	Node      Node
	NodeToken string
}

// Service is the node business logic surface consumed by pkg/api handlers.
type Service interface {
	Register(ctx context.Context, in RegisterInput) (RegisterResult, error)
	List(ctx context.Context) ([]Node, error)
	Get(ctx context.Context, id string) (Node, error)
	Heartbeat(ctx context.Context, id string, in HeartbeatInput) error

	// AuthenticateNodeToken resolves a raw node token to its node ID, or
	// ErrInvalidNodeToken if the token is unknown or doesn't match id.
	AuthenticateNodeToken(ctx context.Context, id, rawToken string) error
}

type service struct {
	repo   Repository
	logger *slog.Logger
}

func NewService(repo Repository, logger *slog.Logger) Service {
	return &service{repo: repo, logger: logger}
}

func (s *service) Register(ctx context.Context, in RegisterInput) (RegisterResult, error) {
	rawToken, hash, err := common.GenerateOpaqueToken()
	if err != nil {
		return RegisterResult{}, err
	}

	now := time.Now()
	created, err := s.repo.Create(ctx, Node{
		Hostname:          in.Hostname,
		IP:                in.IP,
		Status:            StatusOnline,
		TotalCPU:          in.CPU,
		AvailableCPU:      in.CPU,
		TotalMemoryMB:     in.MemoryMB,
		AvailableMemoryMB: in.MemoryMB,
		TotalDiskGB:       in.DiskGB,
		AvailableDiskGB:   in.DiskGB,
		LastHeartbeatAt:   &now,
		TokenHash:         hash,
	})
	if err != nil {
		return RegisterResult{}, err // may be ErrHostnameTaken
	}

	return RegisterResult{Node: created, NodeToken: rawToken}, nil
}

func (s *service) List(ctx context.Context) ([]Node, error) {
	return s.repo.List(ctx)
}

func (s *service) Get(ctx context.Context, id string) (Node, error) {
	n, err := s.repo.Get(ctx, id)
	if errors.Is(err, errNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

func (s *service) Heartbeat(ctx context.Context, id string, in HeartbeatInput) error {
	if err := s.repo.UpdateHeartbeat(ctx, id, in.LoadAverage, in.RunningInstances, StatusOnline); err != nil {
		return err
	}
	s.logger.Info("node heartbeat received",
		"node_id", id,
		"cpu_usage", in.CPUUsage,
		"memory_used_mb", in.MemoryUsedMB,
		"disk_used_gb", in.DiskUsedGB,
		"load_average", in.LoadAverage,
		"running_instances", in.RunningInstances,
	)
	return nil
}

func (s *service) AuthenticateNodeToken(ctx context.Context, id, rawToken string) error {
	n, err := s.repo.FindByTokenHash(ctx, common.HashToken(rawToken))
	if err != nil {
		if errors.Is(err, errNoRows) {
			return ErrInvalidNodeToken
		}
		return err
	}
	if n.ID != id {
		return ErrInvalidNodeToken
	}
	return nil
}
