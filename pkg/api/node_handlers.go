package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/milosursulovic/nebula/internal/node"
)

type registerNodeRequest struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	CPU      int    `json:"cpu"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
}

type registerNodeResponse struct {
	NodeID    string `json:"node_id"`
	NodeToken string `json:"node_token"`
}

func handleRegisterNode(svc node.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerNodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}

		req.Hostname = strings.TrimSpace(req.Hostname)
		req.IP = strings.TrimSpace(req.IP)

		if req.Hostname == "" || req.IP == "" || req.CPU <= 0 || req.MemoryMB <= 0 || req.DiskGB <= 0 {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "hostname, ip, cpu, memory_mb, disk_gb are required and must be positive")
			return
		}

		result, err := svc.Register(r.Context(), node.RegisterInput{
			Hostname: req.Hostname,
			IP:       req.IP,
			CPU:      req.CPU,
			MemoryMB: req.MemoryMB,
			DiskGB:   req.DiskGB,
		})
		if err != nil {
			if errors.Is(err, node.ErrHostnameTaken) {
				writeError(w, r, http.StatusConflict, "HOSTNAME_TAKEN", "a node with this hostname is already registered")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to register node")
			return
		}

		writeJSON(w, http.StatusCreated, registerNodeResponse{
			NodeID:    result.Node.ID,
			NodeToken: result.NodeToken,
		})
	}
}

type nodeResponse struct {
	ID                string  `json:"id"`
	Hostname          string  `json:"hostname"`
	IP                string  `json:"ip"`
	Status            string  `json:"status"`
	TotalCPU          int     `json:"total_cpu"`
	AvailableCPU      int     `json:"available_cpu"`
	TotalMemoryMB     int     `json:"total_memory_mb"`
	AvailableMemoryMB int     `json:"available_memory_mb"`
	TotalDiskGB       int     `json:"total_disk_gb"`
	AvailableDiskGB   int     `json:"available_disk_gb"`
	LoadAverage       float64 `json:"load_average"`
	RunningInstances  int     `json:"running_instances"`
	LastHeartbeatAt   *string `json:"last_heartbeat_at"`
}

func newNodeResponse(n node.Node) nodeResponse {
	resp := nodeResponse{
		ID:                n.ID,
		Hostname:          n.Hostname,
		IP:                n.IP,
		Status:            string(n.Status),
		TotalCPU:          n.TotalCPU,
		AvailableCPU:      n.AvailableCPU,
		TotalMemoryMB:     n.TotalMemoryMB,
		AvailableMemoryMB: n.AvailableMemoryMB,
		TotalDiskGB:       n.TotalDiskGB,
		AvailableDiskGB:   n.AvailableDiskGB,
		LoadAverage:       n.LoadAverage,
		RunningInstances:  n.RunningInstances,
	}
	if n.LastHeartbeatAt != nil {
		formatted := n.LastHeartbeatAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.LastHeartbeatAt = &formatted
	}
	return resp
}

func handleListNodes(svc node.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, err := svc.List(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list nodes")
			return
		}

		resp := make([]nodeResponse, 0, len(nodes))
		for _, n := range nodes {
			resp = append(resp, newNodeResponse(n))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetNode(svc node.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		n, err := svc.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, node.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "NODE_NOT_FOUND", "no node with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get node")
			return
		}

		writeJSON(w, http.StatusOK, newNodeResponse(n))
	}
}

type heartbeatRequest struct {
	CPUUsage         float64 `json:"cpu_usage"`
	MemoryUsedMB     int     `json:"memory_used_mb"`
	DiskUsedGB       int     `json:"disk_used_gb"`
	LoadAverage      float64 `json:"load_average"`
	RunningInstances int     `json:"running_instances"`
}

// handleHeartbeat authenticates with the node's own bearer token (issued at
// registration), not a user JWT — the agent runs unattended and a 15-minute
// access token would not survive between heartbeats.
func handleHeartbeat(svc node.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			writeError(w, r, http.StatusUnauthorized, "MISSING_NODE_TOKEN", "missing bearer node token")
			return
		}
		if err := svc.AuthenticateNodeToken(r.Context(), id, token); err != nil {
			writeError(w, r, http.StatusUnauthorized, "INVALID_NODE_TOKEN", "invalid node token")
			return
		}

		var req heartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}

		err := svc.Heartbeat(r.Context(), id, node.HeartbeatInput{
			CPUUsage:         req.CPUUsage,
			MemoryUsedMB:     req.MemoryUsedMB,
			DiskUsedGB:       req.DiskUsedGB,
			LoadAverage:      req.LoadAverage,
			RunningInstances: req.RunningInstances,
		})
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to record heartbeat")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
