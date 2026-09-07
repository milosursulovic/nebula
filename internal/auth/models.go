package auth

import "time"

// Role is a RBAC role assigned to a user within a tenant (spec section 8).
type Role string

const (
	RoleSuperAdmin  Role = "SUPER_ADMIN"
	RoleTenantAdmin Role = "TENANT_ADMIN"
	RoleUser        Role = "USER"
)

// Valid reports whether r is one of the known RBAC roles.
func (r Role) Valid() bool {
	switch r {
	case RoleSuperAdmin, RoleTenantAdmin, RoleUser:
		return true
	default:
		return false
	}
}

// Tenant is an isolated customer account. Every cloud resource in NEBULA
// belongs to exactly one tenant (spec section 6).
type Tenant struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User is a NEBULA account holder. A user's role is not stored here — it is
// scoped per tenant via TenantMembership.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TenantMembership links a user to a tenant with a specific role.
type TenantMembership struct {
	ID        string
	TenantID  string
	UserID    string
	Role      Role
	CreatedAt time.Time
}

// RefreshToken is server-side state backing a refresh token so it can be
// revoked (required for real logout against stateless JWT access tokens).
// Only the SHA-256 hash of the token is ever stored.
type RefreshToken struct {
	ID        string
	UserID    string
	TenantID  string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}
