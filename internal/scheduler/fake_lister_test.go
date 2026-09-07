package scheduler

import (
	"context"

	"github.com/milosursulovic/nebula/internal/node"
)

type fakeNodeLister struct {
	nodes []node.Node
}

func (f fakeNodeLister) List(ctx context.Context) ([]node.Node, error) {
	return f.nodes, nil
}

func onlineNode(hostname string, totalCPU, availCPU, totalMem, availMem, totalDisk, availDisk int, load float64, running int) node.Node {
	return node.Node{
		Hostname:          hostname,
		Status:            node.StatusOnline,
		TotalCPU:          totalCPU,
		AvailableCPU:      availCPU,
		TotalMemoryMB:     totalMem,
		AvailableMemoryMB: availMem,
		TotalDiskGB:       totalDisk,
		AvailableDiskGB:   availDisk,
		LoadAverage:       load,
		RunningInstances:  running,
	}
}
