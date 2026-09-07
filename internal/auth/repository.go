package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the persistence boundary the auth Service depends on.
// Defined here (consumer side) so it can be faked in unit tests without a
// real database.
type Repository interface {
	// CreateTenantAndUser transactionally creates a new tenant, a new user,
	// and a TENANT_ADMIN membership linking them. Returns ErrEmailTaken if
	// the email is already registered.
	CreateTenantAndUser(ctx context.Context, tenantName, email, passwordHash string) (User, Tenant, TenantMembership, error)

	FindUserByEmail(ctx context.Context, email string) (User, error)
	FindMembershipByUserID(ctx context.Context, userID string) (TenantMembership, error)

	CreateRefreshToken(ctx context.Context, rt RefreshToken) error
	FindRefreshTokenByHash(ctx context.Context, hash string) (RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id string) error
}

type pgxRepository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a Repository backed by PostgreSQL.
func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgxRepository{pool: pool}
}

func (r *pgxRepository) CreateTenantAndUser(ctx context.Context, tenantName, email, passwordHash string) (User, Tenant, TenantMembership, error) {
	var (
		tenant Tenant
		user   User
		member TenantMembership
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO tenants (name) VALUES ($1)
			 RETURNING id, name, created_at, updated_at`,
			tenantName,
		).Scan(&tenant.ID, &tenant.Name, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx,
			`INSERT INTO users (email, password_hash) VALUES ($1, $2)
			 RETURNING id, email, password_hash, created_at, updated_at`,
			email, passwordHash,
		).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx,
			`INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, $3)
			 RETURNING id, tenant_id, user_id, role, created_at`,
			tenant.ID, user.ID, string(RoleTenantAdmin),
		).Scan(&member.ID, &member.TenantID, &member.UserID, &member.Role, &member.CreatedAt); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, Tenant{}, TenantMembership{}, ErrEmailTaken
		}
		return User{}, Tenant{}, TenantMembership{}, err
	}

	return user, tenant, member, nil
}

func (r *pgxRepository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at, updated_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, errNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func (r *pgxRepository) FindMembershipByUserID(ctx context.Context, userID string) (TenantMembership, error) {
	var m TenantMembership
	err := r.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, role, created_at FROM tenant_members WHERE user_id = $1 LIMIT 1`,
		userID,
	).Scan(&m.ID, &m.TenantID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantMembership{}, errNotFound
	}
	if err != nil {
		return TenantMembership{}, err
	}
	return m, nil
}

func (r *pgxRepository) CreateRefreshToken(ctx context.Context, rt RefreshToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, tenant_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		rt.UserID, rt.TenantID, rt.TokenHash, rt.ExpiresAt,
	)
	return err
}

func (r *pgxRepository) FindRefreshTokenByHash(ctx context.Context, hash string) (RefreshToken, error) {
	var rt RefreshToken
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, tenant_id, token_hash, expires_at, revoked_at, created_at
		 FROM refresh_tokens WHERE token_hash = $1`,
		hash,
	).Scan(&rt.ID, &rt.UserID, &rt.TenantID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshToken{}, errNotFound
	}
	if err != nil {
		return RefreshToken{}, err
	}
	return rt, nil
}

func (r *pgxRepository) RevokeRefreshToken(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		id,
	)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
