package provisioning

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/scheduler"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// callRecorder captures the order operations happened in across all the
// fakes below, so tests can assert exact step + compensation ordering.
type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *callRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.calls...)
}

// fakeInstances is a minimal, in-memory instance.Service backing just what
// the saga calls (Get/Transition/SetNodeID) — the other methods panic if
// hit, since the saga never calls them.
type fakeInstances struct {
	mu   sync.Mutex
	rec  *callRecorder
	data map[string]instance.Instance
}

func newFakeInstances(rec *callRecorder, inst instance.Instance) *fakeInstances {
	return &fakeInstances{rec: rec, data: map[string]instance.Instance{inst.ID: inst}}
}

func (f *fakeInstances) Get(ctx context.Context, tenantID, id string) (instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.data[id]
	if !ok {
		return instance.Instance{}, instance.ErrNotFound
	}
	return i, nil
}

func (f *fakeInstances) Transition(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.data[id]
	if !ok {
		return instance.Instance{}, instance.ErrNotFound
	}
	if !instance.CanTransition(i.Status, to) {
		return instance.Instance{}, instance.ErrInvalidTransition
	}
	i.Status = to
	f.data[id] = i
	f.rec.record("transition:" + string(to))
	return i, nil
}

func (f *fakeInstances) SetNodeID(ctx context.Context, tenantID, id, nodeID string) (instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.data[id]
	if !ok || i.Status != instance.StatusProvisioning {
		return instance.Instance{}, instance.ErrInvalidTransition
	}
	i.NodeID = &nodeID
	f.data[id] = i
	f.rec.record("set_node_id:" + nodeID)
	return i, nil
}

func (f *fakeInstances) SetIPAddress(ctx context.Context, tenantID, id, ip string) (instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.data[id]
	if !ok || i.Status != instance.StatusProvisioning {
		return instance.Instance{}, instance.ErrInvalidTransition
	}
	i.IPAddress = &ip
	f.data[id] = i
	f.rec.record("set_ip_address:" + ip)
	return i, nil
}

func (f *fakeInstances) status(id string) instance.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data[id].Status
}

func (f *fakeInstances) Create(ctx context.Context, tenantID string, in instance.CreateInput) (instance.Instance, error) {
	panic("not used in saga tests")
}
func (f *fakeInstances) List(ctx context.Context, tenantID string) ([]instance.Instance, error) {
	panic("not used in saga tests")
}
func (f *fakeInstances) Delete(ctx context.Context, tenantID, id string) (instance.Instance, error) {
	panic("not used in saga tests")
}

// fakeNodes backs just Reserve/Release.
type fakeNodes struct {
	rec        *callRecorder
	reserveErr error
	releaseErr error
}

func (f *fakeNodes) Reserve(ctx context.Context, id string, cpu, memoryMB, diskGB int) (node.Node, error) {
	f.rec.record("reserve:" + id)
	if f.reserveErr != nil {
		return node.Node{}, f.reserveErr
	}
	return node.Node{ID: id}, nil
}

func (f *fakeNodes) Release(ctx context.Context, id string, cpu, memoryMB, diskGB int) (node.Node, error) {
	f.rec.record("release:" + id)
	return node.Node{ID: id}, f.releaseErr
}

func (f *fakeNodes) Register(ctx context.Context, in node.RegisterInput) (node.RegisterResult, error) {
	panic("not used in saga tests")
}
func (f *fakeNodes) List(ctx context.Context) ([]node.Node, error) { panic("not used in saga tests") }
func (f *fakeNodes) Get(ctx context.Context, id string) (node.Node, error) {
	panic("not used in saga tests")
}
func (f *fakeNodes) Heartbeat(ctx context.Context, id string, in node.HeartbeatInput) error {
	panic("not used in saga tests")
}
func (f *fakeNodes) AuthenticateNodeToken(ctx context.Context, id, rawToken string) error {
	panic("not used in saga tests")
}
func (f *fakeNodes) Drain(ctx context.Context, id string) (node.Node, error) {
	panic("not used in saga tests")
}

