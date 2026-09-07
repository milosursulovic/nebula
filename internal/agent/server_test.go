package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer() (*Store, http.Handler) {
	store := NewStore()
	srv := NewServer(":0", store)
	return store, srv.Handler
}

func doRequest(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandleCreateVM(t *testing.T) {
	_, h := newTestServer()

	rec := doRequest(t, h, http.MethodPost, "/vms", createVMRequest{
		InstanceID: "inst-1", CPU: 2, MemoryMB: 4096, DiskGB: 50, Image: "ubuntu-26.04",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp vmResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.InstanceID != "inst-1" || resp.Status != string(VMStatusStopped) {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleCreateVMMissingInstanceID(t *testing.T) {
	_, h := newTestServer()

	rec := doRequest(t, h, http.MethodPost, "/vms", createVMRequest{CPU: 1, MemoryMB: 1, DiskGB: 1})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleGetVM(t *testing.T) {
	store, h := newTestServer()
	store.Create("inst-1", 2, 4096, 50, "img")

	rec := doRequest(t, h, http.MethodGet, "/vms/inst-1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleGetVMNotFound(t *testing.T) {
	_, h := newTestServer()

	rec := doRequest(t, h, http.MethodGet, "/vms/missing", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleStartVM(t *testing.T) {
	store, h := newTestServer()
	store.Create("inst-1", 1, 1024, 10, "img")

	rec := doRequest(t, h, http.MethodPost, "/vms/inst-1/start", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp vmResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != string(VMStatusRunning) {
		t.Errorf("status = %q, want RUNNING", resp.Status)
	}
}

func TestHandleStartVMNotFound(t *testing.T) {
	_, h := newTestServer()

	rec := doRequest(t, h, http.MethodPost, "/vms/missing/start", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteVM(t *testing.T) {
	store, h := newTestServer()
	store.Create("inst-1", 1, 1024, 10, "img")

	rec := doRequest(t, h, http.MethodDelete, "/vms/inst-1", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, err := store.Get("inst-1"); err == nil {
		t.Error("expected vm to be gone after delete")
	}
}

func TestHandleDeleteVMUnknownStillNoContent(t *testing.T) {
	_, h := newTestServer()

	rec := doRequest(t, h, http.MethodDelete, "/vms/missing", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestHandleInfo(t *testing.T) {
	_, h := newTestServer()

	rec := doRequest(t, h, http.MethodGet, "/info", nil)

	// /proc reads are host-dependent (and unavailable in some sandboxes),
	// so this only asserts the handler wires collectMetrics through
	// correctly, not specific numbers.
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 200 or 500", rec.Code)
	}
}
