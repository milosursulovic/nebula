package network

import (
	"context"
	"errors"
	"net"
)

// maxSubnetHostBits caps subnet size at /16 (65534 usable addresses) — a
// safety bound against accidentally pre-populating an unreasonable number
// of ip_addresses rows, not a feature.
const maxSubnetHostBits = 16

// Service is the network business logic surface consumed by pkg/api
// handlers and internal/provisioning's saga steps.
type Service interface {
	// CreateNetwork creates a network and its one subnet (spec section
	// 31's single worked example bundles them) plus the subnet's full IP
	// pool — one row per usable host address, excluding the network,
	// broadcast, and gateway addresses.
	CreateNetwork(ctx context.Context, name, cidr, gateway string) (Network, Subnet, error)

	List(ctx context.Context) ([]Network, error)
	Get(ctx context.Context, id string) (Network, error)
	SubnetsByNetwork(ctx context.Context, networkID string) ([]Subnet, error)

	// AllocateForInstance allocates (or, if already allocated, returns)
	// the IP address for instanceID from the first subnet with capacity.
	AllocateForInstance(ctx context.Context, instanceID string) (string, error)

	// ReleaseForInstance releases instanceID's allocated address, if any.
	ReleaseForInstance(ctx context.Context, instanceID string) error

	// ReserveIP marks one specific address RESERVED (spec's third IPAM
	// verb — an admin pre-reserving an address for manual/static use).
	ReserveIP(ctx context.Context, subnetID, ip string) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) CreateNetwork(ctx context.Context, name, cidr, gateway string) (Network, Subnet, error) {
	usableIPs, err := usableHostIPs(cidr, gateway)
	if err != nil {
		return Network{}, Subnet{}, err
	}
	return s.repo.CreateNetworkWithSubnet(ctx, name, cidr, gateway, usableIPs)
}

func (s *service) List(ctx context.Context) ([]Network, error) {
	return s.repo.List(ctx)
}

func (s *service) Get(ctx context.Context, id string) (Network, error) {
	n, err := s.repo.Get(ctx, id)
	if errors.Is(err, errNoRows) {
		return Network{}, ErrNotFound
	}
	return n, err
}

func (s *service) SubnetsByNetwork(ctx context.Context, networkID string) ([]Subnet, error) {
	return s.repo.SubnetsByNetwork(ctx, networkID)
}

func (s *service) AllocateForInstance(ctx context.Context, instanceID string) (string, error) {
	return s.repo.AllocateForInstance(ctx, instanceID)
}

func (s *service) ReleaseForInstance(ctx context.Context, instanceID string) error {
	return s.repo.ReleaseForInstance(ctx, instanceID)
}

func (s *service) ReserveIP(ctx context.Context, subnetID, ip string) error {
	return s.repo.ReserveIP(ctx, subnetID, ip)
}

// usableHostIPs computes every allocatable address in cidr — every host
// address except the network address, the broadcast address, and gateway.
func usableHostIPs(cidr, gateway string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, ErrInvalidCIDR
	}
	if ip.String() != ipNet.IP.String() {
		// cidr must be the network address itself (e.g. 10.20.0.0/24,
		// not 10.20.0.5/24) — matches spec's own example exactly.
		return nil, ErrInvalidCIDR
	}

	ones, bits := ipNet.Mask.Size()
	hostBits := bits - ones
	if hostBits > maxSubnetHostBits {
		return nil, ErrSubnetTooLarge
	}
	if hostBits < 2 {
		// /31 and /32 have no usable host addresses at all.
		return nil, ErrInvalidCIDR
	}

	gw := net.ParseIP(gateway)
	if gw == nil || !ipNet.Contains(gw) {
		return nil, ErrInvalidCIDR
	}
	gwStr := gw.String()

	network := ipNet.IP.Mask(ipNet.Mask)
	broadcast := make(net.IP, len(network))
	for i := range network {
		broadcast[i] = network[i] | ^ipNet.Mask[i]
	}

	var ips []string
	for cur := cloneIP(network); ipNet.Contains(cur); incIP(cur) {
		s := cur.String()
		if s == network.String() || s == broadcast.String() || s == gwStr {
			continue
		}
		ips = append(ips, s)
	}
	return ips, nil
}

func cloneIP(ip net.IP) net.IP {
	c := make(net.IP, len(ip))
	copy(c, ip)
	return c
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
