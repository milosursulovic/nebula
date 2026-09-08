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
// resources). Every step is real as of Phase 13: resource reservation
// (node.Reserve/Release, Phase 8), disk (internal/storage-backed sparse
// files, disk_steps.go, Phase 13), network (internal/network-backed
// IPAM, network_steps.go, Phase 12), and VM (the target node's
// nebula-agent over TLS-secured gRPC, agent_steps.go, Phases 9-11 — the
// agent's own VM backend can still be a mock hypervisor or real
// libvirt/KVM depending on build). No real libvirt <disk>/<interface>
// device attachment happens yet — see the Phase 12/13 plans' scope
// boundary notes for why.
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

// Steps bundles all seven saga step implementations. This is the only
// constructor — every step needs a real backing service as of Phase 13
// (disk needs internal/storage.Service, network needs
// internal/network.Service), so there's no meaningful all-mock default
// left to wrap; tests construct a Saga literal directly instead (see
// saga_test.go).
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

	if inst.Status == instance.StatusProvisioning {
		// A prior attempt for this same instance started but never
		// finished — the only way Provision() ever observes PROVISIONING
		// at its own entry (one job claimed by one worker at a time; a
		// normal completed attempt always ends at RUNNING or ERROR
		// first) is that the prior attempt's process died mid-flight
		// (spec section 46's "worker crash") before it could run its own
		// compensation. Recover exactly like a normal failed attempt:
		// best-effort release whatever it left behind, then fall through
		// to the same ERROR->PROVISIONING retry path a clean failure
		// already uses below — without this, PROVISIONING has no
		// self-transition, so every retry would fail identically forever
		// and the instance would be stuck (unretriable, undeletable).
		s.compensateLeftovers(ctx, instanceID, inst)
		if _, err := s.instances.Transition(ctx, tenantID, instanceID, instance.StatusError); err != nil {
			return fmt.Errorf("recover orphaned provisioning attempt: %w", err)
		}
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

	if err := s.createDisk(ctx, instanceID, tenantID, chosen.ID, inst.DiskGB); err != nil {
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

// compensateLeftovers best-effort releases whatever a crashed prior
// attempt might have created for this instance, before Provision retries
// it fresh. Only called once, from the PROVISIONING-at-entry crash signal
// above — every one of these four calls is independently idempotent-safe
// when there's nothing to clean up (delete-VM/delete-disk/delete-network
// all no-op on an already-absent target), EXCEPT node.Release, whose SQL
// adds capacity back with no clamp against the node's total — calling it
// twice for the same reservation would silently over-credit a node's
// available capacity. That's exactly why this only runs once, gated on
// the crash signal, rather than unconditionally on every retry.
func (s *Saga) compensateLeftovers(ctx context.Context, instanceID string, inst instance.Instance) {
	if inst.NodeID == nil {
		return // never got past scheduling+reserve; nothing to clean up
	}

	if err := s.deleteVM(ctx, instanceID, *inst.NodeID); err != nil {
		s.logger.Error("saga recovery: delete vm failed", "instance_id", instanceID, "node_id", *inst.NodeID, "error", err)
	}
	if err := s.deleteDisk(ctx, instanceID); err != nil {
		s.logger.Error("saga recovery: delete disk failed", "instance_id", instanceID, "error", err)
	}
	if err := s.deleteNetwork(ctx, instanceID); err != nil {
		s.logger.Error("saga recovery: delete network failed", "instance_id", instanceID, "error", err)
	}
	if _, err := s.nodes.Release(ctx, *inst.NodeID, inst.CPU, inst.MemoryMB, inst.DiskGB); err != nil {
		s.logger.Error("saga recovery: release node capacity failed", "instance_id", instanceID, "node_id", *inst.NodeID, "error", err)
	}
}
