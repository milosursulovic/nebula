package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/milosursulovic/nebula/internal/node"
)

func TestFirstFitPicksFirstEligibleInOrder(t *testing.T) {
	tooSmall := onlineNode("a-too-small", 4, 1, 8192, 1024, 100, 10, 0, 0)
	eligible1 := onlineNode("b-eligible", 16, 8, 32768, 16384, 1000, 500, 1.0, 2)
	eligible2 := onlineNode("c-eligible-too", 16, 16, 32768, 32768, 1000, 1000, 0, 0)

	s := NewFirstFit(fakeNodeLister{nodes: []node.Node{tooSmall, eligible1, eligible2}})

	got, err := s.Schedule(context.Background(), ResourceRequest{CPU: 4, MemoryMB: 4096, DiskGB: 50})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got.Hostname != eligible1.Hostname {
		t.Errorf("Schedule picked %q, want first eligible %q", got.Hostname, eligible1.Hostname)
	}
}

func TestFirstFitExcludesNonOnlineNodes(t *testing.T) {
	degraded := onlineNode("a-degraded", 16, 16, 32768, 32768, 1000, 1000, 0, 0)
	degraded.Status = node.StatusDegraded
	online := onlineNode("b-online", 16, 16, 32768, 32768, 1000, 1000, 0, 0)

	s := NewFirstFit(fakeNodeLister{nodes: []node.Node{degraded, online}})

	got, err := s.Schedule(context.Background(), ResourceRequest{CPU: 1, MemoryMB: 1, DiskGB: 1})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got.Hostname != online.Hostname {
		t.Errorf("Schedule picked %q, want the only ONLINE node %q", got.Hostname, online.Hostname)
	}
}

func TestFirstFitNoCapacity(t *testing.T) {
	small := onlineNode("a", 1, 1, 1024, 1024, 10, 10, 0, 0)

	s := NewFirstFit(fakeNodeLister{nodes: []node.Node{small}})

	_, err := s.Schedule(context.Background(), ResourceRequest{CPU: 100, MemoryMB: 100, DiskGB: 100})
	if !errors.Is(err, ErrNoCapacity) {
		t.Fatalf("Schedule = %v, want ErrNoCapacity", err)
	}
}
