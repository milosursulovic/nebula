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
// job.Handler's shape. All five are real as of Phase 13: disk
// (internal/storage-backed sparse files, see disk_steps.go), network
// (Phase 12: internal/network-backed IPAM, see network_steps.go), and VM
// (Phase 9: the node's nebula-agent, see agent_steps.go). These types are
// what let tests inject a failure at any one step independently.
type (
	DiskCreator    func(ctx context.Context, instanceID, tenantID, nodeID string, diskGB int) error
	DiskDeleter    func(ctx context.Context, instanceID string) error
	NetworkCreator func(ctx context.Context, instanceID string) (ip string, err error)
	NetworkDeleter func(ctx context.Context, instanceID string) error
	VMCreator      func(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) error
	VMDeleter      func(ctx context.Context, instanceID, nodeID string) error
	VMStarter      func(ctx context.Context, instanceID, nodeID string) error
	VMStopper      func(ctx context.Context, instanceID, nodeID string) error
)
