package api

import (
	"net/http"

	"github.com/milosursulovic/nebula/internal/auth"
)

type tenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func newTenantResponse(t auth.Tenant) tenantResponse {
	return tenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// handleListTenants is platform infrastructure, gated SUPER_ADMIN like
// nodes/jobs (spec section 8: "SUPER_ADMIN -> manage tenants") — every
// tenant, not scoped to the caller's own one.
func handleListTenants(svc auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenants, err := svc.ListTenants(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list tenants")
			return
		}

		resp := make([]tenantResponse, 0, len(tenants))
		for _, t := range tenants {
			resp = append(resp, newTenantResponse(t))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
