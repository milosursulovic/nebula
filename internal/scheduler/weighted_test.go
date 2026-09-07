package scheduler

import (
	"context"
	"math"
	"testing"

	"github.com/milosursulovic/nebula/internal/node"
)

func TestWeightedScoreMatchesSpecFormula(t *testing.T) {
	n := onlineNode("a", 10, 5, 1000, 500, 100, 50, 1.0, 3)

	got := weightedScore(n)

	cpuScore := 0.5            // 5/10
	memScore := 0.5            // 500/1000
	diskScore := 0.5           // 50/100
	loadScore := 1.0 / 2.0     // 1/(1+1.0)
	instanceScore := 1.0 / 4.0 // 1/(1+3)

	want := cpuScore*0.30 + memScore*0.30 + diskScore*0.15 + loadScore*0.15 + instanceScore*0.10

	if math.Abs(got-want) > 1e-9 {
		t.Errorf("weightedScore = %v, want %v", got, want)
	}
}

func TestWeightedPicksDominantNode(t *testing.T) {
	// worse on every single dimension: less available capacity, higher
	// load, more running instances.
	worse := onlineNode("a-worse", 16, 4, 32768, 8192, 1000, 250, 9.0, 30)
	// better on every dimension.
	better := onlineNode("b-better", 16, 14, 32768, 28672, 1000, 900, 0.1, 1)

	s := NewWeighted(fakeNodeLister{nodes: []node.Node{worse, better}})

	got, err := s.Schedule(context.Background(), ResourceRequest{CPU: 1, MemoryMB: 1, DiskGB: 1})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got.Hostname != better.Hostname {
		t.Errorf("Schedule picked %q, want dominant node %q", got.Hostname, better.Hostname)
	}
}
