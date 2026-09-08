package auth

import (
	"context"
	"errors"
	"time"
)

// TokenPair is what Register/Login/Refresh hand back to the client.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // access token lifetime, in seconds
}

// Identity is the caller's resolved identity, as carried in a JWT.
type Identity struct {
	UserID   string
	TenantID string
	Role     Role
}

// Service is the auth business logic surface consumed by pkg/api handlers.
type Service interface {
	Register(ctx context.Context, email, password, tenantName string) (TokenPair, error)
	Login(ctx context.Context, email, password string) (TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
	ListTenants(ctx context.Context) ([]Tenant, error)
}

type service struct {
	repo   Repository
	tokens TokenIssuer
}

func NewService(repo Repository, tokens TokenIssuer) Service {
	return &service{repo: repo, tokens: tokens}
}

func (s *service) Register(ctx context.Context, email, password, tenantName string) (TokenPair, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return TokenPair{}, err
	}

	user, tenant, member, err := s.repo.CreateTenantAndUser(ctx, tenantName, email, hash)
	if err != nil {
		return TokenPair{}, err // may be ErrEmailTaken
	}

	return s.issueTokenPair(ctx, user.ID, tenant.ID, member.Role)
}

func (s *service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return TokenPair{}, ErrInvalidCredentials
		}
		return TokenPair{}, err
	}

	if !VerifyPassword(user.PasswordHash, password) {
		return TokenPair{}, ErrInvalidCredentials
	}

	member, err := s.repo.FindMembershipByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return TokenPair{}, ErrNoMembership
		}
		return TokenPair{}, err
	}

	return s.issueTokenPair(ctx, user.ID, member.TenantID, member.Role)
}

func (s *service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	rt, err := s.lookupValidRefreshToken(ctx, refreshToken)
	if err != nil {
		return TokenPair{}, err
	}

	if err := s.repo.RevokeRefreshToken(ctx, rt.ID); err != nil {
		return TokenPair{}, err
	}

	member, err := s.repo.FindMembershipByUserID(ctx, rt.UserID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return TokenPair{}, ErrNoMembership
		}
		return TokenPair{}, err
	}

	return s.issueTokenPair(ctx, rt.UserID, rt.TenantID, member.Role)
}

func (s *service) Logout(ctx context.Context, refreshToken string) error {
	rt, err := s.lookupValidRefreshToken(ctx, refreshToken)
	if err != nil {
		return err
	}
	return s.repo.RevokeRefreshToken(ctx, rt.ID)
}

func (s *service) ListTenants(ctx context.Context) ([]Tenant, error) {
	return s.repo.ListTenants(ctx)
}

func (s *service) lookupValidRefreshToken(ctx context.Context, raw string) (RefreshToken, error) {
	rt, err := s.repo.FindRefreshTokenByHash(ctx, hashRefreshToken(raw))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return RefreshToken{}, ErrRefreshTokenInvalid
		}
		return RefreshToken{}, err
	}
	if rt.RevokedAt != nil || time.Now().After(rt.ExpiresAt) {
		return RefreshToken{}, ErrRefreshTokenInvalid
	}
	return rt, nil
}

func (s *service) issueTokenPair(ctx context.Context, userID, tenantID string, role Role) (TokenPair, error) {
	access, err := s.tokens.IssueAccessToken(userID, tenantID, role)
	if err != nil {
		return TokenPair{}, err
	}

	rawRefresh, hash, err := newRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}

	if err := s.repo.CreateRefreshToken(ctx, RefreshToken{
		UserID:    userID,
		TenantID:  tenantID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(RefreshTokenTTL),
	}); err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}
