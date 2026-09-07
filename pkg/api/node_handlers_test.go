package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/node"
)

func newNodeTestServer(nodeSvc node.Service) (*http.Server, auth.TokenIssuer) {
	tokens := testTokenIssuer()
	return NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, nodeSvc, testLogger()), tokens
}

func superAdminAuthHeader(t *testing.T, tokens auth.TokenIssuer) map[string]string {
	t.Helper()
	token, err := tokens.IssueAccessToken("admin-1", "tenant-1", auth.RoleSuperAdmin)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func tenantAdminAuthHeader(t *testing.T, tokens auth.TokenIssuer) map[string]string {
	t.Helper()
	token, err := tokens.IssueAccessToken("user-1", "tenant-1", auth.RoleTenantAdmin)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestHandleRegisterNodeSuccess(t *testing.T) {
	svc := fakeNodeService{
		registerFn: func(ctx context.Context, in node.RegisterInput) (node.RegisterResult, error) {
			if in.Hostname != "compute-01" {
				t.Fatalf("unexpected hostname: %q", in.Hostname)
			}
			return node.RegisterResult{
				Node:      node.Node{ID: "node-1", Hostname: in.Hostname, Status: node.StatusOnline},
				NodeToken: "raw-node-token",
			}, nil
		},
	}
	srv, tokens := newNodeTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/register", registerNodeRequest{
		Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000,
	}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp registerNodeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NodeID != "node-1" || resp.NodeToken != "raw-node-token" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleRegisterNodeForbiddenForNonSuperAdmin(t *testing.T) {
	srv, tokens := newNodeTestServer(fakeNodeService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/register", registerNodeRequest{
		Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000,
	}, tenantAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleRegisterNodeRequiresAuth(t *testing.T) {
	srv, _ := newNodeTestServer(fakeNodeService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/register", registerNodeRequest{
		Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000,
	}, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleRegisterNodeHostnameTaken(t *testing.T) {
	svc := fakeNodeService{
		registerFn: func(ctx context.Context, in node.RegisterInput) (node.RegisterResult, error) {
			return node.RegisterResult{}, node.ErrHostnameTaken
		},
	}
	srv, tokens := newNodeTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/register", registerNodeRequest{
		Hostname: "compute-01", IP: "10.0.0.11", CPU: 16, MemoryMB: 32768, DiskGB: 1000,
	}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleRegisterNodeValidation(t *testing.T) {
	srv, tokens := newNodeTestServer(fakeNodeService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/register", registerNodeRequest{
		Hostname: "", IP: "", CPU: 0, MemoryMB: 0, DiskGB: 0,
	}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleListNodes(t *testing.T) {
	svc := fakeNodeService{
		listFn: func(ctx context.Context) ([]node.Node, error) {
			return []node.Node{{ID: "node-1", Hostname: "compute-01", Status: node.StatusOnline}}, nil
		},
	}
	srv, tokens := newNodeTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/nodes/", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []nodeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 || resp[0].Hostname != "compute-01" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleGetNodeNotFound(t *testing.T) {
	svc := fakeNodeService{
		getFn: func(ctx context.Context, id string) (node.Node, error) {
			return node.Node{}, node.ErrNotFound
		},
	}
	srv, tokens := newNodeTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/nodes/nonexistent", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleHeartbeatSuccess(t *testing.T) {
	called := false
	svc := fakeNodeService{
		authNodeFn: func(ctx context.Context, id, rawToken string) error {
			if id != "node-1" || rawToken != "correct-node-token" {
				t.Fatalf("unexpected auth args: id=%q token=%q", id, rawToken)
			}
			return nil
		},
		heartbeatFn: func(ctx context.Context, id string, in node.HeartbeatInput) error {
			called = true
			return nil
		},
	}
	srv, _ := newNodeTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/node-1/heartbeat", node.HeartbeatInput{
		LoadAverage: 2.13, RunningInstances: 4,
	}, map[string]string{"Authorization": "Bearer correct-node-token"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !called {
		t.Error("expected Heartbeat to be called")
	}
}

func TestHandleHeartbeatMissingToken(t *testing.T) {
	srv, _ := newNodeTestServer(fakeNodeService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/node-1/heartbeat", node.HeartbeatInput{}, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleHeartbeatInvalidToken(t *testing.T) {
	svc := fakeNodeService{
		authNodeFn: func(ctx context.Context, id, rawToken string) error {
			return node.ErrInvalidNodeToken
		},
	}
	srv, _ := newNodeTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/nodes/node-1/heartbeat", node.HeartbeatInput{},
		map[string]string{"Authorization": "Bearer wrong-token"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
