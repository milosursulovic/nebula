package provisioning

import "context"

// The five saga steps (spec section 26) as named func types — mirrors
// job.Handler's shape. Disk/network/VM creation stay mocked this phase
// (see Saga's doc comment); these types are what let tests inject a
// failure at any one step independently.
type (
	DiskCreator    func(ctx context.Context, instanceID string, diskGB int) error
	DiskDeleter    func(ctx context.Context, instanceID string) error
	NetworkCreator func(ctx context.Context, instanceID string) error
	NetworkDeleter func(ctx context.Context, instanceID string) error
	VMCreator      func(ctx context.Context, instanceID, nodeID string) error
	VMDeleter      func(ctx context.Context, instanceID string) error
	VMStarter      func(ctx context.Context, instanceID string) error
)
