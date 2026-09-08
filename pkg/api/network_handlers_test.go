package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/network"
)

func newNetworkTestServer(networkSvc network.Service) (*http.Server, auth.TokenIssuer) {
	tokens := testTokenIssuer()
	return NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, fakeInstanceService{}, fakeJobService{}, networkSvc, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, testLogger(), testNodeBootstrapSecret), tokens
}

func TestHandleCreateNetworkSuccess(t *testing.T) {
	svc := fakeNetworkService{
		createNetworkFn: func(ctx context.Context, name, cidr, gateway string) (network.Network, network.Subnet, error) {
			if name != "production" || cidr != "10.20.0.0/24" || gateway != "10.20.0.1" {
				t.Fatalf("unexpected args: name=%q cidr=%q gateway=%q", name, cidr, gateway)
			}
			return network.Network{ID: "network-1", Name: name},
				network.Subnet{ID: "subnet-1", NetworkID: "network-1", CIDR: cidr, Gateway: gateway}, nil
		},
	}
	srv, tokens := newNetworkTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/networks/", createNetworkRequest{
		Name: "production", CIDR: "10.20.0.0/24", Gateway: "10.20.0.1",
	}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp networkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "network-1" || len(resp.Subnets) != 1 || resp.Subnets[0].CIDR != "10.20.0.0/24" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleCreateNetworkRequiresSuperAdmin(t *testing.T) {
	srv, tokens := newNetworkTestServer(fakeNetworkService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/networks/", createNetworkRequest{
		Name: "production", CIDR: "10.20.0.0/24", Gateway: "10.20.0.1",
	}, tenantAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleCreateNetworkValidation(t *testing.T) {
	srv, tokens := newNetworkTestServer(fakeNetworkService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/networks/", createNetworkRequest{}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCreateNetworkNameTaken(t *testing.T) {
	svc := fakeNetworkService{
		createNetworkFn: func(ctx context.Context, name, cidr, gateway string) (network.Network, network.Subnet, error) {
			return network.Network{}, network.Subnet{}, network.ErrNameTaken
		},
	}
	srv, tokens := newNetworkTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/networks/", createNetworkRequest{
		Name: "production", CIDR: "10.20.0.0/24", Gateway: "10.20.0.1",
	}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleCreateNetworkInvalidCIDR(t *testing.T) {
	svc := fakeNetworkService{
		createNetworkFn: func(ctx context.Context, name, cidr, gateway string) (network.Network, network.Subnet, error) {
			return network.Network{}, network.Subnet{}, network.ErrInvalidCIDR
		},
	}
	srv, tokens := newNetworkTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/networks/", createNetworkRequest{
		Name: "production", CIDR: "bogus", Gateway: "10.20.0.1",
	}, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleListNetworks(t *testing.T) {
	svc := fakeNetworkService{
		listFn: func(ctx context.Context) ([]network.Network, error) {
			return []network.Network{{ID: "network-1", Name: "production"}}, nil
		},
	}
	srv, tokens := newNetworkTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/networks/", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []networkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 || resp[0].Name != "production" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleGetNetworkNotFound(t *testing.T) {
	svc := fakeNetworkService{
		getFn: func(ctx context.Context, id string) (network.Network, error) {
			return network.Network{}, network.ErrNotFound
		},
	}
	srv, tokens := newNetworkTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/networks/nonexistent", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleGetNetworkSuccess(t *testing.T) {
	svc := fakeNetworkService{
		getFn: func(ctx context.Context, id string) (network.Network, error) {
			return network.Network{ID: id, Name: "production"}, nil
		},
		subnetsByNetworkFn: func(ctx context.Context, networkID string) ([]network.Subnet, error) {
			return []network.Subnet{{ID: "subnet-1", NetworkID: networkID, CIDR: "10.20.0.0/24", Gateway: "10.20.0.1"}}, nil
		},
	}
	srv, tokens := newNetworkTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/networks/network-1", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp networkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "network-1" || len(resp.Subnets) != 1 {
		t.Errorf("unexpected response: %+v", resp)
	}
}
