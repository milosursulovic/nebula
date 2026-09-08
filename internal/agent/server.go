package agent

import (
	"context"
	"errors"
	"net"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/milosursulovic/nebula/internal/agentpb"
)

// tracer's spans are the "Agent -> libvirt" hop of spec section 36's
// trace chain — otelgrpc's server stats handler continues the caller's
// trace automatically; this wraps each RPC's actual hypervisor/disk-store
// call as one child span of it.
var tracer = otel.Tracer("nebula-agent")

// grpcServer implements agentpb.NebulaAgentServer over a Hypervisor (spec
// section 30) — MockHypervisor by default, LibvirtHypervisor when built
// with -tags libvirt — plus a DiskStore for real sparse-file disk
// management (spec section 34, Phase 13). VM ops are a transport swap
// over whichever hypervisor backend is wired in; disk ops are real
// either way (see disk_store.go's doc comment).
type grpcServer struct {
	agentpb.UnimplementedNebulaAgentServer
	hypervisor Hypervisor
	disks      *DiskStore
}

// NewServer builds nebula-agent's TLS-enabled gRPC server, listening on
// addr. certFile/keyFile are the agent's own server certificate (spec
// section 64: "Implement TLS" — server-authenticated only; mTLS, client
// cert verification, is explicitly "later"). Server reflection is
// registered so grpcurl can introspect the service without needing the
// .proto file on hand.
func NewServer(addr, certFile, keyFile string, hypervisor Hypervisor, disks *DiskStore) (*grpc.Server, net.Listener, error) {
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return nil, nil, err
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	srv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	agentpb.RegisterNebulaAgentServer(srv, &grpcServer{hypervisor: hypervisor, disks: disks})
	reflection.Register(srv)

	return srv, lis, nil
}

// withSpan runs fn as a child span named name, continuing whatever trace
// otelgrpc's server handler already attached to ctx.
func withSpan(ctx context.Context, name string, fn func(context.Context) error) error {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func (g *grpcServer) GetNodeInfo(ctx context.Context, _ *agentpb.GetNodeInfoRequest) (*agentpb.GetNodeInfoResponse, error) {
	m, err := collectMetrics(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to collect metrics")
	}
	count, err := g.hypervisor.CountVMs(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to count vms")
	}
	return &agentpb.GetNodeInfoResponse{
		CpuUsage:         m.CPUUsage,
		MemoryUsedMb:     int64(m.MemUsedMB),
		DiskUsedGb:       int64(m.DiskUsedGB),
		LoadAverage:      m.LoadAverage,
		RunningInstances: int32(count),
	}, nil
}

func (g *grpcServer) CreateVM(ctx context.Context, req *agentpb.CreateVMRequest) (*agentpb.CreateVMResponse, error) {
	if req.GetInstanceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id is required")
	}
	spec := VMSpec{
		InstanceID: req.GetInstanceId(),
		CPU:        int(req.GetCpu()),
		MemoryMB:   int(req.GetMemoryMb()),
		DiskGB:     int(req.GetDiskGb()),
		Image:      req.GetImage(),
	}
	err := withSpan(ctx, "hypervisor.CreateVM", func(ctx context.Context) error {
		return g.hypervisor.CreateVM(ctx, spec)
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &agentpb.CreateVMResponse{InstanceId: spec.InstanceID, Status: string(VMStatusStopped)}, nil
}

func (g *grpcServer) DeleteVM(ctx context.Context, req *agentpb.DeleteVMRequest) (*agentpb.DeleteVMResponse, error) {
	err := withSpan(ctx, "hypervisor.DeleteVM", func(ctx context.Context) error {
		return g.hypervisor.DeleteVM(ctx, req.GetInstanceId())
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &agentpb.DeleteVMResponse{}, nil
}

func (g *grpcServer) StartVM(ctx context.Context, req *agentpb.StartVMRequest) (*agentpb.StartVMResponse, error) {
	err := withSpan(ctx, "hypervisor.StartVM", func(ctx context.Context) error {
		return g.hypervisor.StartVM(ctx, req.GetInstanceId())
	})
	if err != nil {
		return nil, vmError(err)
	}
	vm, err := g.hypervisor.GetVMStatus(ctx, req.GetInstanceId())
	if err != nil {
		return nil, vmError(err)
	}
	return &agentpb.StartVMResponse{InstanceId: vm.InstanceID, Status: string(vm.Status)}, nil
}

func (g *grpcServer) StopVM(ctx context.Context, req *agentpb.StopVMRequest) (*agentpb.StopVMResponse, error) {
	err := withSpan(ctx, "hypervisor.StopVM", func(ctx context.Context) error {
		return g.hypervisor.StopVM(ctx, req.GetInstanceId())
	})
	if err != nil {
		return nil, vmError(err)
	}
	vm, err := g.hypervisor.GetVMStatus(ctx, req.GetInstanceId())
	if err != nil {
		return nil, vmError(err)
	}
	return &agentpb.StopVMResponse{InstanceId: vm.InstanceID, Status: string(vm.Status)}, nil
}

func (g *grpcServer) GetVMStatus(ctx context.Context, req *agentpb.GetVMStatusRequest) (*agentpb.GetVMStatusResponse, error) {
	vm, err := g.hypervisor.GetVMStatus(ctx, req.GetInstanceId())
	if err != nil {
		return nil, vmError(err)
	}
	return &agentpb.GetVMStatusResponse{
		InstanceId: vm.InstanceID,
		Status:     string(vm.Status),
		Cpu:        int32(vm.CPU),
		MemoryMb:   int32(vm.MemoryMB),
		DiskGb:     int32(vm.DiskGB),
		Image:      vm.Image,
	}, nil
}

func vmError(err error) error {
	if errors.Is(err, ErrVMNotFound) {
		return status.Error(codes.NotFound, "no vm for this instance id")
	}
	return status.Error(codes.Internal, err.Error())
}

func (g *grpcServer) CreateDisk(ctx context.Context, req *agentpb.CreateDiskRequest) (*agentpb.CreateDiskResponse, error) {
	if req.GetDiskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "disk_id is required")
	}
	var path string
	err := withSpan(ctx, "diskStore.Create", func(ctx context.Context) error {
		var err error
		path, err = g.disks.Create(req.GetDiskId(), int(req.GetSizeGb()))
		return err
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &agentpb.CreateDiskResponse{DiskId: req.GetDiskId(), Path: path}, nil
}

func (g *grpcServer) DeleteDisk(ctx context.Context, req *agentpb.DeleteDiskRequest) (*agentpb.DeleteDiskResponse, error) {
	err := withSpan(ctx, "diskStore.Delete", func(ctx context.Context) error {
		return g.disks.Delete(req.GetDiskId())
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &agentpb.DeleteDiskResponse{}, nil
}

func (g *grpcServer) ResizeDisk(ctx context.Context, req *agentpb.ResizeDiskRequest) (*agentpb.ResizeDiskResponse, error) {
	err := withSpan(ctx, "diskStore.Resize", func(ctx context.Context) error {
		return g.disks.Resize(req.GetDiskId(), int(req.GetNewSizeGb()))
	})
	if err != nil {
		return nil, diskError(err)
	}
	return &agentpb.ResizeDiskResponse{}, nil
}

func diskError(err error) error {
	if errors.Is(err, ErrDiskNotFound) {
		return status.Error(codes.NotFound, "no disk for this id")
	}
	return status.Error(codes.Internal, err.Error())
}
