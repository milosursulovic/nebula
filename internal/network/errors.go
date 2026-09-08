package network

import "errors"

var (
	// ErrNameTaken is returned when creating a network whose name already
	// exists.
	ErrNameTaken = errors.New("network name already taken")

	// ErrNotFound is returned when a network ID does not match any network.
	ErrNotFound = errors.New("network not found")

	// ErrInvalidCIDR is returned for a malformed CIDR or a gateway address
	// outside it.
	ErrInvalidCIDR = errors.New("invalid cidr or gateway")

	// ErrSubnetTooLarge guards against accidentally pre-populating an
	// unreasonable number of ip_addresses rows (a safety bound, not a
	// feature) — subnets larger than /16 are rejected.
	ErrSubnetTooLarge = errors.New("subnet too large: must be /16 or smaller")

	// ErrNoCapacity is returned when a subnet has no AVAILABLE address
	// left to allocate or reserve.
	ErrNoCapacity = errors.New("no available ip address")

	// ErrIPNotAvailable is returned when ReserveIP targets an address
	// that isn't currently AVAILABLE (already allocated/reserved, or not
	// part of the subnet's pool).
	ErrIPNotAvailable = errors.New("ip address not available")

	// errNoRows is an internal repository-layer sentinel translated by the
	// service into ErrNotFound as appropriate.
	errNoRows = errors.New("no rows")
)
