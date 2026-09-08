package agent

import "context"

// Hypervisor abstracts VM lifecycle operations (spec section 30) so the
// gRPC layer (server.go) doesn't care whether it's talking to an
// in-memory mock or real libvirt/KVM. CountVMs is one addition beyond
// spec's five methods — GetNodeInfo's heartbeat metrics genuinely need a
// running-instance count, and both backends can trivially provide one.
type Hypervisor interface {
	CreateVM(ctx context.Context, spec VMSpec) error
	DeleteVM(ctx context.Context, instanceID string) error
	StartVM(ctx context.Context, instanceID string) error
	StopVM(ctx context.Context, instanceID string) error
	GetVMStatus(ctx context.Context, instanceID string) (VM, error)
	CountVMs(ctx context.Context) (int, error)
}
