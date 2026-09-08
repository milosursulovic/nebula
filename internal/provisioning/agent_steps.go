package provisioning

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	logger    *slog.Logger
}

// NewAgentSteps builds AgentSteps. tlsCAFile is the agent's own
// (self-signed, dev-only) server certificate, pinned as the trust anchor
// — server-authenticated TLS only this phase; mTLS (agent verifying the
// caller) is explicitly deferred.
func NewAgentSteps(nodes node.Service, agentPort, tlsCAFile string, logger *slog.Logger) (*AgentSteps, error) {
	creds, err := credentials.NewClientTLSFromFile(tlsCAFile, "")
	if err != nil {
		return nil, fmt.Errorf("load agent tls ca file: %w", err)
	}
	return &AgentSteps{nodes: nodes, agentPort: agentPort, creds: creds, logger: logger}, nil
}

// dial opens a short-lived connection to nodeID's agent — matches the
// prior HTTP client's per-call simplicity, no connection-pooling this
// phase doesn't need.
func (a *AgentSteps) dial(ctx context.Context, nodeID string) (agentpb.NebulaAgentClient, func(), error) {
	n, err := a.nodes.Get(ctx, nodeID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve node %s: %w", nodeID, err)
	}

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%s", n.IP, a.agentPort), grpc.WithTransportCredentials(a.creds))
	if err != nil {
		return nil, nil, fmt.Errorf("dial agent: %w", err)
	}
	return agentpb.NewNebulaAgentClient(conn), func() { conn.Close() }, nil
}

func (a *AgentSteps) CreateVM(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) error {
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

func (a *AgentSteps) DeleteVM(ctx context.Context, instanceID, nodeID string) error {
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

func (a *AgentSteps) StartVM(ctx context.Context, instanceID, nodeID string) error {
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
