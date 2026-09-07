package node

import (
	"context"
	"log/slog"
	"time"
)

// MonitorInterval is how often the monitor checks node heartbeats (spec section 11).
const MonitorInterval = 5 * time.Second

// Monitor periodically checks each node's last heartbeat and transitions
// its status when it goes stale. "Publish event" (spec section 11) is a
// structured log line for now — the real event bus (Kafka + transactional
// outbox) arrives in a later phase.
type Monitor struct {
	repo     Repository
	logger   *slog.Logger
	interval time.Duration
}

func NewMonitor(repo Repository, logger *slog.Logger) *Monitor {
	return &Monitor{repo: repo, logger: logger, interval: MonitorInterval}
}

// Run blocks, checking node health every interval until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *Monitor) tick(ctx context.Context) {
	nodes, err := m.repo.List(ctx)
	if err != nil {
		m.logger.Error("node monitor: list nodes failed", "error", err)
		return
	}

	now := time.Now()
	for _, n := range nodes {
		if n.Status == StatusDraining || n.LastHeartbeatAt == nil {
			continue
		}

		newStatus := DetermineStatus(*n.LastHeartbeatAt, now)
		if newStatus == n.Status {
			continue
		}

		if err := m.repo.UpdateStatus(ctx, n.ID, newStatus); err != nil {
			m.logger.Error("node monitor: update status failed", "node_id", n.ID, "error", err)
			continue
		}

		m.logger.Info("node status changed",
			"node_id", n.ID,
			"hostname", n.Hostname,
			"from", n.Status,
			"to", newStatus,
		)
	}
}

// DetermineStatus computes a node's status from how long ago its last
// heartbeat was received, per spec section 11's thresholds: <10s ONLINE,
// 10-30s DEGRADED, >30s OFFLINE.
func DetermineStatus(lastHeartbeat, now time.Time) Status {
	age := now.Sub(lastHeartbeat)
	switch {
	case age < OnlineThreshold:
		return StatusOnline
	case age < DegradedThreshold:
		return StatusDegraded
	default:
		return StatusOffline
	}
}
