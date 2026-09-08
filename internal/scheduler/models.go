package scheduler

import (
	"context"
	"errors"

	"github.com/milosursulovic/nebula/internal/node"
)

// ErrNoCapacity is returned when no eligible node satisfies a ResourceRequest.
var ErrNoCapacity = errors.New("no node has sufficient capacity")

// ResourceRequest describes the capacity an instance needs (spec section 18).
type ResourceRequest struct {
	CPU      int
	MemoryMB int
	DiskGB   int
}

// Scheduler picks a compute node for a resource request (spec section 18).
type Scheduler interface {
	Schedule(ctx context.Context, req ResourceRequest) (node.Node, error)
}

// NodeLister is the only capability a scheduling strategy needs. A package-
// local, narrow interface rather than the full node.Repository/node.Service
// — both already satisfy it structurally, so wiring a real caller in later
// needs no changes here.
type NodeLister interface {
	List(ctx context.Context) ([]node.Node, error)
}

// eligibleNodes returns the ONLINE nodes with enough available capacity to
// satisfy req, in the order NodeLister returned them.
func eligibleNodes(ctx context.Context, nodes NodeLister, req ResourceRequest) ([]node.Node, error) {
	all, err := nodes.List(ctx)
	if err != nil {
		return nil, err
	}

	// Pre-sized to the worst case (every node qualifies) — a nil slice
	// grown via append alone forces several reallocate-and-copy cycles as
	// it doubles, each copying increasingly many (fairly large) node.Node
	// values. Phase 17's benchmarking/profiling found this function
	// accounting for 100% of the scheduler benchmark's allocations and a
	// third of its CPU time (mostly downstream GC pressure from those
	// allocations, not eligibleNodes' own filtering logic) — this is the
	// concrete, safe fix that came out of it, same pre-sizing idiom
	// already used elsewhere in this codebase (e.g. pkg/api's list
	// handlers' `make([]xResponse, 0, len(items))`).
	eligible := make([]node.Node, 0, len(all))
	for _, n := range all {
		if n.Status != node.StatusOnline {
			continue
		}
		if n.AvailableCPU < req.CPU || n.AvailableMemoryMB < req.MemoryMB || n.AvailableDiskGB < req.DiskGB {
			continue
		}
		eligible = append(eligible, n)
	}
	return eligible, nil
}
