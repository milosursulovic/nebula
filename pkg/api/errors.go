package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// errorResponse matches the API error shape from spec section 39.
type errorResponse struct {
	Timestamp string `json:"timestamp"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Status:    status,
		Code:      code,
		Message:   message,
		TraceID:   middleware.GetReqID(r.Context()),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
