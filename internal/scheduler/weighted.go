package scheduler

import (
	"context"

	"github.com/milosursulovic/nebula/internal/node"
)

// Weighted scores eligible nodes with spec section 18's exact formula and
// picks the highest score:
//
//	score = cpu_score*0.30 + memory_score*0.30 + disk_score*0.15
//	      + load_score*0.15 + instance_score*0.10
type Weighted struct {
	nodes NodeLister
}

func NewWeighted(nodes NodeLister) *Weighted {
	return &Weighted{nodes: nodes}
}

func (s *Weighted) Schedule(ctx context.Context, req ResourceRequest) (node.Node, error) {
	eligible, err := eligibleNodes(ctx, s.nodes, req)
	if err != nil {
		return node.Node{}, err
	}
	if len(eligible) == 0 {
		return node.Node{}, ErrNoCapacity
	}

	best := eligible[0]
	bestScore := weightedScore(best)
	for _, n := range eligible[1:] {
		if score := weightedScore(n); score > bestScore {
			best, bestScore = n, score
		}
	}
	return best, nil
}

func weightedScore(n node.Node) float64 {
	cpuScore := float64(n.AvailableCPU) / float64(n.TotalCPU)
	memoryScore := float64(n.AvailableMemoryMB) / float64(n.TotalMemoryMB)
	diskScore := float64(n.AvailableDiskGB) / float64(n.TotalDiskGB)
	loadScore := 1 / (1 + n.LoadAverage)
	instanceScore := 1 / float64(1+n.RunningInstances)

	return cpuScore*0.30 + memoryScore*0.30 + diskScore*0.15 + loadScore*0.15 + instanceScore*0.10
}
