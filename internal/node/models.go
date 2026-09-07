package node

import "time"

// Status is a compute node's health state (spec section 9).
type Status string

const (
	StatusOnline   Status = "ONLINE"
	StatusDegraded Status = "DEGRADED"
	StatusOffline  Status = "OFFLINE"
	StatusDraining Status = "DRAINING"
)

// Heartbeat staleness thresholds (spec section 11).
const (
	OnlineThreshold   = 10 * time.Second
	DegradedThreshold = 30 * time.Second
)

// Node is a physical or virtual Linux machine capable of running VMs.
type Node struct {
	ID                string
	Hostname          string
	IP                string
	Status            Status
	TotalCPU          int
	AvailableCPU      int
	TotalMemoryMB     int
	AvailableMemoryMB int
	TotalDiskGB       int
	AvailableDiskGB   int
	LoadAverage       float64
	RunningInstances  int
	LastHeartbeatAt   *time.Time
	TokenHash         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// RegisterInput is what a caller supplies to register a new node
// (spec section 10's example request).
type RegisterInput struct {
	Hostname string
	IP       string
	CPU      int
	MemoryMB int
	DiskGB   int
}

// HeartbeatInput is what an agent reports periodically (spec section 11).
type HeartbeatInput struct {
	CPUUsage         float64
	MemoryUsedMB     int
	DiskUsedGB       int
	LoadAverage      float64
	RunningInstances int
}
