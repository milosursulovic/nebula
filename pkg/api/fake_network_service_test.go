package api

import (
	"context"

	"github.com/milosursulovic/nebula/internal/network"
)

// fakeNetworkService is a hand-rolled network.Service double for handler tests.
type fakeNetworkService struct {
	createNetworkFn      func(ctx context.Context, name, cidr, gateway string) (network.Network, network.Subnet, error)
	listFn               func(ctx context.Context) ([]network.Network, error)
	getFn                func(ctx context.Context, id string) (network.Network, error)
	subnetsByNetworkFn   func(ctx context.Context, networkID string) ([]network.Subnet, error)
	allocateForInstance  func(ctx context.Context, instanceID string) (string, error)
	releaseForInstanceFn func(ctx context.Context, instanceID string) error
	reserveIPFn          func(ctx context.Context, subnetID, ip string) error
}

func (f fakeNetworkService) CreateNetwork(ctx context.Context, name, cidr, gateway string) (network.Network, network.Subnet, error) {
	return f.createNetworkFn(ctx, name, cidr, gateway)
}

func (f fakeNetworkService) List(ctx context.Context) ([]network.Network, error) {
	return f.listFn(ctx)
}

func (f fakeNetworkService) Get(ctx context.Context, id string) (network.Network, error) {
	return f.getFn(ctx, id)
}

func (f fakeNetworkService) SubnetsByNetwork(ctx context.Context, networkID string) ([]network.Subnet, error) {
	return f.subnetsByNetworkFn(ctx, networkID)
}

func (f fakeNetworkService) AllocateForInstance(ctx context.Context, instanceID string) (string, error) {
	return f.allocateForInstance(ctx, instanceID)
}

func (f fakeNetworkService) ReleaseForInstance(ctx context.Context, instanceID string) error {
	return f.releaseForInstanceFn(ctx, instanceID)
}

func (f fakeNetworkService) ReserveIP(ctx context.Context, subnetID, ip string) error {
	return f.reserveIPFn(ctx, subnetID, ip)
}
