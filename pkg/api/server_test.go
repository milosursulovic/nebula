package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
)

type fakePinger struct {
	err error
}

func (f fakePinger) Ping(ctx context.Context) error {
	return f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testTokenIssuer() auth.TokenIssuer {
	return auth.NewTokenIssuer("test-secret")
}

func TestHandleHealth(t *testing.T) {
	srv := NewServer(":0", fakePinger{}, fakeAuthService{}, testTokenIssuer(), fakeNodeService{}, fakeInstanceService{}, fakeJobService{}, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestHandleReady(t *testing.T) {
	tests := []struct {
		name       string
		pinger     fakePinger
		wantStatus int
	}{
		{"database healthy", fakePinger{err: nil}, http.StatusOK},
		{"database unreachable", fakePinger{err: errors.New("connection refused")}, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(":0", tt.pinger, fakeAuthService{}, testTokenIssuer(), fakeNodeService{}, fakeInstanceService{}, fakeJobService{}, testLogger())

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}
