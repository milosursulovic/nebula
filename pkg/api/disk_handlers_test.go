package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/storage"
)

func newDiskTestServer(instanceSvc instance.Service, storageSvc storage.Service) (*http.Server, auth.TokenIssuer) {
	tokens := testTokenIssuer()
	return NewServer(":0", fakePinger{}, fakeAuthService{}, tokens, fakeNodeService{}, instanceSvc, fakeJobService{}, fakeNetworkService{}, storageSvc, noopDeleteVM, noopReleaseIP, noopStartVM, noopStopVM, newFakeLimiter(), false, testLogger(), testNodeBootstrapSecret), tokens
}

func scheduledInstance(nodeID string) fakeInstanceService {
	return fakeInstanceService{
		getFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, TenantID: tenantID, Status: instance.StatusRunning, NodeID: &nodeID}, nil
		},
	}
}

func TestHandleCreateDiskSuccess(t *testing.T) {
	instanceSvc := scheduledInstance("node-1")
	storageSvc := fakeStorageService{
		createDiskFn: func(ctx context.Context, tenantID, nodeID string, instanceID *string, diskType storage.Type, sizeGB int) (storage.Disk, error) {
			if tenantID != "tenant-1" || nodeID != "node-1" || diskType != storage.TypeData || sizeGB != 20 {
				t.Fatalf("unexpected args: tenantID=%q nodeID=%q type=%q sizeGB=%d", tenantID, nodeID, diskType, sizeGB)
			}
			return storage.Disk{ID: "disk-1", TenantID: tenantID, InstanceID: instanceID, NodeID: nodeID, Type: diskType, SizeGB: sizeGB}, nil
		},
	}
	srv, tokens := newDiskTestServer(instanceSvc, storageSvc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/disks", createDiskRequest{
		Type: "DATA", SizeGB: 20,
	}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp diskResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "disk-1" || resp.Type != "DATA" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleCreateDiskRejectsRootType(t *testing.T) {
	srv, tokens := newDiskTestServer(scheduledInstance("node-1"), fakeStorageService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/disks", createDiskRequest{
		Type: "ROOT", SizeGB: 20,
	}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleCreateDiskUnscheduledInstance(t *testing.T) {
	instanceSvc := fakeInstanceService{
		getFn: func(ctx context.Context, tenantID, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, TenantID: tenantID, Status: instance.StatusPending}, nil
		},
	}
	srv, tokens := newDiskTestServer(instanceSvc, fakeStorageService{})

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/instances/inst-1/disks", createDiskRequest{
		Type: "DATA", SizeGB: 20,
	}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleGetDiskCrossTenantIsNotFound(t *testing.T) {
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "other-tenant"}, nil
		},
	}
	srv, tokens := newDiskTestServer(fakeInstanceService{}, storageSvc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/disks/disk-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleGetDiskNotFound(t *testing.T) {
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{}, storage.ErrNotFound
		},
	}
	srv, tokens := newDiskTestServer(fakeInstanceService{}, storageSvc)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/disks/disk-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleAttachDiskSuccess(t *testing.T) {
	instanceSvc := scheduledInstance("node-1")
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "tenant-1"}, nil
		},
		attachDiskFn: func(ctx context.Context, diskID, instanceNodeID, instanceID string) (storage.Disk, error) {
			if instanceNodeID != "node-1" || instanceID != "inst-1" {
				t.Fatalf("unexpected args: nodeID=%q instanceID=%q", instanceNodeID, instanceID)
			}
			return storage.Disk{ID: diskID, TenantID: "tenant-1", InstanceID: &instanceID, NodeID: instanceNodeID}, nil
		},
	}
	srv, tokens := newDiskTestServer(instanceSvc, storageSvc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/disks/disk-1/attach", attachDiskRequest{InstanceID: "inst-1"}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleAttachDiskWrongNode(t *testing.T) {
	instanceSvc := scheduledInstance("node-1")
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "tenant-1"}, nil
		},
		attachDiskFn: func(ctx context.Context, diskID, instanceNodeID, instanceID string) (storage.Disk, error) {
			return storage.Disk{}, storage.ErrWrongNode
		},
	}
	srv, tokens := newDiskTestServer(instanceSvc, storageSvc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/disks/disk-1/attach", attachDiskRequest{InstanceID: "inst-1"}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleDetachDiskRootRejected(t *testing.T) {
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "tenant-1", Type: storage.TypeRoot}, nil
		},
		detachDiskFn: func(ctx context.Context, diskID string) (storage.Disk, error) {
			return storage.Disk{}, storage.ErrRootDiskNotDetachable
		},
	}
	srv, tokens := newDiskTestServer(fakeInstanceService{}, storageSvc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/disks/disk-1/detach", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleResizeDiskShrinkRejected(t *testing.T) {
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "tenant-1", SizeGB: 50}, nil
		},
		resizeDiskFn: func(ctx context.Context, diskID string, newSizeGB int) (storage.Disk, error) {
			return storage.Disk{}, storage.ErrShrinkNotAllowed
		},
	}
	srv, tokens := newDiskTestServer(fakeInstanceService{}, storageSvc)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/disks/disk-1/resize", resizeDiskRequest{SizeGB: 10}, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleDeleteDiskStillAttached(t *testing.T) {
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "tenant-1"}, nil
		},
		deleteDiskFn: func(ctx context.Context, diskID string) error {
			return storage.ErrStillAttached
		},
	}
	srv, tokens := newDiskTestServer(fakeInstanceService{}, storageSvc)

	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/disks/disk-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestHandleDeleteDiskSuccess(t *testing.T) {
	storageSvc := fakeStorageService{
		getFn: func(ctx context.Context, id string) (storage.Disk, error) {
			return storage.Disk{ID: id, TenantID: "tenant-1"}, nil
		},
		deleteDiskFn: func(ctx context.Context, diskID string) error { return nil },
	}
	srv, tokens := newDiskTestServer(fakeInstanceService{}, storageSvc)

	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/disks/disk-1", nil, userAuthHeader(t, tokens, "tenant-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
