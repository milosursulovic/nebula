package node

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"

	"github.com/milosursulovic/nebula/internal/common"
	"github.com/milosursulovic/nebula/internal/metrics"
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

	// Reserve decrements a node's available capacity (spec section 16:
	// AVAILABLE -> RESERVED), retrying on optimistic-concurrency conflicts.
	// Returns ErrInsufficientCapacity if the node can't fit the request at
	// all, or ErrReservationConflict if retries are exhausted.
	Reserve(ctx context.Context, id string, cpu, memoryMB, diskGB int) (Node, error)

	// Release increments a node's available capacity back (spec section
	// 16: RESERVED -> AVAILABLE on failure), retrying on conflicts.
	Release(ctx context.Context, id string, cpu, memoryMB, diskGB int) (Node, error)
}

// maxReservationRetries bounds the optimistic-concurrency retry loop for
// Reserve/Release (spec section 17's "must retry scheduling" example). Set
// high enough that heavy contention (spec section 19's 100 concurrent
// requests against one node) resolves every request to a real outcome
// (success or ErrInsufficientCapacity) rather than exhausting retries.
const maxReservationRetries = 50

// reservationRetryBackoff adds jitter between optimistic-concurrency retry
// attempts (same idea as spec section 24's retry system: "add jitter to
// avoid synchronized retries") so many goroutines racing the same row's
// version don't all retry in lockstep.
func reservationRetryBackoff() {
	time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
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
	metrics.NodeCPUUsage.WithLabelValues(id).Set(in.CPUUsage)
	metrics.NodeMemoryUsage.WithLabelValues(id).Set(float64(in.MemoryUsedMB))
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

func (s *service) Reserve(ctx context.Context, id string, cpu, memoryMB, diskGB int) (Node, error) {
	for attempt := 0; attempt < maxReservationRetries; attempt++ {
		if attempt > 0 {
			reservationRetryBackoff()
		}

		current, err := s.Get(ctx, id)
		if err != nil {
			return Node{}, err
		}

		if current.AvailableCPU < cpu || current.AvailableMemoryMB < memoryMB || current.AvailableDiskGB < diskGB {
			return Node{}, ErrInsufficientCapacity
		}

		updated, ok, err := s.repo.TryReserve(ctx, id, current.Version, cpu, memoryMB, diskGB)
		if err != nil {
			return Node{}, err
		}
		if ok {
			return updated, nil
		}
		// Version changed concurrently; re-read and retry.
	}
	return Node{}, ErrReservationConflict
}

func (s *service) Release(ctx context.Context, id string, cpu, memoryMB, diskGB int) (Node, error) {
	for attempt := 0; attempt < maxReservationRetries; attempt++ {
		if attempt > 0 {
			reservationRetryBackoff()
		}

		current, err := s.Get(ctx, id)
		if err != nil {
			return Node{}, err
		}

		updated, ok, err := s.repo.TryRelease(ctx, id, current.Version, cpu, memoryMB, diskGB)
		if err != nil {
			return Node{}, err
		}
		if ok {
			return updated, nil
		}
		// Version changed concurrently; re-read and retry.
	}
	return Node{}, ErrReservationConflict
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
