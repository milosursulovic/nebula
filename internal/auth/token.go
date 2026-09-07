package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/milosursulovic/nebula/internal/common"
)

const (
	// AccessTokenTTL is how long an issued access token remains valid.
	AccessTokenTTL = 15 * time.Minute
	// RefreshTokenTTL is how long an issued refresh token remains valid.
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// Claims are the JWT access token claims (spec section 7).
type Claims struct {
	TenantID string `json:"tenant_id"`
	Role     Role   `json:"role"`
	jwt.RegisteredClaims
}

// TokenIssuer issues and parses JWT access tokens using an HMAC secret.
type TokenIssuer struct {
	secret []byte
}

func NewTokenIssuer(secret string) TokenIssuer {
	return TokenIssuer{secret: []byte(secret)}
}

// IssueAccessToken creates a signed JWT for the given user/tenant/role.
func (t TokenIssuer) IssueAccessToken(userID, tenantID string, role Role) (string, error) {
	now := time.Now()
	claims := Claims{
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(t.secret)
}

// ParseAccessToken validates a JWT and returns its claims.
func (t TokenIssuer) ParseAccessToken(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return t.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid access token")
	}
	return claims, nil
}

// newRefreshToken generates a new opaque refresh token, returning both the
// raw value (to hand back to the client) and its SHA-256 hash (to store).
func newRefreshToken() (raw, hash string, err error) {
	return common.GenerateOpaqueToken()
}

func hashRefreshToken(raw string) string {
	return common.HashToken(raw)
}
