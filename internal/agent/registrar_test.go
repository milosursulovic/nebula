package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testRegistrarLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRegisterRetriesThrough409 is the regression test for Phase 16's
// registrar fix: a 409 (hostname already registered) is no longer treated
// as an instant, permanent failure — the control plane may reclaim the
// hostname moments later once its stale row goes OFFLINE (internal/node's
// ReclaimOffline), so the registrar must keep retrying through a 409
// exactly like any other failure, not give up on the first one.
func TestRegisterRetriesThrough409(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(registerResponse{NodeID: "node-1", NodeToken: "raw-token"})
	}))
	defer srv.Close()

	cfg := Config{
		APIURL: srv.URL, Hostname: "compute-01", IP: "10.0.0.11",
		CPU: 4, MemoryMB: 8192, DiskGB: 100, BootstrapSecret: "secret",
	}
	r := NewRegistrar(cfg, NewMockHypervisor(NewStore()), testRegistrarLogger())

	if err := r.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (two 409s then success)", attempts)
	}
	if r.NodeID() != "node-1" {
		t.Errorf("NodeID() = %q, want node-1", r.NodeID())
	}
}

// TestRegisterStopsOnContextCancellation covers a genuine, permanent
// hostname collision (a different, still-live node): the registrar must
// still give up rather than retrying forever. A real 10-attempt/~150s
// exhaustion isn't worth a slow test — a short-deadline context exercises
// the same "persistent 409, no success" path (one real 409 handled, then
// bailing during the backoff wait) without the wait.
func TestRegisterStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	cfg := Config{
		APIURL: srv.URL, Hostname: "compute-01", IP: "10.0.0.11",
		CPU: 4, MemoryMB: 8192, DiskGB: 100, BootstrapSecret: "secret",
	}
	r := NewRegistrar(cfg, NewMockHypervisor(NewStore()), testRegistrarLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Register(ctx); err == nil {
		t.Fatal("expected an error")
	}
	if attempts == 0 {
		t.Error("expected at least one real registration attempt before giving up")
	}
}
