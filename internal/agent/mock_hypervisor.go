package agent

import "context"

// MockHypervisor is Hypervisor's default, always-succeeding implementation
// (spec: "initially mock VM operations") — a thin adapter over Store,
// which does all the actual work. Selected via NEBULA_AGENT_HYPERVISOR
// (default "mock"); this is what Docker Compose runs.
type MockHypervisor struct {
	store *Store
}

func NewMockHypervisor(store *Store) *MockHypervisor {
	return &MockHypervisor{store: store}
}

func (m *MockHypervisor) CreateVM(ctx context.Context, spec VMSpec) error {
	m.store.Create(spec.InstanceID, spec.CPU, spec.MemoryMB, spec.DiskGB, spec.Image)
	return nil
}

func (m *MockHypervisor) DeleteVM(ctx context.Context, instanceID string) error {
	m.store.Delete(instanceID)
	return nil
}

func (m *MockHypervisor) StartVM(ctx context.Context, instanceID string) error {
	_, err := m.store.Start(instanceID)
	return err
}

func (m *MockHypervisor) StopVM(ctx context.Context, instanceID string) error {
	_, err := m.store.Stop(instanceID)
	return err
}

func (m *MockHypervisor) GetVMStatus(ctx context.Context, instanceID string) (VM, error) {
	return m.store.Get(instanceID)
}

func (m *MockHypervisor) CountVMs(ctx context.Context) (int, error) {
	return m.store.Count(), nil
}
