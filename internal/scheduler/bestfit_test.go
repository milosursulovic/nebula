package scheduler

import (
	"context"
	"testing"

	"github.com/milosursulovic/nebula/internal/node"
)

func TestBestFitPicksTightestFit(t *testing.T) {
	// Roomy node: request leaves lots of leftover capacity.
	roomy := onlineNode("a-roomy", 64, 64, 65536, 65536, 4000, 4000, 0, 0)
	// Snug node: request nearly exhausts it.
	snug := onlineNode("b-snug", 4, 4, 4096, 4096, 100, 100, 0, 0)

	s := NewBestFit(fakeNodeLister{nodes: []node.Node{roomy, snug}})

	got, err := s.Schedule(context.Background(), ResourceRequest{CPU: 4, MemoryMB: 4096, DiskGB: 100})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got.Hostname != snug.Hostname {
		t.Errorf("Schedule picked %q, want tightest fit %q", got.Hostname, snug.Hostname)
	}
}

func TestBestFitNoCapacity(t *testing.T) {
	small := onlineNode("a", 1, 1, 1024, 1024, 10, 10, 0, 0)

	s := NewBestFit(fakeNodeLister{nodes: []node.Node{small}})

	if _, err := s.Schedule(context.Background(), ResourceRequest{CPU: 100, MemoryMB: 100, DiskGB: 100}); err == nil {
		t.Fatal("expected ErrNoCapacity")
	}
}
