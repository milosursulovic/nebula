package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenIssuerRoundTrip(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")

	token, err := issuer.IssueAccessToken("user-1", "tenant-1", RoleTenantAdmin)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := issuer.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}

	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
	}
	if claims.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", claims.TenantID, "tenant-1")
	}
	if claims.Role != RoleTenantAdmin {
		t.Errorf("Role = %q, want %q", claims.Role, RoleTenantAdmin)
	}
}

func TestTokenIssuerRejectsWrongSecret(t *testing.T) {
	issuer := NewTokenIssuer("secret-a")
	other := NewTokenIssuer("secret-b")

	token, err := issuer.IssueAccessToken("user-1", "tenant-1", RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	if _, err := other.ParseAccessToken(token); err == nil {
		t.Error("expected parsing with the wrong secret to fail")
	}
}

func TestTokenIssuerRejectsExpiredToken(t *testing.T) {
	issuer := NewTokenIssuer("test-secret")

	claims := Claims{
		TenantID: "tenant-1",
		Role:     RoleUser,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(issuer.secret)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	if _, err := issuer.ParseAccessToken(token); err == nil {
		t.Error("expected expired token to be rejected")
	}
}

func TestNewRefreshTokenHashesDeterministically(t *testing.T) {
	raw, hash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatal("expected non-empty raw token and hash")
	}
	if hashRefreshToken(raw) != hash {
		t.Error("hashRefreshToken(raw) should match the hash returned by newRefreshToken")
	}

	raw2, hash2, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}
	if raw == raw2 || hash == hash2 {
		t.Error("expected distinct refresh tokens across calls")
	}
}
