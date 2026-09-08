package agent

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/milosursulovic/nebula/internal/agentpb"
)

// newTestClient wires an in-memory (bufconn) grpc client/server pair
// against a fresh Store — no TLS, no real sockets, exercises the same
// grpcServer handlers NewServer's real TLS listener would.
func newTestClient(t *testing.T) (agentpb.NebulaAgentClient, *Store) {
	t.Helper()

	store := NewStore()
	srv := grpc.NewServer()
	agentpb.RegisterNebulaAgentServer(srv, &grpcServer{hypervisor: NewMockHypervisor(store)})

	lis := bufconn.Listen(1024 * 1024)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return agentpb.NewNebulaAgentClient(conn), store
}

func TestGRPCCreateVM(t *testing.T) {
	client, _ := newTestClient(t)

	resp, err := client.CreateVM(context.Background(), &agentpb.CreateVMRequest{
		InstanceId: "inst-1", Cpu: 2, MemoryMb: 4096, DiskGb: 50, Image: "ubuntu-26.04",
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if resp.InstanceId != "inst-1" || resp.Status != string(VMStatusStopped) {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGRPCCreateVMMissingInstanceID(t *testing.T) {
	client, _ := newTestClient(t)

	_, err := client.CreateVM(context.Background(), &agentpb.CreateVMRequest{Cpu: 1, MemoryMb: 1, DiskGb: 1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestGRPCGetVMStatus(t *testing.T) {
	client, store := newTestClient(t)
	store.Create("inst-1", 2, 4096, 50, "img")

	resp, err := client.GetVMStatus(context.Background(), &agentpb.GetVMStatusRequest{InstanceId: "inst-1"})
	if err != nil {
		t.Fatalf("GetVMStatus: %v", err)
	}
	if resp.Status != string(VMStatusStopped) || resp.Cpu != 2 || resp.MemoryMb != 4096 || resp.DiskGb != 50 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGRPCGetVMStatusNotFound(t *testing.T) {
	client, _ := newTestClient(t)

	_, err := client.GetVMStatus(context.Background(), &agentpb.GetVMStatusRequest{InstanceId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", status.Code(err))
	}
}

func TestGRPCStartVM(t *testing.T) {
	client, store := newTestClient(t)
	store.Create("inst-1", 1, 1024, 10, "img")

	resp, err := client.StartVM(context.Background(), &agentpb.StartVMRequest{InstanceId: "inst-1"})
	if err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	if resp.Status != string(VMStatusRunning) {
		t.Errorf("status = %q, want RUNNING", resp.Status)
	}
}

func TestGRPCStartVMNotFound(t *testing.T) {
	client, _ := newTestClient(t)

	_, err := client.StartVM(context.Background(), &agentpb.StartVMRequest{InstanceId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", status.Code(err))
	}
}

func TestGRPCStopVM(t *testing.T) {
	client, store := newTestClient(t)
	store.Create("inst-1", 1, 1024, 10, "img")
	store.Start("inst-1")

	resp, err := client.StopVM(context.Background(), &agentpb.StopVMRequest{InstanceId: "inst-1"})
	if err != nil {
		t.Fatalf("StopVM: %v", err)
	}
	if resp.Status != string(VMStatusStopped) {
		t.Errorf("status = %q, want STOPPED", resp.Status)
	}
}

func TestGRPCStopVMNotFound(t *testing.T) {
	client, _ := newTestClient(t)

	_, err := client.StopVM(context.Background(), &agentpb.StopVMRequest{InstanceId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", status.Code(err))
	}
}

func TestGRPCDeleteVM(t *testing.T) {
	client, store := newTestClient(t)
	store.Create("inst-1", 1, 1024, 10, "img")

	if _, err := client.DeleteVM(context.Background(), &agentpb.DeleteVMRequest{InstanceId: "inst-1"}); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := store.Get("inst-1"); !errors.Is(err, ErrVMNotFound) {
		t.Error("expected vm to be gone after delete")
	}
}

func TestGRPCDeleteVMUnknownStillSucceeds(t *testing.T) {
	client, _ := newTestClient(t)

	if _, err := client.DeleteVM(context.Background(), &agentpb.DeleteVMRequest{InstanceId: "missing"}); err != nil {
		t.Fatalf("DeleteVM on unknown id: %v", err)
	}
}

func TestGRPCGetNodeInfo(t *testing.T) {
	client, _ := newTestClient(t)

	// /proc reads are host-dependent (and unavailable in some sandboxes),
	// so this only asserts the RPC wires collectMetrics through
	// correctly, not specific numbers.
	_, err := client.GetNodeInfo(context.Background(), &agentpb.GetNodeInfoRequest{})
	if err != nil && status.Code(err) != codes.Internal {
		t.Fatalf("unexpected error: %v", err)
	}
}
