package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/milosursulovic/nebula/internal/node"
)

// AgentSteps implements the VM saga steps by calling out to the target
// node's nebula-agent over HTTP (spec section 27: "the control plane
// should NOT directly execute arbitrary shell commands on nodes... the
// agent is the abstraction layer"). The agent's own VM backend is still a
// mock (Phase 11/KVM replaces it) — what's real here is the network call.
type AgentSteps struct {
	nodes      node.Service
	agentPort  string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAgentSteps builds AgentSteps. A short client timeout means an
// unreachable agent fails the saga step promptly (feeding into the
// existing compensation + job retry/backoff) rather than hanging a
// worker goroutine.
func NewAgentSteps(nodes node.Service, agentPort string, logger *slog.Logger) *AgentSteps {
	return &AgentSteps{
		nodes:      nodes,
		agentPort:  agentPort,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
	}
}

func (a *AgentSteps) agentURL(ctx context.Context, nodeID, path string) (string, error) {
	n, err := a.nodes.Get(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("resolve node %s: %w", nodeID, err)
	}
	return fmt.Sprintf("http://%s:%s%s", n.IP, a.agentPort, path), nil
}

func (a *AgentSteps) CreateVM(ctx context.Context, instanceID, nodeID string, spec InstanceSpec) error {
	url, err := a.agentURL(ctx, nodeID, "/vms")
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"instance_id": instanceID,
		"cpu":         spec.CPU,
		"memory_mb":   spec.MemoryMB,
		"disk_gb":     spec.DiskGB,
		"image":       spec.Image,
	})
	if err != nil {
		return fmt.Errorf("marshal create vm request: %w", err)
	}

	if err := a.do(ctx, http.MethodPost, url, body, http.StatusCreated); err != nil {
		return fmt.Errorf("agent create vm: %w", err)
	}
	a.logger.Info("provisioning: VM created via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

func (a *AgentSteps) DeleteVM(ctx context.Context, instanceID, nodeID string) error {
	url, err := a.agentURL(ctx, nodeID, "/vms/"+instanceID)
	if err != nil {
		return err
	}

	if err := a.do(ctx, http.MethodDelete, url, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("agent delete vm: %w", err)
	}
	a.logger.Info("provisioning: VM deleted via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

func (a *AgentSteps) StartVM(ctx context.Context, instanceID, nodeID string) error {
	url, err := a.agentURL(ctx, nodeID, "/vms/"+instanceID+"/start")
	if err != nil {
		return err
	}

	if err := a.do(ctx, http.MethodPost, url, nil, http.StatusOK); err != nil {
		return fmt.Errorf("agent start vm: %w", err)
	}
	a.logger.Info("provisioning: VM started via agent", "instance_id", instanceID, "node_id", nodeID)
	return nil
}

func (a *AgentSteps) do(ctx context.Context, method, url string, body []byte, wantStatus int) error {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		return fmt.Errorf("unexpected status %d from %s %s", resp.StatusCode, method, url)
	}
	return nil
}
