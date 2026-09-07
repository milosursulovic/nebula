package api

import (
	"context"
	"net/http"
)

// Pinger is satisfied by anything that can verify its own connectivity.
// *pgxpool.Pool implements this.
type Pinger interface {
	Ping(ctx context.Context) error
}

// handleHealth reports liveness: the process is up and serving requests.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleReady reports readiness: dependencies (currently: the database) are reachable.
func handleReady(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
