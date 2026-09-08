package agent

import (
	"sync"
	"time"
)

// Store is an in-memory, mutex-guarded VM registry — this agent's mock
// backing store for VM operations (spec: "initially mock VM operations").
// Same shape as a domain package's fake_repository_test.go fake, except
// this is the real (non-test) store: there's no libvirt yet to back it.
type Store struct {
	mu  sync.Mutex
	vms map[string]VM
}

func NewStore() *Store {
	return &Store{vms: make(map[string]VM)}
}

// Create records a new VM for instanceID, or replaces an existing one — a
// job retry re-running CreateVM after a prior partial failure should just
// overwrite the stale record, no separate idempotency key needed.
func (s *Store) Create(instanceID string, cpu, memoryMB, diskGB int, image string) VM {
	s.mu.Lock()
	defer s.mu.Unlock()

	vm := VM{
		InstanceID: instanceID,
		Status:     VMStatusStopped,
		CPU:        cpu,
		MemoryMB:   memoryMB,
		DiskGB:     diskGB,
		Image:      image,
		CreatedAt:  time.Now(),
	}
	s.vms[instanceID] = vm
	return vm
}

// Start transitions a VM to RUNNING.
func (s *Store) Start(instanceID string) (VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vm, ok := s.vms[instanceID]
	if !ok {
		return VM{}, ErrVMNotFound
	}
	vm.Status = VMStatusRunning
	s.vms[instanceID] = vm
	return vm, nil
}

// Stop transitions a VM to STOPPED.
func (s *Store) Stop(instanceID string) (VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vm, ok := s.vms[instanceID]
	if !ok {
		return VM{}, ErrVMNotFound
	}
	vm.Status = VMStatusStopped
	s.vms[instanceID] = vm
	return vm, nil
}

// Delete removes a VM record. Deleting an unknown instanceID is a no-op
// success (compensation may call this after a step that never created
// anything).
func (s *Store) Delete(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vms, instanceID)
}

func (s *Store) Get(instanceID string) (VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vm, ok := s.vms[instanceID]
	if !ok {
		return VM{}, ErrVMNotFound
	}
	return vm, nil
}

// Count returns the number of VMs currently tracked (any status) — used to
// report running_instances on heartbeats.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.vms)
}
