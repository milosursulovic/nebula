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
)

type createInstanceRequest struct {
	Name     string `json:"name"`
	CPU      int    `json:"cpu"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	Image    string `json:"image"`
}

type instanceResponse struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	CPU      int     `json:"cpu"`
	MemoryMB int     `json:"memory_mb"`
	DiskGB   int     `json:"disk_gb"`
	Image    string  `json:"image"`
	NodeID   *string `json:"node_id"`
}

func newInstanceResponse(i instance.Instance) instanceResponse {
	return instanceResponse{
		ID:       i.ID,
		Name:     i.Name,
		Status:   string(i.Status),
		CPU:      i.CPU,
		MemoryMB: i.MemoryMB,
		DiskGB:   i.DiskGB,
		Image:    i.Image,
		NodeID:   i.NodeID,
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

func handleDeleteInstance(svc instance.Service, nodeSvc node.Service, deleteVM provisioning.VMDeleter, logger *slog.Logger) http.HandlerFunc {
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

		writeJSON(w, http.StatusOK, newInstanceResponse(deleted))
	}
}
