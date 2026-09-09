package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/instance"
)

func TestRateLimitBlocksOverLimitRequests(t *testing.T) {
	limiter := newFakeLimiter()
	limiter.allowFn = func(key string, limit int) bool { return false } // every check fails

	tokens := testTokenIssuer()
	svc := fakeInstanceService{
		listFn: func(ctx context.Context, tenantID string) ([]instance.Instance, error) {
			t.Fatal("handler should not run — rate limit should reject the request first")
			return nil, nil
		},
	}
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, svc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, limiter, false, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/instances", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}

func TestRateLimitAllowsUnderLimitRequests(t *testing.T) {
	limiter := newFakeLimiter() // default: always allows

	tokens := testTokenIssuer()
	svc := fakeInstanceService{
		listFn: func(ctx context.Context, tenantID string) ([]instance.Instance, error) {
			return []instance.Instance{}, nil
		},
	}
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, svc, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, limiter, false, testLogger(), testNodeBootstrapSecret)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/instances", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRateLimitSkipsUnauthenticatedRequests(t *testing.T) {
	limiter := newFakeLimiter()
	limiter.allowFn = func(key string, limit int) bool { return false }

	tokens := testTokenIssuer()
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, fakeInstanceService{}, fakeJobService{}, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, limiter, false, testLogger(), testNodeBootstrapSecret)

	// /health has no identity — must never be rate-limited.
	rec := doJSON(t, srv, http.MethodGet, "/health", nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
