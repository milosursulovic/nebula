package api

import (
	"context"

	"github.com/milosursulovic/nebula/internal/auth"
)

// fakeAuthService is a hand-rolled auth.Service double for handler tests —
// no database, no real token issuing, fully scripted per test case.
type fakeAuthService struct {
	registerFn    func(ctx context.Context, email, password, tenantName string) (auth.TokenPair, error)
	loginFn       func(ctx context.Context, email, password string) (auth.TokenPair, error)
	refreshFn     func(ctx context.Context, refreshToken string) (auth.TokenPair, error)
	logoutFn      func(ctx context.Context, refreshToken string) error
	listTenantsFn func(ctx context.Context) ([]auth.Tenant, error)
}

func (f fakeAuthService) Register(ctx context.Context, email, password, tenantName string) (auth.TokenPair, error) {
	return f.registerFn(ctx, email, password, tenantName)
}

func (f fakeAuthService) Login(ctx context.Context, email, password string) (auth.TokenPair, error) {
	return f.loginFn(ctx, email, password)
}

func (f fakeAuthService) Refresh(ctx context.Context, refreshToken string) (auth.TokenPair, error) {
	return f.refreshFn(ctx, refreshToken)
}

func (f fakeAuthService) Logout(ctx context.Context, refreshToken string) error {
	return f.logoutFn(ctx, refreshToken)
}

func (f fakeAuthService) ListTenants(ctx context.Context) ([]auth.Tenant, error) {
	return f.listTenantsFn(ctx)
}
