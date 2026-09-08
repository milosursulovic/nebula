package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/node"
)

func newInstanceTestServer(instanceSvc instance.Service) (*http.Server, auth.TokenIssuer) {
	tokens := testTokenIssuer()
	return NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, testLogger(), testNodeBootstrapSecret), tokens
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

func TestHandleStopInstanceSuccess(t *testing.T) {
	nodeID := "node-1"
	transitions := []instance.Status{}
	instanceSvc := fakeInstanceService{
		transitionFn: func(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
			transitions = append(transitions, to)
			return instance.Instance{ID: id, Status: to, NodeID: &nodeID}, nil
		},
	}
	vmStopped := false
	stopVM := func(ctx context.Context, instanceID, nid string) error {
		vmStopped = true
		if instanceID != "inst-1" || nid != nodeID {
			t.Fatalf("unexpected stopVM args: instanceID=%q nodeID=%q", instanceID, nid)
		}
		return nil
	}

	tokens := testTokenIssuer()
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, stopVM, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/stop", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !vmStopped {
		t.Error("expected stopVM to be called")
	}
	if len(transitions) != 2 || transitions[0] != instance.StatusStopping || transitions[1] != instance.StatusStopped {
		t.Errorf("unexpected transition sequence: %v", transitions)
	}
}

func TestHandleStopInstanceInvalidTransition(t *testing.T) {
	svc := fakeInstanceService{
		transitionFn: func(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
			return instance.Instance{}, instance.ErrInvalidTransition
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/stop", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleStopInstanceAgentErrorMarksError(t *testing.T) {
	nodeID := "node-1"
	transitions := []instance.Status{}
	instanceSvc := fakeInstanceService{
		transitionFn: func(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
			transitions = append(transitions, to)
			return instance.Instance{ID: id, Status: to, NodeID: &nodeID}, nil
		},
	}
	stopVM := func(ctx context.Context, instanceID, nid string) error {
		return errors.New("agent unreachable")
	}

	tokens := testTokenIssuer()
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, stopVM, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/stop", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if len(transitions) != 2 || transitions[0] != instance.StatusStopping || transitions[1] != instance.StatusError {
		t.Errorf("unexpected transition sequence: %v", transitions)
	}
}

func TestHandleStartInstanceSuccess(t *testing.T) {
	nodeID := "node-1"
	instanceSvc := fakeInstanceService{
		getFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, Status: instance.StatusStopped, NodeID: &nodeID}, nil
		},
		transitionFn: func(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
			if to != instance.StatusRunning {
				t.Fatalf("unexpected transition target: %q", to)
			}
			return instance.Instance{ID: id, Status: to, NodeID: &nodeID}, nil
		},
	}
	vmStarted := false
	startVM := func(ctx context.Context, instanceID, nid string) error {
		vmStarted = true
		if instanceID != "inst-1" || nid != nodeID {
			t.Fatalf("unexpected startVM args: instanceID=%q nodeID=%q", instanceID, nid)
		}
		return nil
	}

	tokens := testTokenIssuer()
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, startVM, noopStopVM, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/start", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !vmStarted {
		t.Error("expected startVM to be called")
	}

	var resp instanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != string(instance.StatusRunning) {
		t.Errorf("Status = %q, want %q", resp.Status, instance.StatusRunning)
	}
}

func TestHandleStartInstanceRequiresStopped(t *testing.T) {
	svc := fakeInstanceService{
		getFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, Status: instance.StatusRunning}, nil
		},
	}
	srv, tokens := newInstanceTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/start", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleStartInstanceAgentErrorLeavesStopped(t *testing.T) {
	nodeID := "node-1"
	transitionCalled := false
	instanceSvc := fakeInstanceService{
		getFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, Status: instance.StatusStopped, NodeID: &nodeID}, nil
		},
		transitionFn: func(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
			transitionCalled = true
			return instance.Instance{}, nil
		},
	}
	startVM := func(ctx context.Context, instanceID, nid string) error {
		return errors.New("agent unreachable")
	}

	tokens := testTokenIssuer()
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, startVM, noopStopVM, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/start", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if transitionCalled {
		t.Error("Transition should not be called when the agent call fails — instance must stay STOPPED")
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

func TestHandleDeleteInstanceReleasesNodeCapacity(t *testing.T) {
	nodeID := "node-1"
	ip := "10.20.0.5"
	instanceSvc := fakeInstanceService{
		deleteFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, Status: instance.StatusDeleted, CPU: 2, MemoryMB: 4096, DiskGB: 50, NodeID: &nodeID, IPAddress: &ip}, nil
		},
	}
	released := false
	nodeSvc := fakeNodeService{
		releaseFn: func(ctx context.Context, id string, cpu, memoryMB, diskGB int) (node.Node, error) {
			released = true
			if id != nodeID || cpu != 2 || memoryMB != 4096 || diskGB != 50 {
				t.Fatalf("unexpected release args: id=%q cpu=%d memoryMB=%d diskGB=%d", id, cpu, memoryMB, diskGB)
			}
			return node.Node{ID: nodeID}, nil
		},
	}
	vmDeleted := false
	deleteVM := func(ctx context.Context, instanceID, nid string) error {
		vmDeleted = true
		if instanceID != "inst-1" || nid != nodeID {
			t.Fatalf("unexpected deleteVM args: instanceID=%q nodeID=%q", instanceID, nid)
		}
		return nil
	}

	ipReleased := false
	releaseIP := func(ctx context.Context, instanceID string) error {
		ipReleased = true
		if instanceID != "inst-1" {
			t.Fatalf("unexpected releaseIP instanceID: %q", instanceID)
		}
		return nil
	}

	tokens := testTokenIssuer()
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, nodeSvc, instanceSvc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, deleteVM, releaseIP, noopStartVM, noopStopVM, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/instances/inst-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !released {
		t.Error("expected nodeSvc.Release to be called when deleted instance had a NodeID")
	}
	if !vmDeleted {
		t.Error("expected deleteVM to be called when deleted instance had a NodeID")
	}
	if !ipReleased {
		t.Error("expected releaseIP to be called when deleted instance had an IPAddress")
	}
}
