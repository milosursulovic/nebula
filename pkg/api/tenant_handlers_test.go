package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/milosursulovic/nebula/internal/auth"
)

func TestHandleListTenantsSuperAdmin(t *testing.T) {
	svc := fakeAuthService{
		listTenantsFn: func(ctx context.Context) ([]auth.Tenant, error) {
			return []auth.Tenant{
				{ID: "tenant-1", Name: "ana-co", CreatedAt: time.Now()},
				{ID: "tenant-2", Name: "bob-co", CreatedAt: time.Now()},
			}, nil
		},
	}
	tokens := testTokenIssuer()
	srv := newAuthTestServer(svc, tokens)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/tenants", nil, superAdminAuthHeader(t, tokens))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var got []tenantResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(tenants) = %d, want 2", len(got))
	}
}

func TestHandleListTenantsRequiresSuperAdmin(t *testing.T) {
	svc := fakeAuthService{
		listTenantsFn: func(ctx context.Context) ([]auth.Tenant, error) {
			t.Fatal("ListTenants should not be called for a non-SUPER_ADMIN caller")
			return nil, nil
		},
	}
	tokens := testTokenIssuer()
	srv := newAuthTestServer(svc, tokens)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/tenants", nil, tenantAdminAuthHeader(t, tokens))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
}
