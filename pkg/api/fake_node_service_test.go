package api

import (
	"context"

	"github.com/milosursulovic/nebula/internal/node"
)

// fakeNodeService is a hand-rolled node.Service double for handler tests.
type fakeNodeService struct {
	registerFn  func(ctx context.Context, in node.RegisterInput) (node.RegisterResult, error)
	listFn      func(ctx context.Context) ([]node.Node, error)
	getFn       func(ctx context.Context, id string) (node.Node, error)
	heartbeatFn func(ctx context.Context, id string, in node.HeartbeatInput) error
	authNodeFn  func(ctx context.Context, id, rawToken string) error
}

func (f fakeNodeService) Register(ctx context.Context, in node.RegisterInput) (node.RegisterResult, error) {
	return f.registerFn(ctx, in)
}

func (f fakeNodeService) List(ctx context.Context) ([]node.Node, error) {
	return f.listFn(ctx)
}

func (f fakeNodeService) Get(ctx context.Context, id string) (node.Node, error) {
	return f.getFn(ctx, id)
}

func (f fakeNodeService) Heartbeat(ctx context.Context, id string, in node.HeartbeatInput) error {
	return f.heartbeatFn(ctx, id, in)
}

func (f fakeNodeService) AuthenticateNodeToken(ctx context.Context, id, rawToken string) error {
	return f.authNodeFn(ctx, id, rawToken)
}
