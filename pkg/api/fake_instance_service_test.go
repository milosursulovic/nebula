package api

import (
	"context"

	"github.com/milosursulovic/nebula/internal/instance"
)

// fakeInstanceService is a hand-rolled instance.Service double for handler tests.
type fakeInstanceService struct {
	createFn     func(ctx context.Context, tenantID string, in instance.CreateInput) (instance.Instance, error)
	listFn       func(ctx context.Context, tenantID string) ([]instance.Instance, error)
	getFn        func(ctx context.Context, tenantID, id string) (instance.Instance, error)
	deleteFn     func(ctx context.Context, tenantID, id string) (instance.Instance, error)
	transitionFn func(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error)
	setNodeIDFn  func(ctx context.Context, tenantID, id, nodeID string) (instance.Instance, error)
	setIPFn      func(ctx context.Context, tenantID, id, ip string) (instance.Instance, error)
}

func (f fakeInstanceService) Create(ctx context.Context, tenantID string, in instance.CreateInput) (instance.Instance, error) {
	return f.createFn(ctx, tenantID, in)
}

func (f fakeInstanceService) List(ctx context.Context, tenantID string) ([]instance.Instance, error) {
	return f.listFn(ctx, tenantID)
}

func (f fakeInstanceService) Get(ctx context.Context, tenantID, id string) (instance.Instance, error) {
	return f.getFn(ctx, tenantID, id)
}

func (f fakeInstanceService) Delete(ctx context.Context, tenantID, id string) (instance.Instance, error) {
	return f.deleteFn(ctx, tenantID, id)
}

func (f fakeInstanceService) Transition(ctx context.Context, tenantID, id string, to instance.Status) (instance.Instance, error) {
	return f.transitionFn(ctx, tenantID, id, to)
}

func (f fakeInstanceService) SetNodeID(ctx context.Context, tenantID, id, nodeID string) (instance.Instance, error) {
	return f.setNodeIDFn(ctx, tenantID, id, nodeID)
}

func (f fakeInstanceService) SetIPAddress(ctx context.Context, tenantID, id, ip string) (instance.Instance, error) {
	return f.setIPFn(ctx, tenantID, id, ip)
}
