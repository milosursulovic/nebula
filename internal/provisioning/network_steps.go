package provisioning

import (
	"context"
	"fmt"

	"github.com/milosursulovic/nebula/internal/network"
)

// NetworkSteps implements the network saga step by allocating/releasing a
// real IP address via internal/network's IPAM (Phase 12) — replacing the
// mock CreateNetwork/DeleteNetwork Phase 8 left in mocks.go. No Linux
// bridge/veth/namespace device is created (spec section 33's "Eventually"
// — deferred, see the Phase 12 plan's scope boundary note); this is
// control-plane address allocation only.
type NetworkSteps struct {
	networks network.Service
}

func NewNetworkSteps(networks network.Service) *NetworkSteps {
	return &NetworkSteps{networks: networks}
}

func (n *NetworkSteps) CreateNetwork(ctx context.Context, instanceID string) (string, error) {
	ip, err := n.networks.AllocateForInstance(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("allocate ip: %w", err)
	}
	return ip, nil
}

func (n *NetworkSteps) DeleteNetwork(ctx context.Context, instanceID string) error {
	if err := n.networks.ReleaseForInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("release ip: %w", err)
	}
	return nil
}
