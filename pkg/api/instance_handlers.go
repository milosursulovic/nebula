package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/provisioning"
	"github.com/milosursulovic/nebula/internal/storage"
)

type createInstanceRequest struct {
	Name     string `json:"name"`
	CPU      int    `json:"cpu"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	Image    string `json:"image"`
}

type instanceResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPU       int     `json:"cpu"`
	MemoryMB  int     `json:"memory_mb"`
	DiskGB    int     `json:"disk_gb"`
	Image     string  `json:"image"`
	NodeID    *string `json:"node_id"`
	IPAddress *string `json:"ip_address"`
}

func newInstanceResponse(i instance.Instance) instanceResponse {
	return instanceResponse{
		ID:        i.ID,
		Name:      i.Name,
		Status:    string(i.Status),
		CPU:       i.CPU,
		MemoryMB:  i.MemoryMB,
		DiskGB:    i.DiskGB,
		Image:     i.Image,
		NodeID:    i.NodeID,
		IPAddress: i.IPAddress,
	}
}

func handleCreateInstance(svc instance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		var req createInstanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Image = strings.TrimSpace(req.Image)

		if req.Name == "" || req.Image == "" || req.CPU <= 0 || req.MemoryMB <= 0 || req.DiskGB <= 0 {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "name, image, cpu, memory_mb, disk_gb are required and must be positive")
			return
		}

		created, err := svc.Create(r.Context(), identity.TenantID, instance.CreateInput{
			Name: req.Name, CPU: req.CPU, MemoryMB: req.MemoryMB, DiskGB: req.DiskGB, Image: req.Image,
		})
		if err != nil {
			if errors.Is(err, instance.ErrNameTaken) {
				writeError(w, r, http.StatusConflict, "NAME_TAKEN", "an instance with this name already exists")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create instance")
			return
		}

		// Provisioning is asynchronous (spec section 14): the instance is
		// already created and returned; a worker picks up the CREATE_INSTANCE
		// job that Create enqueued atomically alongside it (spec section 23 —
		// no separate, crash-vulnerable enqueue step).
		writeJSON(w, http.StatusCreated, newInstanceResponse(created))
	}
}

func handleListInstances(svc instance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		instances, err := svc.List(r.Context(), identity.TenantID)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list instances")
			return
		}

		resp := make([]instanceResponse, 0, len(instances))
		for _, i := range instances {
			resp = append(resp, newInstanceResponse(i))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetInstance(svc instance.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		id := chi.URLParam(r, "id")

		i, err := svc.Get(r.Context(), identity.TenantID, id)
		if err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get instance")
			return
		}

		writeJSON(w, http.StatusOK, newInstanceResponse(i))
	}
}

// handleStopInstance drives RUNNING -> STOPPING -> STOPPED (spec section
// 49's "nebula instance stop"). Synchronous, same precedent as
// handleDeleteInstance: transition DB state, then call the agent directly
// in the same request — no separate job type (job.TypeStopInstance stays
// an unused schema placeholder, same as the network/disk job types Phases
// 12/13 also left unused once wired directly).
func handleStopInstance(svc instance.Service, stopVM provisioning.VMStopper, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		id := chi.URLParam(r, "id")

		stopping, err := svc.Transition(r.Context(), identity.TenantID, id, instance.StatusStopping)
		if err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			if errors.Is(err, instance.ErrInvalidTransition) {
				writeError(w, r, http.StatusConflict, "INVALID_TRANSITION", "instance must be RUNNING to stop")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to stop instance")
			return
		}

		if stopping.NodeID == nil {
			logger.Error("instance in STOPPING has no node_id", "instance_id", id)
			if _, err := svc.Transition(r.Context(), identity.TenantID, id, instance.StatusError); err != nil {
				logger.Error("failed to transition instance to ERROR", "instance_id", id, "error", err)
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "instance has no assigned node")
			return
		}

		if err := stopVM(r.Context(), id, *stopping.NodeID); err != nil {
			logger.Error("failed to stop vm on agent", "instance_id", id, "node_id", *stopping.NodeID, "error", err)
			if _, terr := svc.Transition(r.Context(), identity.TenantID, id, instance.StatusError); terr != nil {
				logger.Error("failed to transition instance to ERROR", "instance_id", id, "error", terr)
			}
			writeError(w, r, http.StatusBadGateway, "AGENT_ERROR", "failed to stop vm on agent")
			return
		}

		stopped, err := svc.Transition(r.Context(), identity.TenantID, id, instance.StatusStopped)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark instance stopped")
			return
		}

		writeJSON(w, http.StatusOK, newInstanceResponse(stopped))
	}
}

// handleStartInstance drives STOPPED -> RUNNING (spec section 49's "nebula
// instance start"). The state machine has no STOPPED->ERROR transition, so
// unlike stop this calls the agent BEFORE transitioning — a failure here
// leaves the instance untouched (still STOPPED) rather than stuck
// mid-transition with nowhere legal to go.
func handleStartInstance(svc instance.Service, startVM provisioning.VMStarter, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		id := chi.URLParam(r, "id")

		current, err := svc.Get(r.Context(), identity.TenantID, id)
		if err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get instance")
			return
		}

		if current.Status != instance.StatusStopped {
			writeError(w, r, http.StatusConflict, "INVALID_TRANSITION", "instance must be STOPPED to start")
			return
		}
		if current.NodeID == nil {
			logger.Error("instance in STOPPED has no node_id", "instance_id", id)
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "instance has no assigned node")
			return
		}

		if err := startVM(r.Context(), id, *current.NodeID); err != nil {
			logger.Error("failed to start vm on agent", "instance_id", id, "node_id", *current.NodeID, "error", err)
			writeError(w, r, http.StatusBadGateway, "AGENT_ERROR", "failed to start vm on agent")
			return
		}

		running, err := svc.Transition(r.Context(), identity.TenantID, id, instance.StatusRunning)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark instance running")
			return
		}

		writeJSON(w, http.StatusOK, newInstanceResponse(running))
	}
}

func handleDeleteInstance(svc instance.Service, nodeSvc node.Service, storageSvc storage.Service, deleteVM provisioning.VMDeleter, releaseIP provisioning.NetworkDeleter, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		id := chi.URLParam(r, "id")

		deleted, err := svc.Delete(r.Context(), identity.TenantID, id)
		if err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			if errors.Is(err, instance.ErrInvalidTransition) {
				writeError(w, r, http.StatusConflict, "INVALID_TRANSITION", "instance cannot be deleted from its current state")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete instance")
			return
		}

		// The provisioning saga (Phase 8) may have reserved real node
		// capacity for this instance, and (Phase 9) actually created a VM
		// on the node's agent — tear both down now that it's deleted.
		// Best-effort: the instance is already gone either way; a failure
		// here leaks reserved capacity/a mock VM rather than blocking the
		// delete (same shape as the create/job-enqueue gap Phase 6 left
		// and Phase 7 later closed properly via the outbox).
		if deleted.NodeID != nil {
			if err := deleteVM(r.Context(), deleted.ID, *deleted.NodeID); err != nil {
				logger.Error("failed to delete vm on agent after instance delete",
					"instance_id", deleted.ID, "node_id", *deleted.NodeID, "error", err)
			}
			if _, err := nodeSvc.Release(r.Context(), *deleted.NodeID, deleted.CPU, deleted.MemoryMB, deleted.DiskGB); err != nil {
				logger.Error("failed to release node capacity after instance delete",
					"instance_id", deleted.ID, "node_id", *deleted.NodeID, "error", err)
			}
		}

		// The saga's network step (Phase 12) may have allocated a real IP
		// independently of node/VM state (it runs after createDisk, before
		// createVM) — release it on its own condition, best-effort, same
		// shape as the node/VM cleanup above.
		if deleted.IPAddress != nil {
			if err := releaseIP(r.Context(), deleted.ID); err != nil {
				logger.Error("failed to release ip after instance delete",
					"instance_id", deleted.ID, "ip_address", *deleted.IPAddress, "error", err)
			}
		}

		// The saga's disk step (Phase 13) may have created a ROOT disk
		// before either of the above ever ran — DeleteForInstance is a
		// no-op if there isn't one, so this is safe to call unconditionally.
		if err := storageSvc.DeleteForInstance(r.Context(), deleted.ID); err != nil {
			logger.Error("failed to delete root disk after instance delete",
				"instance_id", deleted.ID, "error", err)
		}

		writeJSON(w, http.StatusOK, newInstanceResponse(deleted))
	}
}
