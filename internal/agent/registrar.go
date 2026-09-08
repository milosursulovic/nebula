package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Registrar registers this agent with the control plane and keeps it
// heartbeating (spec sections 10/11's client side — the control-plane
// endpoints already exist, nothing has ever called them until this agent).
type Registrar struct {
	cfg        Config
	hypervisor Hypervisor
	client     *http.Client
	logger     *slog.Logger

	nodeID    string
	nodeToken string
}

func NewRegistrar(cfg Config, hypervisor Hypervisor, logger *slog.Logger) *Registrar {
	return &Registrar{
		cfg:        cfg,
		hypervisor: hypervisor,
		client:     &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
	}
}

// NodeID returns the ID assigned at registration (empty until Register
// succeeds).
func (r *Registrar) NodeID() string {
	return r.nodeID
}

type registerRequest struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	CPU      int    `json:"cpu"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
}

type registerResponse struct {
	NodeID    string `json:"node_id"`
	NodeToken string `json:"node_token"`
}

// Register calls the control plane's POST /nodes/register, retrying with
// backoff — nebula-api has no readiness gate compose can depend_on, so it
// may not be migrated yet when this agent starts.
func (r *Registrar) Register(ctx context.Context) error {
	body, err := json.Marshal(registerRequest{
		Hostname: r.cfg.Hostname,
		IP:       r.cfg.IP,
		CPU:      r.cfg.CPU,
		MemoryMB: r.cfg.MemoryMB,
		DiskGB:   r.cfg.DiskGB,
	})
	if err != nil {
		return fmt.Errorf("marshal register request: %w", err)
	}

	const maxAttempts = 10
	backoff := time.Second
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var resp registerResponse
		status, err := r.post(ctx, "/api/v1/nodes/register", r.cfg.BootstrapSecret, body, http.StatusCreated, &resp)
		if err == nil {
			r.nodeID = resp.NodeID
			r.nodeToken = resp.NodeToken
			r.logger.Info("nebula-agent: registered", "node_id", r.nodeID, "hostname", r.cfg.Hostname)
			return nil
		}

		// 409 (hostname already registered) is no longer necessarily
		// permanent (Phase 16): the control plane reclaims a hostname
		// once its existing node row goes OFFLINE (30s of missed
		// heartbeats), so a restarted agent racing that window — its own
		// prior row not yet demoted — just needs to keep retrying, same
		// as any other failure. This backoff schedule already clears 30s
		// well before its budget runs out (1+2+4+8+16=31s by attempt 6),
		// so no special-casing is needed; a 409 held by a genuinely
		// still-live node (real hostname collision) correctly exhausts
		// the same retry budget and fails below, same as any other
		// unrecoverable error.
		lastErr = err
		r.logger.Warn("nebula-agent: registration attempt failed, retrying",
			"attempt", attempt, "status", status, "error", err)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}

	return fmt.Errorf("registration failed after %d attempts: %w", maxAttempts, lastErr)
}

type heartbeatRequest struct {
	CPUUsage         float64 `json:"cpu_usage"`
	MemoryUsedMB     int     `json:"memory_used_mb"`
	DiskUsedGB       int     `json:"disk_used_gb"`
	LoadAverage      float64 `json:"load_average"`
	RunningInstances int     `json:"running_instances"`
}

// RunHeartbeatLoop posts a heartbeat every cfg.HeartbeatInterval until ctx
// is cancelled — same shape as the goroutines section 12 calls out
// (context-aware, graceful shutdown via ctx cancellation).
func (r *Registrar) RunHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sendHeartbeat(ctx)
		}
	}
}

func (r *Registrar) sendHeartbeat(ctx context.Context) {
	m, err := collectMetrics(ctx)
	if err != nil {
		r.logger.Error("nebula-agent: failed to collect metrics", "error", err)
		return
	}

	count, err := r.hypervisor.CountVMs(ctx)
	if err != nil {
		r.logger.Error("nebula-agent: failed to count vms", "error", err)
		return
	}

	body, err := json.Marshal(heartbeatRequest{
		CPUUsage:         m.CPUUsage,
		MemoryUsedMB:     m.MemUsedMB,
		DiskUsedGB:       m.DiskUsedGB,
		LoadAverage:      m.LoadAverage,
		RunningInstances: count,
	})
	if err != nil {
		r.logger.Error("nebula-agent: failed to marshal heartbeat", "error", err)
		return
	}

	path := "/api/v1/nodes/" + r.nodeID + "/heartbeat"
	if _, err := r.post(ctx, path, r.nodeToken, body, http.StatusOK, nil); err != nil {
		r.logger.Error("nebula-agent: heartbeat failed", "error", err)
	}
}

// post returns the response status code alongside any error (0 if the
// request never got a response at all) so callers can distinguish a
// permanent rejection (e.g. 409) from a transient failure worth retrying.
func (r *Registrar) post(ctx context.Context, path, bearerToken string, body []byte, wantStatus int, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.APIURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		return resp.StatusCode, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, path)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response from %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}
