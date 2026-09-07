package scheduler

import (
	"context"

	"github.com/milosursulovic/nebula/internal/node"
)

// BestFit chooses the node with the smallest remaining capacity after the
// hypothetical allocation (spec section 18) — the tightest fit. Remaining
// capacity is measured as the average leftover fraction across CPU/memory/
// disk (each normalized by that node's own total), so no single resource's
// raw unit scale (MB vs. cores) dominates the comparison.
type BestFit struct {
	nodes NodeLister
}

func NewBestFit(nodes NodeLister) *BestFit {
	return &BestFit{nodes: nodes}
}

func (s *BestFit) Schedule(ctx context.Context, req ResourceRequest) (node.Node, error) {
	eligible, err := eligibleNodes(ctx, s.nodes, req)
	if err != nil {
		return node.Node{}, err
	}
	if len(eligible) == 0 {
		return node.Node{}, ErrNoCapacity
	}

	best := eligible[0]
	bestScore := leftoverFraction(best, req)
	for _, n := range eligible[1:] {
		if score := leftoverFraction(n, req); score < bestScore {
			best, bestScore = n, score
		}
	}
	return best, nil
}

func leftoverFraction(n node.Node, req ResourceRequest) float64 {
	cpuFrac := float64(n.AvailableCPU-req.CPU) / float64(n.TotalCPU)
	memFrac := float64(n.AvailableMemoryMB-req.MemoryMB) / float64(n.TotalMemoryMB)
	diskFrac := float64(n.AvailableDiskGB-req.DiskGB) / float64(n.TotalDiskGB)
	return (cpuFrac + memFrac + diskFrac) / 3
}
