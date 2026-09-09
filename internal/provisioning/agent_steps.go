package provisioning

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/milosursulovic/nebula/internal/agentpb"
	"github.com/milosursulovic/nebula/internal/node"
)

// callTimeout bounds a single gRPC call to the agent — an unreachable
// agent fails the saga step promptly (feeding into the existing
// compensation + job retry/backoff) rather than hanging a worker
// goroutine.
const callTimeout = 5 * time.Second

// AgentSteps implements the VM saga steps by calling out to the target
// node's nebula-agent over gRPC (spec section 64: move control plane ->
// agent communication to gRPC; section 27: "the control plane should NOT
// directly execute arbitrary shell commands on nodes... the agent is the
// abstraction layer"). The agent's own VM backend is still a mock (Phase
// 11/KVM replaces it) — what's real here is the network call.
type AgentSteps struct {
	nodes     node.Service
	agentPort string
	creds     credentials.TransportCredentials
	breakers  *circuitBreakers
	logger    *slog.Logger
}

// NewAgentSteps builds AgentSteps. tlsCAFile is the agent's own
// (self-signed, dev-only) server certificate, pinned as the trust anchor
// for verifying it; clientCertFile/clientKeyFile is nebula-api's own
// client identity, presented to the agent's now-mandatory mTLS (Phase
// 18 closes the "mTLS later" deferral from Phase 10/spec section 51).
func NewAgentSteps(nodes node.Service, agentPort, tlsCAFile, clientCertFile, clientKeyFile string, logger *slog.Logger) (*AgentSteps, error) {
	serverCAPEM, err := os.ReadFile(tlsCAFile)
	if err != nil {
		return nil, fmt.Errorf("read agent tls ca file: %w", err)
	}
	serverCAs := x509.NewCertPool()
	if !serverCAs.AppendCertsFromPEM(serverCAPEM) {
		return nil, fmt.Errorf("parse agent tls ca file %s: no certificates found", tlsCAFile)
	}

	clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      serverCAs,
	})
	return &AgentSteps{nodes: nodes, agentPort: agentPort, creds: creds, breakers: newCircuitBreakers(), logger: logger}, nil
}

// dial opens a short-lived connection to nodeID's agent — matches the
// prior HTTP client's per-call simplicity, no connection-pooling this
// phase doesn't need. Checks the per-node circuit breaker first (spec
// section 72) — a node that's failed circuitBreakerFailureThreshold
// times in a row fails immediately here rather than paying dial + the
// full callTimeout again on a node that's very likely still down.
func (a *AgentSteps) dial(ctx context.Context, nodeID string) (agentpb.NebulaAgentClient, func(), error) {
	if !a.breakers.allow(nodeID) {
		return nil, nil, ErrCircuitOpen
	}

	n, err := a.nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve node %s: %w", nodeID, err)
	}

	// otelgrpc's stats handler propagates the active span (from the job's
	// re-hydrated trace context, see instance.Repository.CreateWithJob /
	// job.Pool.execute) over gRPC metadata automatically — spec section
	// 36's "gRPC -> Agent" hop.
	conn, err := grpc.NewClient(fmt.Sprintf("%s:%s", n.IP, a.agentPort),
		grpc.WithTransportCredentials(a.creds),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("dial agent: %w", err)
	}
	return agentpb.NewNebulaAgentClient(conn), func() { conn.Close() }, nil
}

func (a *AgentSteps) CreateVM(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) (err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := client.CreateVM(callCtx, &agentpb.CreateVMRequest{
		InstanceId: instanceID, Cpu: int32(spec.CPU), MemoryMb: int32(spec.MemoryMB), DiskGb: int32(spec.DiskGB), Image: spec.Image,
	}); err != nil {
		return fmt.Errorf("agent create vm: %w", err)
	}
	a.logger.Info("provisioning: VM created via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

func (a *AgentSteps) DeleteVM(ctx context.Context, instanceID, nodeID string) (err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := client.DeleteVM(callCtx, &agentpb.DeleteVMRequest{InstanceId: instanceID}); err != nil {
		return fmt.Errorf("agent delete vm: %w", err)
	}
	a.logger.Info("provisioning: VM deleted via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

func (a *AgentSteps) StartVM(ctx context.Context, instanceID, nodeID string) (err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := client.StartVM(callCtx, &agentpb.StartVMRequest{InstanceId: instanceID}); err != nil {
		return fmt.Errorf("agent start vm: %w", err)
	}
	a.logger.Info("provisioning: VM started via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

// StopVM is the CLI-driven (spec section 49's "nebula instance stop")
// counterpart to StartVM — same dial/call/log shape, different RPC.
func (a *AgentSteps) StopVM(ctx context.Context, instanceID, nodeID string) (err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := client.StopVM(callCtx, &agentpb.StopVMRequest{InstanceId: instanceID}); err != nil {
		return fmt.Errorf("agent stop vm: %w", err)
	}
	a.logger.Info("provisioning: VM stopped via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

// CreateDiskFile, DeleteDiskFile, and ResizeDiskFile satisfy
// internal/storage.AgentDiskClient structurally (that interface is
// package-local to internal/storage — no import needed here, same
// pattern as internal/scheduler.NodeLister). AgentSteps already has the
// TLS-gRPC dial logic the VM steps use; these just call the disk RPCs
// (spec section 34, Phase 13) over the same connection shape.

func (a *AgentSteps) CreateDiskFile(ctx context.Context, nodeID, diskID string, sizeGB int) (path string, err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return "", err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	resp, err := client.CreateDisk(callCtx, &agentpb.CreateDiskRequest{DiskId: diskID, SizeGb: int32(sizeGB)})
	if err != nil {
		return "", fmt.Errorf("agent create disk: %w", err)
	}
	a.logger.Info("storage: disk created via agent", "disk_id", diskID, "node_id", nodeID)
	return resp.GetPath(), nil
}

func (a *AgentSteps) DeleteDiskFile(ctx context.Context, nodeID, diskID string) (err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := client.DeleteDisk(callCtx, &agentpb.DeleteDiskRequest{DiskId: diskID}); err != nil {
		return fmt.Errorf("agent delete disk: %w", err)
	}
	a.logger.Info("storage: disk deleted via agent", "disk_id", diskID, "node_id", nodeID)
	return nil
}

func (a *AgentSteps) ResizeDiskFile(ctx context.Context, nodeID, diskID string, newSizeGB int) (err error) {
	defer func() { a.breakers.recordResult(nodeID, err) }()

	client, closeConn, err := a.dial(ctx, nodeID)
	if err != nil {
		return err
	}
	defer closeConn()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if _, err := client.ResizeDisk(callCtx, &agentpb.ResizeDiskRequest{DiskId: diskID, NewSizeGb: int32(newSizeGB)}); err != nil {
		return fmt.Errorf("agent resize disk: %w", err)
	}
	a.logger.Info("storage: disk resized via agent", "disk_id", diskID, "node_id", nodeID)
	return nil
}
