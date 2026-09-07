package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
)

func newInstanceTestServer(instanceSvc instance.Service) (*http.Server, auth.TokenIssuer) {
	tokens := testTokenIssuer()
	return NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, testLogger()), tokens
}

func userAuthHeader(t *testing.T, tokens auth.TokenIssuer, tenantID string) map[string]string {
	t.Helper()
	token, err := tokens.IssueAccessToken("user-1", tenantID, auth.RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestHandleCreateInstanceSuccess(t *testing.T) {
	svc := fakeInstanceService{
		createFn: func(ctx context.Context, tenantID string, in instance.CreateInput) (instance.Instance, error) {
			if tenantID != "tenant-1" || in.Name != "database-01" {
				t.Fatalf("unexpected args: tenantID=%q in=%+v", tenantID, in)
			}
			return instance.Instance{ID: "inst-1", Name: in.Name, Status: instance.StatusPending}, nil
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/", createInstanceRequest{
		Name: "database-01", CPU: 4, MemoryMB: 8192, DiskGB: 100, Image: "ubuntu-26.04",
	}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp instanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "inst-1" || resp.Status != string(instance.StatusPending) {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleCreateInstanceRequiresAuth(t *testing.T) {
	srv, _ := newInstanceTestServer(fakeInstanceService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/", createInstanceRequest{
		Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img",
	}, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleCreateInstanceValidation(t *testing.T) {
	srv, tokens := newInstanceTestServer(fakeInstanceService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/", createInstanceRequest{
		Name: "", CPU: 0, MemoryMB: 0, DiskGB: 0, Image: "",
	}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCreateInstanceNameTaken(t *testing.T) {
	svc := fakeInstanceService{
		createFn: func(ctx context.Context, tenantID string, in instance.CreateInput) (instance.Instance, error) {
			return instance.Instance{}, instance.ErrNameTaken
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/", createInstanceRequest{
		Name: "dup", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img",
	}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleListInstancesScopedToCallerTenant(t *testing.T) {
	svc := fakeInstanceService{
		listFn: func(ctx context.Context, tenantID string) ([]instance.Instance, error) {
			if tenantID != "tenant-1" {
				t.Fatalf("unexpected tenantID: %q", tenantID)
			}
			return []instance.Instance{{ID: "inst-1", Name: "a", Status: instance.StatusPending}}, nil
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/instances/", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []instanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 || resp[0].ID != "inst-1" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleGetInstanceNotFound(t *testing.T) {
	svc := fakeInstanceService{
		getFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{}, instance.ErrNotFound
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/instances/nonexistent", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleDeleteInstanceSuccess(t *testing.T) {
	svc := fakeInstanceService{
		deleteFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, Status: instance.StatusDeleted}, nil
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/instances/inst-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp instanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != string(instance.StatusDeleted) {
		t.Errorf("Status = %q, want %q", resp.Status, instance.StatusDeleted)
	}
}

func TestHandleDeleteInstanceInvalidTransition(t *testing.T) {
	svc := fakeInstanceService{
		deleteFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{}, instance.ErrInvalidTransition
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/instances/inst-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}
