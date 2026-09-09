package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/job"
)

func newJobTestServer(jobSvc job.Service) (*http.Server, auth.TokenIssuer) {
	tokens := testTokenIssuer()
	return NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, fakeInstanceService{}, jobSvc, fakeNetworkService{}, fakeStorageService{}, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, newFakeLimiter(), false, testLogger(), testNodeBootstrapSecret), tokens
}

func TestHandleListJobsRequiresSuperAdmin(t *testing.T) {
	srv, tokens := newJobTestServer(fakeJobService{})

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/jobs/", nil, tenantAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleListJobsFiltersByStatus(t *testing.T) {
	svc := fakeJobService{
		listFn: func(ctx context.Context, status *job.Status) ([]job.Job, error) {
			if status == nil || *status != job.StatusFailed {
				t.Fatalf("unexpected status filter: %+v", status)
			}
			return []job.Job{{ID: "job-1", Type: job.TypeCreateInstance, Status: job.StatusFailed}}, nil
		},
	}
	srv, tokens := newJobTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/jobs/?status=FAILED", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []jobResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 || resp[0].ID != "job-1" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleGetJobNotFound(t *testing.T) {
	svc := fakeJobService{
		getFn: func(ctx context.Context, id string) (job.Job, error) {
			return job.Job{}, job.ErrNotFound
		},
	}
	srv, tokens := newJobTestServer(svc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/jobs/nonexistent", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleRetryJobSuccess(t *testing.T) {
	svc := fakeJobService{
		retryFn: func(ctx context.Context, id string) (job.Job, error) {
			return job.Job{ID: id, Status: job.StatusQueued}, nil
		},
	}
	srv, tokens := newJobTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/jobs/job-1/retry", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp jobResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != string(job.StatusQueued) {
		t.Errorf("Status = %q, want %q", resp.Status, job.StatusQueued)
	}
}

func TestHandleRetryJobNotFailed(t *testing.T) {
	svc := fakeJobService{
		retryFn: func(ctx context.Context, id string) (job.Job, error) {
			return job.Job{}, job.ErrNotFailed
		},
	}
	srv, tokens := newJobTestServer(svc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/jobs/job-1/retry", nil, superAdminAuthHeader(t, tokens))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}