// fakeScheduler backs Schedule.
type fakeScheduler struct {
	rec  *callRecorder
	node node.Node
	err  error
}

func (f *fakeScheduler) Schedule(ctx context.Context, req scheduler.ResourceRequest) (node.Node, error) {
	f.rec.record("schedule")
	if f.err != nil {
		return node.Node{}, f.err
	}
	return f.node, nil
}

// recordingSteps returns the seven saga step funcs, all recording their
// name on the shared recorder; failStep (if non-empty) returns failErr
// instead of succeeding — this is what lets a single test force a failure
// at exactly one step.
func recordingSteps(rec *callRecorder, failStep string, failErr error) (
	createDisk DiskCreator, deleteDisk DiskDeleter,
	createNetwork NetworkCreator, deleteNetwork NetworkDeleter,
	createVM VMCreator, deleteVM VMDeleter, startVM VMStarter,
) {
	step := func(name string) error {
		rec.record(name)
		if name == failStep {
			return failErr
		}
		return nil
	}
	createDisk = func(ctx context.Context, instanceID, tenantID, nodeID string, diskGB int) error {
		return step("create_disk")
	}
	deleteDisk = func(ctx context.Context, instanceID string) error { return step("delete_disk") }
	createNetwork = func(ctx context.Context, instanceID string) (string, error) {
		return "10.20.0.99", step("create_network")
	}
	deleteNetwork = func(ctx context.Context, instanceID string) error { return step("delete_network") }
	createVM = func(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) error {
		return step("create_vm")
	}
	deleteVM = func(ctx context.Context, instanceID, nodeID string) error { return step("delete_vm") }
	startVM = func(ctx context.Context, instanceID, nodeID string) error { return step("start_vm") }
	return
}

func newTestSaga(rec *callRecorder, insts *fakeInstances, nodes *fakeNodes, sched *fakeScheduler, failStep string, failErr error) *Saga {
	createDisk, deleteDisk, createNetwork, deleteNetwork, createVM, deleteVM, startVM := recordingSteps(rec, failStep, failErr)
	return &Saga{
		instances: insts, nodes: nodes, scheduler: sched, logger: testLogger(),
		createDisk: createDisk, deleteDisk: deleteDisk,
		createNetwork: createNetwork, deleteNetwork: deleteNetwork,
		createVM: createVM, deleteVM: deleteVM, startVM: startVM,
	}
}

func newPendingInstance() instance.Instance {
	return instance.Instance{ID: "inst-1", TenantID: "tenant-1", CPU: 2, MemoryMB: 4096, DiskGB: 50, Status: instance.StatusPending}
}

func assertTrace(t *testing.T, rec *callRecorder, want []string) {
	t.Helper()
	got := rec.list()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("call trace =\n  %v\nwant\n  %v", got, want)
	}
}

func TestSagaHappyPath(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}
	saga := newTestSaga(rec, insts, nodes, sched, "", nil)

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if insts.status("inst-1") != instance.StatusRunning {
		t.Errorf("final status = %q, want RUNNING", insts.status("inst-1"))
	}
	assertTrace(t, rec, []string{
		"transition:PROVISIONING", "schedule", "reserve:node-1", "set_node_id:node-1",
		"create_disk", "create_network", "set_ip_address:10.20.0.99", "create_vm", "start_vm", "transition:RUNNING",
	})
}

func TestSagaFailsAtSchedule(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, err: scheduler.ErrNoCapacity}
	saga := newTestSaga(rec, insts, nodes, sched, "", nil)

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected an error")
	}

	if insts.status("inst-1") != instance.StatusError {
		t.Errorf("final status = %q, want ERROR", insts.status("inst-1"))
	}
	// Nothing was ever reserved, so there's nothing to compensate.
	assertTrace(t, rec, []string{"transition:PROVISIONING", "schedule", "transition:ERROR"})
}

