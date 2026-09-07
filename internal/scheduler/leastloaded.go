package scheduler

import (
	"context"

	"github.com/milosursulovic/nebula/internal/node"
)

// LeastLoaded chooses the eligible node with the lowest load average (spec
// section 18), breaking ties by hostname for determinism.
type LeastLoaded struct {
	nodes NodeLister
}

func NewLeastLoaded(nodes NodeLister) *LeastLoaded {
	return &LeastLoaded{nodes: nodes}
}

func (s *LeastLoaded) Schedule(ctx context.Context, req ResourceRequest) (node.Node, error) {
	eligible, err := eligibleNodes(ctx, s.nodes, req)
	if err != nil {
		return node.Node{}, err
	}
	if len(eligible) == 0 {
		return node.Node{}, ErrNoCapacity
	}

	best := eligible[0]
	for _, n := range eligible[1:] {
		if n.LoadAverage < best.LoadAverage ||
			(n.LoadAverage == best.LoadAverage && n.Hostname < best.Hostname) {
			best = n
		}
	}
	return best, nil
}
