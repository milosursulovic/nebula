package agent

import (
	"context"
	"errors"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/milosursulovic/nebula/internal/agentpb"
)

// grpcServer implements agentpb.NebulaAgentServer over a Hypervisor (spec
// section 30) — MockHypervisor by default, LibvirtHypervisor when built
// with -tags libvirt. This is a transport swap over whichever backend is
// wired in, not a redesign of either.
type grpcServer struct {
	agentpb.UnimplementedNebulaAgentServer
	hypervisor Hypervisor
}

// NewServer builds nebula-agent's TLS-enabled gRPC server, listening on
// addr. certFile/keyFile are the agent's own server certificate (spec
// section 64: "Implement TLS" — server-authenticated only; mTLS, client
// cert verification, is explicitly "later"). Server reflection is
// registered so grpcurl can introspect the service without needing the
// .proto file on hand.
func NewServer(addr, certFile, keyFile string, hypervisor Hypervisor) (*grpc.Server, net.Listener, error) {
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return nil, nil, err
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	srv := grpc.NewServer(grpc.Creds(creds))
	agentpb.RegisterNebulaAgentServer(srv, &grpcServer{hypervisor: hypervisor})
	reflection.Register(srv)

	return srv, lis, nil
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
	if err := g.hypervisor.CreateVM(ctx, spec); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &agentpb.CreateVMResponse{InstanceId: spec.InstanceID, Status: string(VMStatusStopped)}, nil
}

func (g *grpcServer) DeleteVM(ctx context.Context, req *agentpb.DeleteVMRequest) (*agentpb.DeleteVMResponse, error) {
	if err := g.hypervisor.DeleteVM(ctx, req.GetInstanceId()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &agentpb.DeleteVMResponse{}, nil
}

func (g *grpcServer) StartVM(ctx context.Context, req *agentpb.StartVMRequest) (*agentpb.StartVMResponse, error) {
	if err := g.hypervisor.StartVM(ctx, req.GetInstanceId()); err != nil {
		return nil, vmError(err)
	}
	vm, err := g.hypervisor.GetVMStatus(ctx, req.GetInstanceId())
	if err != nil {
		return nil, vmError(err)
	}
	return &agentpb.StartVMResponse{InstanceId: vm.InstanceID, Status: string(vm.Status)}, nil
}

func (g *grpcServer) StopVM(ctx context.Context, req *agentpb.StopVMRequest) (*agentpb.StopVMResponse, error) {
	if err := g.hypervisor.StopVM(ctx, req.GetInstanceId()); err != nil {
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
