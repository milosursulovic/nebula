package provisioning

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/scheduler"
)

// Saga drives instance provisioning through spec section 26's pipeline —
// reserve resources, create disk, create network, create VM, start VM —
// compensating in reverse order on any step's failure (spec's own
// example: Create VM fails -> delete network -> delete disk -> release
// resources). Resource reservation is real (node.Reserve/Release, wired
// Phase 8); disk/network steps stay mocked (see mocks.go) — those are
// Phase 12/13's job. VM steps (Phase 9) call out to the target node's
// nebula-agent over HTTP (see agent_steps.go) — still a mock VM backend
// on the agent side, but a real network hop, same as section 27 intends.
type Saga struct {
	instances instance.Service
	nodes     node.Service
	scheduler scheduler.Scheduler
	logger    *slog.Logger

	createDisk    DiskCreator
	deleteDisk    DiskDeleter
	createNetwork NetworkCreator
	deleteNetwork NetworkDeleter
	createVM      VMCreator
	deleteVM      VMDeleter
	startVM       VMStarter
}

// Steps bundles all seven saga step implementations, letting a caller mix
// sources (e.g. mocked disk with agent-backed VM ops and IPAM-backed
// network). This is the only constructor — network can't be meaningfully
// all-mocked since Phase 12 (it needs a real internal/network.Service);
// tests construct a Saga literal directly instead (see saga_test.go).
type Steps struct {
	CreateDisk    DiskCreator
	DeleteDisk    DiskDeleter
	CreateNetwork NetworkCreator
	DeleteNetwork NetworkDeleter
	CreateVM      VMCreator
	DeleteVM      VMDeleter
	StartVM       VMStarter
}

// NewSagaWithSteps builds a Saga with an explicit set of step
// implementations.
func NewSagaWithSteps(instances instance.Service, nodes node.Service, sched scheduler.Scheduler, logger *slog.Logger, steps Steps) *Saga {
	return &Saga{
		instances: instances, nodes: nodes, scheduler: sched, logger: logger,
		createDisk: steps.CreateDisk, deleteDisk: steps.DeleteDisk,
		createNetwork: steps.CreateNetwork, deleteNetwork: steps.DeleteNetwork,
		createVM: steps.CreateVM, deleteVM: steps.DeleteVM,
		startVM: steps.StartVM,
	}
}

// Provision runs the saga once for one instance. It's called once per
// CREATE_INSTANCE job attempt (see cmd/nebula-api/main.go) — a job retry
// after a failure calls Provision again, and the first step (transition
// into PROVISIONING) accepts starting from ERROR as well as PENDING (see
// internal/instance's ERROR->PROVISIONING edge), re-scheduling fresh each
// time rather than assuming the previously-chosen node is still viable.
func (s *Saga) Provision(ctx context.Context, tenantID, instanceID string) error {
	inst, err := s.instances.Get(ctx, tenantID, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	if _, err := s.instances.Transition(ctx, tenantID, instanceID, instance.StatusProvisioning); err != nil {
		return fmt.Errorf("transition to PROVISIONING: %w", err)
	}

	var compensations []func(context.Context)

	fail := func(stepErr error) error {
		for i := len(compensations) - 1; i >= 0; i-- {
			compensations[i](ctx)
		}
		if _, err := s.instances.Transition(ctx, tenantID, instanceID, instance.StatusError); err != nil {
			s.logger.Error("saga: transition to ERROR failed", "instance_id", instanceID, "error", err)
		}
		return stepErr
	}

	chosen, err := s.scheduler.Schedule(ctx, scheduler.ResourceRequest{CPU: inst.CPU, MemoryMB: inst.MemoryMB, DiskGB: inst.DiskGB})
	if err != nil {
		return fail(fmt.Errorf("schedule: %w", err))
	}

	if _, err := s.nodes.Reserve(ctx, chosen.ID, inst.CPU, inst.MemoryMB, inst.DiskGB); err != nil {
		return fail(fmt.Errorf("reserve resources: %w", err))
	}
	compensations = append(compensations, func(ctx context.Context) {
		if _, err := s.nodes.Release(ctx, chosen.ID, inst.CPU, inst.MemoryMB, inst.DiskGB); err != nil {
			s.logger.Error("saga compensation: release resources failed", "instance_id", instanceID, "node_id", chosen.ID, "error", err)
		}
	})

	if _, err := s.instances.SetNodeID(ctx, tenantID, instanceID, chosen.ID); err != nil {
		return fail(fmt.Errorf("set node id: %w", err))
	}

	if err := s.createDisk(ctx, instanceID, inst.DiskGB); err != nil {
		return fail(fmt.Errorf("create disk: %w", err))
	}
	compensations = append(compensations, func(ctx context.Context) {
		if err := s.deleteDisk(ctx, instanceID); err != nil {
			s.logger.Error("saga compensation: delete disk failed", "instance_id", instanceID, "error", err)
		}
	})

	ip, err := s.createNetwork(ctx, instanceID)
	if err != nil {
		return fail(fmt.Errorf("create network: %w", err))
	}
	compensations = append(compensations, func(ctx context.Context) {
		if err := s.deleteNetwork(ctx, instanceID); err != nil {
			s.logger.Error("saga compensation: delete network failed", "instance_id", instanceID, "error", err)
		}
	})

	if _, err := s.instances.SetIPAddress(ctx, tenantID, instanceID, ip); err != nil {
		return fail(fmt.Errorf("set ip address: %w", err))
	}

	vmSpec := InstanceSpec{CPU: inst.CPU, MemoryMB: inst.MemoryMB, DiskGB: inst.DiskGB, Image: inst.Image}
	if err := s.createVM(ctx, instanceID, chosen.ID, vmSpec); err != nil {
		return fail(fmt.Errorf("create vm: %w", err))
	}
	compensations = append(compensations, func(ctx context.Context) {
		if err := s.deleteVM(ctx, instanceID, chosen.ID); err != nil {
			s.logger.Error("saga compensation: delete vm failed", "instance_id", instanceID, "error", err)
		}
	})

	if err := s.startVM(ctx, instanceID, chosen.ID); err != nil {
		return fail(fmt.Errorf("start vm: %w", err))
	}

	if _, err := s.instances.Transition(ctx, tenantID, instanceID, instance.StatusRunning); err != nil {
		return fmt.Errorf("transition to RUNNING: %w", err)
	}

	s.logger.Info("saga: instance provisioned", "instance_id", instanceID, "node_id", chosen.ID)
	return nil
}
