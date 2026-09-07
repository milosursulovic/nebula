package agent

import "time"

// VMStatus is a mock VM's lifecycle state as tracked by this agent.
type VMStatus string

const (
	VMStatusStopped VMStatus = "STOPPED"
	VMStatusRunning VMStatus = "RUNNING"
)

// VM is an in-memory record of one instance's VM on this node — a stand-in
// until Phase 11 (KVM/libvirt) replaces the store with the real thing.
type VM struct {
	InstanceID string
	Status     VMStatus
	CPU        int
	MemoryMB   int
	DiskGB     int
	Image      string
	CreatedAt  time.Time
}
