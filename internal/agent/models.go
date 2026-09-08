package agent

import "time"

// VMStatus is a mock VM's lifecycle state as tracked by this agent.
type VMStatus string

const (
	VMStatusStopped VMStatus = "STOPPED"
	VMStatusRunning VMStatus = "RUNNING"
)

// VM is a record of one instance's VM on this node — returned by both
// Hypervisor implementations (MockHypervisor's in-memory Store, and
// LibvirtHypervisor's real libvirt domain).
type VM struct {
	InstanceID string
	Status     VMStatus
	CPU        int
	MemoryMB   int
	DiskGB     int
	Image      string
	CreatedAt  time.Time
}

// VMSpec is what Hypervisor.CreateVM needs to define a VM (spec section
// 30's Hypervisor interface).
type VMSpec struct {
	InstanceID string
	CPU        int
	MemoryMB   int
	DiskGB     int
	Image      string
}
