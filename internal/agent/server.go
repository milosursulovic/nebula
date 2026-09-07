package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// NewServer builds nebula-agent's own HTTP server — the REST shape of
// section 28's eventual protobuf NebulaAgent service (GetNodeInfo,
// CreateVM, DeleteVM, StartVM; no auth/TLS yet, that's Phase 10).
func NewServer(addr string, store *Store) *http.Server {
	r := chi.NewRouter()

	r.Get("/info", handleInfo(store))
	r.Post("/vms", handleCreateVM(store))
	r.Get("/vms/{instanceID}", handleGetVM(store))
	r.Post("/vms/{instanceID}/start", handleStartVM(store))
	r.Delete("/vms/{instanceID}", handleDeleteVM(store))

	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

type infoResponse struct {
	CPUUsage         float64 `json:"cpu_usage"`
	MemoryUsedMB     int     `json:"memory_used_mb"`
	DiskUsedGB       int     `json:"disk_used_gb"`
	LoadAverage      float64 `json:"load_average"`
	RunningInstances int     `json:"running_instances"`
}

// handleInfo is GetNodeInfo's REST shape — the same metrics posted on
// heartbeats, readable directly for debugging/verification.
func handleInfo(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := collectMetrics(r.Context())
		if err != nil {
			writeAgentError(w, http.StatusInternalServerError, "failed to collect metrics")
			return
		}
		writeJSON(w, http.StatusOK, infoResponse{
			CPUUsage:         m.CPUUsage,
			MemoryUsedMB:     m.MemUsedMB,
			DiskUsedGB:       m.DiskUsedGB,
			LoadAverage:      m.LoadAverage,
			RunningInstances: store.Count(),
		})
	}
}

type createVMRequest struct {
	InstanceID string `json:"instance_id"`
	CPU        int    `json:"cpu"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	Image      string `json:"image"`
}

type vmResponse struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	CPU        int    `json:"cpu"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	Image      string `json:"image"`
}

func newVMResponse(vm VM) vmResponse {
	return vmResponse{
		InstanceID: vm.InstanceID,
		Status:     string(vm.Status),
		CPU:        vm.CPU,
		MemoryMB:   vm.MemoryMB,
		DiskGB:     vm.DiskGB,
		Image:      vm.Image,
	}
}

func handleCreateVM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createVMRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAgentError(w, http.StatusBadRequest, "request body must be valid JSON")
			return
		}
		if req.InstanceID == "" {
			writeAgentError(w, http.StatusBadRequest, "instance_id is required")
			return
		}

		vm := store.Create(req.InstanceID, req.CPU, req.MemoryMB, req.DiskGB, req.Image)
		writeJSON(w, http.StatusCreated, newVMResponse(vm))
	}
}

func handleGetVM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instanceID := chi.URLParam(r, "instanceID")

		vm, err := store.Get(instanceID)
		if err != nil {
			if errors.Is(err, ErrVMNotFound) {
				writeAgentError(w, http.StatusNotFound, "no vm for this instance id")
				return
			}
			writeAgentError(w, http.StatusInternalServerError, "failed to get vm")
			return
		}
		writeJSON(w, http.StatusOK, newVMResponse(vm))
	}
}

func handleStartVM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instanceID := chi.URLParam(r, "instanceID")

		vm, err := store.Start(instanceID)
		if err != nil {
			if errors.Is(err, ErrVMNotFound) {
				writeAgentError(w, http.StatusNotFound, "no vm for this instance id")
				return
			}
			writeAgentError(w, http.StatusInternalServerError, "failed to start vm")
			return
		}
		writeJSON(w, http.StatusOK, newVMResponse(vm))
	}
}

func handleDeleteVM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instanceID := chi.URLParam(r, "instanceID")
		store.Delete(instanceID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeAgentError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
