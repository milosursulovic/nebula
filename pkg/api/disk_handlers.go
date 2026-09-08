package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/storage"
)

type createDiskRequest struct {
	Type   string `json:"type"`
	SizeGB int    `json:"size_gb"`
}

type diskResponse struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	SizeGB     int     `json:"size_gb"`
	InstanceID *string `json:"instance_id"`
	NodeID     string  `json:"node_id"`
}

func newDiskResponse(d storage.Disk) diskResponse {
	return diskResponse{
		ID:         d.ID,
		Type:       string(d.Type),
		SizeGB:     d.SizeGB,
		InstanceID: d.InstanceID,
		NodeID:     d.NodeID,
	}
}

// handleCreateDisk is POST /instances/{id}/disks — a standalone DATA/
// BACKUP disk attached to an already-scheduled instance. ROOT disks are
// saga-automatic only (internal/provisioning/disk_steps.go), not
// creatable through this endpoint.
func handleCreateDisk(instanceSvc instance.Service, storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		instanceID := chi.URLParam(r, "id")
		inst, err := instanceSvc.Get(r.Context(), identity.TenantID, instanceID)
		if err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get instance")
			return
		}
		if inst.NodeID == nil {
			writeError(w, r, http.StatusConflict, "INSTANCE_NOT_SCHEDULED", "instance has no node yet; cannot place a disk")
			return
		}

		var req createDiskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}
		req.Type = strings.ToUpper(strings.TrimSpace(req.Type))

		var diskType storage.Type
		switch req.Type {
		case string(storage.TypeData):
			diskType = storage.TypeData
		case string(storage.TypeBackup):
			diskType = storage.TypeBackup
		case string(storage.TypeRoot):
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "root disks are created automatically and cannot be requested directly")
			return
		default:
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "type must be DATA or BACKUP")
			return
		}
		if req.SizeGB <= 0 {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "size_gb must be positive")
			return
		}

		d, err := storageSvc.CreateDisk(r.Context(), identity.TenantID, *inst.NodeID, &instanceID, diskType, req.SizeGB)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create disk")
			return
		}

		writeJSON(w, http.StatusCreated, newDiskResponse(d))
	}
}

func handleListInstanceDisks(instanceSvc instance.Service, storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		instanceID := chi.URLParam(r, "id")
		if _, err := instanceSvc.Get(r.Context(), identity.TenantID, instanceID); err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get instance")
			return
		}

		disks, err := storageSvc.ListByInstance(r.Context(), instanceID)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list disks")
			return
		}

		resp := make([]diskResponse, 0, len(disks))
		for _, d := range disks {
			resp = append(resp, newDiskResponse(d))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// getOwnedDisk fetches a disk and verifies it belongs to the caller's
// tenant — cross-tenant access returns 404 (writing the error itself),
// matching every other resource's "no existence leak" convention. The
// second return is false if a response has already been written.
func getOwnedDisk(w http.ResponseWriter, r *http.Request, storageSvc storage.Service, tenantID, diskID string) (storage.Disk, bool) {
	d, err := storageSvc.Get(r.Context(), diskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "DISK_NOT_FOUND", "no disk with this id")
			return storage.Disk{}, false
		}
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get disk")
		return storage.Disk{}, false
	}
	if d.TenantID != tenantID {
		writeError(w, r, http.StatusNotFound, "DISK_NOT_FOUND", "no disk with this id")
		return storage.Disk{}, false
	}
	return d, true
}

func handleGetDisk(storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		d, ok := getOwnedDisk(w, r, storageSvc, identity.TenantID, chi.URLParam(r, "id"))
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, newDiskResponse(d))
	}
}

type attachDiskRequest struct {
	InstanceID string `json:"instance_id"`
}

func handleAttachDisk(instanceSvc instance.Service, storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		diskID := chi.URLParam(r, "id")
		if _, ok := getOwnedDisk(w, r, storageSvc, identity.TenantID, diskID); !ok {
			return
		}

		var req attachDiskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}
		req.InstanceID = strings.TrimSpace(req.InstanceID)
		if req.InstanceID == "" {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "instance_id is required")
			return
		}

		inst, err := instanceSvc.Get(r.Context(), identity.TenantID, req.InstanceID)
		if err != nil {
			if errors.Is(err, instance.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "INSTANCE_NOT_FOUND", "no instance with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get instance")
			return
		}
		if inst.NodeID == nil {
			writeError(w, r, http.StatusConflict, "INSTANCE_NOT_SCHEDULED", "instance has no node yet; cannot attach a disk")
			return
		}

		d, err := storageSvc.AttachDisk(r.Context(), diskID, *inst.NodeID, req.InstanceID)
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrAlreadyAttached):
				writeError(w, r, http.StatusConflict, "DISK_ALREADY_ATTACHED", "disk is already attached")
			case errors.Is(err, storage.ErrWrongNode):
				writeError(w, r, http.StatusConflict, "DISK_WRONG_NODE", "disk and instance are on different nodes")
			default:
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to attach disk")
			}
			return
		}

		writeJSON(w, http.StatusOK, newDiskResponse(d))
	}
}

func handleDetachDisk(storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		diskID := chi.URLParam(r, "id")
		if _, ok := getOwnedDisk(w, r, storageSvc, identity.TenantID, diskID); !ok {
			return
		}

		d, err := storageSvc.DetachDisk(r.Context(), diskID)
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrRootDiskNotDetachable):
				writeError(w, r, http.StatusConflict, "ROOT_DISK_NOT_DETACHABLE", "root disks cannot be detached")
			case errors.Is(err, storage.ErrNotAttached):
				writeError(w, r, http.StatusConflict, "DISK_NOT_ATTACHED", "disk is not attached")
			default:
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to detach disk")
			}
			return
		}

		writeJSON(w, http.StatusOK, newDiskResponse(d))
	}
}

type resizeDiskRequest struct {
	SizeGB int `json:"size_gb"`
}

func handleResizeDisk(storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		diskID := chi.URLParam(r, "id")
		if _, ok := getOwnedDisk(w, r, storageSvc, identity.TenantID, diskID); !ok {
			return
		}

		var req resizeDiskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}
		if req.SizeGB <= 0 {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "size_gb must be positive")
			return
		}

		d, err := storageSvc.ResizeDisk(r.Context(), diskID, req.SizeGB)
		if err != nil {
			if errors.Is(err, storage.ErrShrinkNotAllowed) {
				writeError(w, r, http.StatusBadRequest, "SHRINK_NOT_ALLOWED", "disks can only grow, not shrink")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resize disk")
			return
		}

		writeJSON(w, http.StatusOK, newDiskResponse(d))
	}
}

func handleDeleteDisk(storageSvc storage.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
			return
		}

		diskID := chi.URLParam(r, "id")
		d, ok := getOwnedDisk(w, r, storageSvc, identity.TenantID, diskID)
		if !ok {
			return
		}

		if err := storageSvc.DeleteDisk(r.Context(), diskID); err != nil {
			if errors.Is(err, storage.ErrStillAttached) {
				writeError(w, r, http.StatusConflict, "DISK_STILL_ATTACHED", "disk must be detached before it can be deleted")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete disk")
			return
		}

		writeJSON(w, http.StatusOK, newDiskResponse(d))
	}
}
