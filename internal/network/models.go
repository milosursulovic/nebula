package network

import "time"

// IPStatus is one IP address's allocation state (spec section 32).
type IPStatus string

const (
	IPStatusAvailable IPStatus = "AVAILABLE"
	IPStatusAllocated IPStatus = "ALLOCATED"
	IPStatusReserved  IPStatus = "RESERVED"
)

// Network is a logical grouping of subnets (spec section 31's "Network:
// production" example) — platform infrastructure, not tenant-scoped
// (same shape as compute nodes).
type Network struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Subnet is one CIDR block within a Network (spec section 31's
// "Subnet: 10.20.0.0/24, Gateway: 10.20.0.1" example). AWS VPC/Subnet's
// 1:many shape, simplified (no availability zones).
type Subnet struct {
	ID        string
	NetworkID string
	CIDR      string
	Gateway   string
	CreatedAt time.Time
}