func TestSagaFailsAtReserve(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec, reserveErr: node.ErrInsufficientCapacity}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}
	saga := newTestSaga(rec, insts, nodes, sched, "", nil)

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected an error")
	}

	if insts.status("inst-1") != instance.StatusError {
		t.Errorf("final status = %q, want ERROR", insts.status("inst-1"))
	}
	assertTrace(t, rec, []string{"transition:PROVISIONING", "schedule", "reserve:node-1", "transition:ERROR"})
}

func TestSagaFailsAtCreateDisk(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}
	saga := newTestSaga(rec, insts, nodes, sched, "create_disk", errors.New("disk backend unavailable"))

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected an error")
	}

	if insts.status("inst-1") != instance.StatusError {
		t.Errorf("final status = %q, want ERROR", insts.status("inst-1"))
	}
	assertTrace(t, rec, []string{
		"transition:PROVISIONING", "schedule", "reserve:node-1", "set_node_id:node-1",
		"create_disk", "release:node-1", "transition:ERROR",
	})
}

func TestSagaFailsAtCreateNetwork(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}
	saga := newTestSaga(rec, insts, nodes, sched, "create_network", errors.New("network backend unavailable"))

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected an error")
	}

	// Reverse order of what succeeded: disk was created, so it's undone
	// before releasing resources.
	assertTrace(t, rec, []string{
		"transition:PROVISIONING", "schedule", "reserve:node-1", "set_node_id:node-1",
		"create_disk", "create_network", "delete_disk", "release:node-1", "transition:ERROR",
	})
}

func TestSagaFailsAtCreateVM(t *testing.T) {
	// This is spec section 26's own worked example: Create VM fails ->
	// delete network -> delete disk -> release resources.
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}
	saga := newTestSaga(rec, insts, nodes, sched, "create_vm", errors.New("hypervisor unavailable"))

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected an error")
	}

	assertTrace(t, rec, []string{
		"transition:PROVISIONING", "schedule", "reserve:node-1", "set_node_id:node-1",
		"create_disk", "create_network", "set_ip_address:10.20.0.99", "create_vm",
		"delete_network", "delete_disk", "release:node-1", "transition:ERROR",
	})
}

func TestSagaFailsAtStartVM(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}
	saga := newTestSaga(rec, insts, nodes, sched, "start_vm", errors.New("VM failed to boot"))

	if err := saga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected an error")
	}

	assertTrace(t, rec, []string{
		"transition:PROVISIONING", "schedule", "reserve:node-1", "set_node_id:node-1",
		"create_disk", "create_network", "set_ip_address:10.20.0.99", "create_vm", "start_vm",
		"delete_vm", "delete_network", "delete_disk", "release:node-1", "transition:ERROR",
	})
}

func TestSagaRetryAfterFailureSucceeds(t *testing.T) {
	rec := &callRecorder{}
	insts := newFakeInstances(rec, newPendingInstance())
	nodes := &fakeNodes{rec: rec}
	sched := &fakeScheduler{rec: rec, node: node.Node{ID: "node-1"}}

	failingSaga := newTestSaga(rec, insts, nodes, sched, "create_vm", errors.New("hypervisor unavailable"))
	if err := failingSaga.Provision(context.Background(), "tenant-1", "inst-1"); err == nil {
		t.Fatal("expected the first attempt to fail")
	}
	if insts.status("inst-1") != instance.StatusError {
		t.Fatalf("status after first attempt = %q, want ERROR", insts.status("inst-1"))
	}

	// Simulate a job retry: Provision is called again on the same
	// instance, now sitting in ERROR. This requires ERROR->PROVISIONING
	// to be a valid transition (internal/instance/models.go).
	retrySaga := newTestSaga(rec, insts, nodes, sched, "", nil)
	if err := retrySaga.Provision(context.Background(), "tenant-1", "inst-1"); err != nil {
		t.Fatalf("retry Provision: %v", err)
	}
	if insts.status("inst-1") != instance.StatusRunning {
		t.Errorf("status after retry = %q, want RUNNING", insts.status("inst-1"))
	}
}
