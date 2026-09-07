package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
)

func newAuthTestServer(authSvc auth.Service, tokens auth.TokenIssuer) *http.Server {
	return NewServer(":0", fakePinger{}, authSvc, tokens, fakeNodeService{}, fakeInstanceService{}, fakeJobService{}, noopDeleteVM, testLogger(), testNodeBootstrapSecret)
}

func doJSON(t *testing.T, srv *http.Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
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

func TestHandleRegisterSuccess(t *testing.T) {
	svc := fakeAuthService{
		registerFn: func(ctx context.Context, email, password, tenantName string) (auth.TokenPair, error) {
			if email != "ana@example.com" || tenantName != "ana-co" {
				t.Fatalf("unexpected args: email=%q tenantName=%q", email, tenantName)
			}
			return auth.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900}, nil
		},
	}
	srv := newAuthTestServer(svc, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "ana@example.com", Password: "hunter2hunter2", TenantName: "ana-co",
	}, nil)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken != "access" || resp.RefreshToken != "refresh" {
		t.Errorf("unexpected token response: %+v", resp)
	}
}

func TestHandleRegisterEmailTaken(t *testing.T) {
	svc := fakeAuthService{
		registerFn: func(ctx context.Context, email, password, tenantName string) (auth.TokenPair, error) {
			return auth.TokenPair{}, auth.ErrEmailTaken
		},
	}
	srv := newAuthTestServer(svc, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "dup@example.com", Password: "hunter2hunter2", TenantName: "dup-co",
	}, nil)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleRegisterValidation(t *testing.T) {
	svc := fakeAuthService{}
	srv := newAuthTestServer(svc, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/auth/register", registerRequest{
		Email: "", Password: "short", TenantName: "",
	}, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleLoginInvalidCredentials(t *testing.T) {
	svc := fakeAuthService{
		loginFn: func(ctx context.Context, email, password string) (auth.TokenPair, error) {
			return auth.TokenPair{}, auth.ErrInvalidCredentials
		},
	}
	srv := newAuthTestServer(svc, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", loginRequest{
		Email: "bob@example.com", Password: "wrong",
	}, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}

	var errResp errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != "INVALID_CREDENTIALS" {
		t.Errorf("error code = %q, want INVALID_CREDENTIALS", errResp.Code)
	}
}

func TestHandleRefreshInvalidToken(t *testing.T) {
	svc := fakeAuthService{
		refreshFn: func(ctx context.Context, refreshToken string) (auth.TokenPair, error) {
			return auth.TokenPair{}, auth.ErrRefreshTokenInvalid
		},
	}
	srv := newAuthTestServer(svc, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/auth/refresh", refreshRequest{RefreshToken: "stale"}, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleLogoutSuccess(t *testing.T) {
	called := false
	svc := fakeAuthService{
		logoutFn: func(ctx context.Context, refreshToken string) error {
			called = true
			return nil
		},
	}
	srv := newAuthTestServer(svc, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/auth/logout", refreshRequest{RefreshToken: "abc"}, nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if !called {
		t.Error("expected Logout to be called")
	}
}

func TestHandleMeRequiresAuth(t *testing.T) {
	srv := newAuthTestServer(fakeAuthService{}, testTokenIssuer())

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/me", nil, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleMeWithValidToken(t *testing.T) {
	tokens := testTokenIssuer()
	srv := newAuthTestServer(fakeAuthService{}, tokens)

	accessToken, err := tokens.IssueAccessToken("user-1", "tenant-1", auth.RoleTenantAdmin)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/me", nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp meResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.UserID != "user-1" || resp.TenantID != "tenant-1" || resp.Role != string(auth.RoleTenantAdmin) {
		t.Errorf("unexpected /me response: %+v", resp)
	}
}
