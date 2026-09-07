package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/milosursulovic/nebula/internal/job"
)

type jobResponse struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	TenantID    *string `json:"tenant_id"`
	InstanceID  *string `json:"instance_id"`
	NodeID      *string `json:"node_id"`
	Attempts    int     `json:"attempts"`
	MaxAttempts int     `json:"max_attempts"`
	Error       *string `json:"error"`
}

func newJobResponse(j job.Job) jobResponse {
	return jobResponse{
		ID:          j.ID,
		Type:        string(j.Type),
		Status:      string(j.Status),
		TenantID:    j.TenantID,
		InstanceID:  j.InstanceID,
		NodeID:      j.NodeID,
		Attempts:    j.Attempts,
		MaxAttempts: j.MaxAttempts,
		Error:       j.Error,
	}
}

func handleListJobs(svc job.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var status *job.Status
		if raw := r.URL.Query().Get("status"); raw != "" {
			s := job.Status(raw)
			status = &s
		}

		jobs, err := svc.List(r.Context(), status)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list jobs")
			return
		}

		resp := make([]jobResponse, 0, len(jobs))
		for _, j := range jobs {
			resp = append(resp, newJobResponse(j))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetJob(svc job.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		j, err := svc.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, job.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "no job with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get job")
			return
		}

		writeJSON(w, http.StatusOK, newJobResponse(j))
	}
}

func handleRetryJob(svc job.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		retried, err := svc.Retry(r.Context(), id)
		if err != nil {
			if errors.Is(err, job.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "no job with this id")
				return
			}
			if errors.Is(err, job.ErrNotFailed) {
				writeError(w, r, http.StatusConflict, "JOB_NOT_FAILED", "only a FAILED job can be retried")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retry job")
			return
		}

		writeJSON(w, http.StatusOK, newJobResponse(retried))
	}
}
