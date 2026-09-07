package provisioning

import (
	"context"
	"log/slog"
	"time"
)

// mockStepDelay simulates real work for a saga step, same shape as Phase
// 6's mock CREATE_INSTANCE handler.
const mockStepDelay = 100 * time.Millisecond

// MockSteps implements the disk/network/VM saga steps as simulated,
// always-succeeding work (spec: "provisioning can still use mocks") —
// stand-ins until Phases 11-13 (KVM/libvirt, Networking, Storage) build
// the real thing. This is deliberately not persisted anywhere (no
// instance_disks/networks tables yet) — see the Phase 8 plan's scope
// boundary note.
type MockSteps struct {
	logger *slog.Logger
}

func NewMockSteps(logger *slog.Logger) *MockSteps {
	return &MockSteps{logger: logger}
}

func (m *MockSteps) simulateWork(ctx context.Context) error {
	select {
	case <-time.After(mockStepDelay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *MockSteps) CreateDisk(ctx context.Context, instanceID string, diskGB int) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: disk created (mock)", "instance_id", instanceID, "disk_gb", diskGB)
	return nil
}

func (m *MockSteps) DeleteDisk(ctx context.Context, instanceID string) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: disk deleted (mock)", "instance_id", instanceID)
	return nil
}

func (m *MockSteps) CreateNetwork(ctx context.Context, instanceID string) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: network created (mock)", "instance_id", instanceID)
	return nil
}

func (m *MockSteps) DeleteNetwork(ctx context.Context, instanceID string) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: network deleted (mock)", "instance_id", instanceID)
	return nil
}

func (m *MockSteps) CreateVM(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: VM created (mock)", "instance_id", instanceID, "node_id", nodeID, "spec", spec)
	return nil
}

func (m *MockSteps) DeleteVM(ctx context.Context, instanceID, nodeID string) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: VM deleted (mock)", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

func (m *MockSteps) StartVM(ctx context.Context, instanceID, nodeID string) error {
	if err := m.simulateWork(ctx); err != nil {
		return err
	}
	m.logger.Info("provisioning: VM started (mock)", "instance_id", instanceID, "node_id", nodeID)
	return nil
}
