package scheduler

import (
	"context"
	"testing"

	"github.com/milosursulovic/nebula/internal/node"
)

func TestLeastLoadedPicksLowestLoad(t *testing.T) {
	busy := onlineNode("a-busy", 16, 16, 32768, 32768, 1000, 1000, 8.5, 20)
	idle := onlineNode("b-idle", 16, 16, 32768, 32768, 1000, 1000, 0.2, 1)

	s := NewLeastLoaded(fakeNodeLister{nodes: []node.Node{busy, idle}})

	got, err := s.Schedule(context.Background(), ResourceRequest{CPU: 1, MemoryMB: 1, DiskGB: 1})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got.Hostname != idle.Hostname {
		t.Errorf("Schedule picked %q, want lowest-load node %q", got.Hostname, idle.Hostname)
	}
}

func TestLeastLoadedBreaksTiesByHostname(t *testing.T) {
	b := onlineNode("b-tied", 16, 16, 32768, 32768, 1000, 1000, 1.0, 0)
	a := onlineNode("a-tied", 16, 16, 32768, 32768, 1000, 1000, 1.0, 0)

	s := NewLeastLoaded(fakeNodeLister{nodes: []node.Node{b, a}})

	got, err := s.Schedule(context.Background(), ResourceRequest{CPU: 1, MemoryMB: 1, DiskGB: 1})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got.Hostname != "a-tied" {
		t.Errorf("Schedule picked %q, want deterministic tie-break %q", got.Hostname, "a-tied")
	}
}
