package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientRefreshesOn401 proves the one piece of real logic in the CLI:
// a stale access token (15-minute TTL, internal/auth.AccessTokenTTL) gets
// silently refreshed once and the original request retried, rather than
// failing the command or requiring the user to `nebula login` again mid
// session.
func TestClientRefreshesOn401(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var refreshCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/refresh":
			refreshCalls++
			var req struct {
				RefreshToken string `json:"refresh_token"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.RefreshToken != "old-refresh" {
				t.Errorf("refresh called with unexpected token %q", req.RefreshToken)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"access_token": "new-access", "refresh_token": "new-refresh",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/instances":
			auth := r.Header.Get("Authorization")
			if auth == "Bearer old-access" {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{"code": "UNAUTHENTICATED", "message": "expired"})
				return
			}
			if auth != "Bearer new-access" {
				t.Fatalf("unexpected Authorization header: %q", auth)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]instanceResponse{{ID: "inst-1", Name: "db01"}})

		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newClient(credentials{APIURL: srv.URL, AccessToken: "old-access", RefreshToken: "old-refresh"})

	var instances []instanceResponse
	if err := c.do(http.MethodGet, "/api/v1/instances", nil, &instances); err != nil {
		t.Fatalf("do: %v", err)
	}

	if len(instances) != 1 || instances[0].ID != "inst-1" {
		t.Fatalf("unexpected instances: %+v", instances)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}

	// The refreshed pair must be persisted, so the next CLI invocation
	// (a fresh process) doesn't have to refresh again immediately.
	saved, err := loadCredentials()
	if err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if saved.AccessToken != "new-access" || saved.RefreshToken != "new-refresh" {
		t.Errorf("saved credentials = %+v, want new-access/new-refresh", saved)
	}
}

func TestClientSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"status": 409, "code": "NAME_TAKEN", "message": "an instance with this name already exists"})
	}))
	defer srv.Close()

	c := newClient(credentials{APIURL: srv.URL, AccessToken: "access"})

	err := c.do(http.MethodPost, "/api/v1/instances", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*apiError)
	if !ok {
		t.Fatalf("error type = %T, want *apiError", err)
	}
	if apiErr.Code != "NAME_TAKEN" {
		t.Errorf("Code = %q, want NAME_TAKEN", apiErr.Code)
	}
}
