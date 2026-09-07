package node

import (
	"context"
	"testing"
	"time"
)

func TestDetermineStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		age  time.Duration
		want Status
	}{
		{"just heartbeat", 0, StatusOnline},
		{"still fresh", 9 * time.Second, StatusOnline},
		{"just past online threshold", 10 * time.Second, StatusDegraded},
		{"mid degraded", 20 * time.Second, StatusDegraded},
		{"just past degraded threshold", 30 * time.Second, StatusOffline},
		{"long stale", 5 * time.Minute, StatusOffline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineStatus(now.Add(-tt.age), now)
			if got != tt.want {
				t.Errorf("DetermineStatus(age=%v) = %q, want %q", tt.age, got, tt.want)
			}
		})
	}
}

func TestMonitorTickTransitionsStaleNode(t *testing.T) {
	repo := newFakeRepository()
	ctx := context.Background()

	staleHeartbeat := time.Now().Add(-time.Hour)
	created, err := repo.Create(ctx, Node{
		Hostname: "compute-01", Status: StatusOnline, LastHeartbeatAt: &staleHeartbeat, TokenHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m := NewMonitor(repo, testLogger())
	m.tick(ctx)

	updated, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != StatusOffline {
		t.Errorf("Status after tick = %q, want %q", updated.Status, StatusOffline)
	}
}

func TestMonitorTickIgnoresDrainingNode(t *testing.T) {
	repo := newFakeRepository()
	ctx := context.Background()

	staleHeartbeat := time.Now().Add(-time.Hour)
	created, err := repo.Create(ctx, Node{
		Hostname: "compute-01", Status: StatusDraining, LastHeartbeatAt: &staleHeartbeat, TokenHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m := NewMonitor(repo, testLogger())
	m.tick(ctx)

	updated, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != StatusDraining {
		t.Errorf("Status after tick = %q, want unchanged %q", updated.Status, StatusDraining)
	}
}
