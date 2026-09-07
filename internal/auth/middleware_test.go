package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func protectedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			http.Error(w, "no identity in context", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-User-ID", identity.UserID)
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthenticateMissingHeader(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")
	handler := Authenticate(issuer)(protectedHandler())

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticateInvalidToken(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")
	handler := Authenticate(issuer)(protectedHandler())

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticateValidToken(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")
	token, err := issuer.IssueAccessToken("user-1", "tenant-1", RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	handler := Authenticate(issuer)(protectedHandler())

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-User-ID"); got != "user-1" {
		t.Errorf("X-User-ID = %q, want %q", got, "user-1")
	}
}

func TestRequireRoleAllowsMatchingRole(t *testing.T) {
	handler := RequireRole(RoleTenantAdmin, RoleSuperAdmin)(protectedHandler())

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(withIdentity(req.Context(), Identity{UserID: "user-1", Role: RoleTenantAdmin}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRoleRejectsOtherRole(t *testing.T) {
	handler := RequireRole(RoleTenantAdmin, RoleSuperAdmin)(protectedHandler())

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(withIdentity(req.Context(), Identity{UserID: "user-1", Role: RoleUser}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireRoleRejectsMissingIdentity(t *testing.T) {
	handler := RequireRole(RoleTenantAdmin)(protectedHandler())

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
