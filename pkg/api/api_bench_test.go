package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
)

// benchDoJSON mirrors doJSON (auth_handlers_test.go) but for *testing.B —
// full HTTP round trip through the real router/middleware stack, fake
// service underneath, no DB (spec section 47's "API benchmark" measures
// this layer's own overhead, isolated from network/DB latency).
func benchDoJSON(b *testing.B, srv *http.Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	b.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			b.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	return rec
}

func benchAuthHeader(b *testing.B, tokens auth.TokenIssuer, tenantID string) map[string]string {
	b.Helper()
	token, err := tokens.IssueAccessToken("bench-user", tenantID, auth.RoleUser)
	if err != nil {
		b.Fatalf("IssueAccessToken: %v", err)
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func BenchmarkListInstances(b *testing.B) {
	svc := fakeInstanceService{
		listFn: func(ctx context.Context, tenantID string) ([]instance.Instance, error) {
			instances := make([]instance.Instance, 20)
			for i := range instances {
				instances[i] = instance.Instance{ID: "inst", TenantID: tenantID, Name: "bench-vm", Status: instance.StatusRunning, CPU: 2, MemoryMB: 4096, DiskGB: 50, Image: "ubuntu-26.04"}
			}
			return instances, nil
		},
	}
	srv, tokens := newInstanceTestServer(svc)
	headers := benchAuthHeader(b, tokens, "tenant-1")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := benchDoJSON(b, srv, http.MethodGet, "/api/v1/instances", nil, headers)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}

func BenchmarkCreateInstance(b *testing.B) {
	svc := fakeInstanceService{
		createFn: func(ctx context.Context, tenantID string, in instance.CreateInput) (instance.Instance, error) {
			return instance.Instance{ID: "inst-new", TenantID: tenantID, Name: in.Name, Status: instance.StatusPending, CPU: in.CPU, MemoryMB: in.MemoryMB, DiskGB: in.DiskGB, Image: in.Image}, nil
		},
	}
	srv, tokens := newInstanceTestServer(svc)
	headers := benchAuthHeader(b, tokens, "tenant-1")
	body := createInstanceRequest{Name: "bench-vm", CPU: 2, MemoryMB: 4096, DiskGB: 50, Image: "ubuntu-26.04"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := benchDoJSON(b, srv, http.MethodPost, "/api/v1/instances", body, headers)
		if rec.Code != http.StatusCreated {
			b.Fatalf("status = %d", rec.Code)
		}
	}
}
