package provisioning

import "context"

// InstanceSpec is the subset of an instance's resource request the VM
// steps need to actually create something (disk/network steps don't need
// it — DiskCreator already carries diskGB on its own).
type InstanceSpec struct {
	CPU      int
	MemoryMB int
	DiskGB   int
	Image    string
}

// The five saga steps (spec section 26) as named func types — mirrors
// job.Handler's shape. Disk stays mocked this phase (Phase 13/Storage's
// job); network is real (Phase 12: internal/network-backed IPAM, see
// network_steps.go); VM steps (Phase 9) call out to the node's
// nebula-agent. These types are what let tests inject a failure at any
// one step independently.
type (
	DiskCreator    func(ctx context.Context, instanceID string, diskGB int) error
	DiskDeleter    func(ctx context.Context, instanceID string) error
	NetworkCreator func(ctx context.Context, instanceID string) (ip string, err error)
	NetworkDeleter func(ctx context.Context, instanceID string) error
	VMCreator      func(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) error
	VMDeleter      func(ctx context.Context, instanceID, nodeID string) error
	VMStarter      func(ctx context.Context, instanceID, nodeID string) error
)
