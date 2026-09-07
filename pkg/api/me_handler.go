package api

import (
	"net/http"

	"github.com/milosursulovic/nebula/internal/auth"
)

type meResponse struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
}

// handleMe returns the authenticated caller's identity. It is the minimal
// concrete proof that the Authenticate middleware and JWT claims work
// end-to-end; later phases build role-gated endpoints on the same pattern.
func handleMe(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no authenticated identity")
		return
	}

	writeJSON(w, http.StatusOK, meResponse{
		UserID:   identity.UserID,
		TenantID: identity.TenantID,
		Role:     string(identity.Role),
	})
}
