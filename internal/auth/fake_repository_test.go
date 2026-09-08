package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// fakeRepository is an in-memory Repository for unit-testing Service without
// a real database.
type fakeRepository struct {
	mu sync.Mutex

	nextID int

	usersByEmail  map[string]User
	usersByID     map[string]User
	tenants       map[string]Tenant
	memberships   map[string]TenantMembership // keyed by userID
	refreshTokens map[string]RefreshToken     // keyed by token hash
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		usersByEmail:  make(map[string]User),
		usersByID:     make(map[string]User),
		tenants:       make(map[string]Tenant),
		memberships:   make(map[string]TenantMembership),
		refreshTokens: make(map[string]RefreshToken),
	}
}

func (f *fakeRepository) newID(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s-%d", prefix, f.nextID)
}

func (f *fakeRepository) CreateTenantAndUser(ctx context.Context, tenantName, email, passwordHash string) (User, Tenant, TenantMembership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.usersByEmail[email]; exists {
		return User{}, Tenant{}, TenantMembership{}, ErrEmailTaken
	}

	tenant := Tenant{ID: f.newID("tenant"), Name: tenantName}
	user := User{ID: f.newID("user"), Email: email, PasswordHash: passwordHash}
	member := TenantMembership{ID: f.newID("member"), TenantID: tenant.ID, UserID: user.ID, Role: RoleTenantAdmin}

	f.tenants[tenant.ID] = tenant
	f.usersByEmail[email] = user
	f.usersByID[user.ID] = user
	f.memberships[user.ID] = member

	return user, tenant, member, nil
}

func (f *fakeRepository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	u, ok := f.usersByEmail[email]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}

func (f *fakeRepository) FindMembershipByUserID(ctx context.Context, userID string) (TenantMembership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	m, ok := f.memberships[userID]
	if !ok {
		return TenantMembership{}, errNotFound
	}
	return m, nil
}

func (f *fakeRepository) ListTenants(ctx context.Context) ([]Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	tenants := make([]Tenant, 0, len(f.tenants))
	for _, t := range f.tenants {
		tenants = append(tenants, t)
	}
	return tenants, nil
}

func (f *fakeRepository) CreateRefreshToken(ctx context.Context, rt RefreshToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	rt.ID = f.newID("refresh")
	f.refreshTokens[rt.TokenHash] = rt
	return nil
}

func (f *fakeRepository) FindRefreshTokenByHash(ctx context.Context, hash string) (RefreshToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	rt, ok := f.refreshTokens[hash]
	if !ok {
		return RefreshToken{}, errNotFound
	}
	return rt, nil
}

func (f *fakeRepository) RevokeRefreshToken(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for hash, rt := range f.refreshTokens {
		if rt.ID == id {
			revoked := time.Now()
			rt.RevokedAt = &revoked
			f.refreshTokens[hash] = rt
			return nil
		}
	}
	return nil
}
