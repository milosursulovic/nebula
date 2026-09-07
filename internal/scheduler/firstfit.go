package scheduler

import (
	"context"

	"github.com/milosursulovic/nebula/internal/node"
)

// FirstFit chooses the first eligible node with enough resources (spec
// section 18), in NodeLister's listing order.
type FirstFit struct {
	nodes NodeLister
}

func NewFirstFit(nodes NodeLister) *FirstFit {
	return &FirstFit{nodes: nodes}
}

func (s *FirstFit) Schedule(ctx context.Context, req ResourceRequest) (node.Node, error) {
	eligible, err := eligibleNodes(ctx, s.nodes, req)
	if err != nil {
		return node.Node{}, err
	}
	if len(eligible) == 0 {
		return node.Node{}, ErrNoCapacity
	}
	return eligible[0], nil
}
