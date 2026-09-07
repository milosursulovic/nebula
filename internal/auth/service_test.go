package auth

import (
	"context"
	"errors"
	"testing"
)

func newTestService() Service {
	return NewService(newFakeRepository(), NewTokenIssuer("test-secret"))
}

func TestRegisterIssuesTokens(t *testing.T) {
	svc := newTestService()

	tokens, err := svc.Register(context.Background(), "ana@example.com", "hunter2hunter2", "ana-co")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("expected non-empty access and refresh tokens")
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, "dup@example.com", "hunter2hunter2", "co-1"); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err := svc.Register(ctx, "dup@example.com", "hunter2hunter2", "co-2")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("Register duplicate = %v, want ErrEmailTaken", err)
	}
}

func TestLoginSuccess(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, "bob@example.com", "correct-password", "bob-co"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tokens, err := svc.Login(ctx, "bob@example.com", "correct-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Register(ctx, "carol@example.com", "correct-password", "carol-co"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := svc.Login(ctx, "carol@example.com", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login wrong password = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginUnknownEmail(t *testing.T) {
	svc := newTestService()

	_, err := svc.Login(context.Background(), "nobody@example.com", "whatever123")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login unknown email = %v, want ErrInvalidCredentials", err)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tokens, err := svc.Register(ctx, "dave@example.com", "correct-password", "dave-co")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	rotated, err := svc.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if rotated.RefreshToken == tokens.RefreshToken {
		t.Error("expected refresh to issue a new refresh token")
	}

	// The old refresh token must no longer be usable.
	if _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("Refresh with stale token = %v, want ErrRefreshTokenInvalid", err)
	}

	// The rotated token must work.
	if _, err := svc.Refresh(ctx, rotated.RefreshToken); err != nil {
		t.Fatalf("Refresh with rotated token: %v", err)
	}
}

func TestRefreshWithUnknownToken(t *testing.T) {
	svc := newTestService()

	_, err := svc.Refresh(context.Background(), "not-a-real-token")
	if !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("Refresh unknown token = %v, want ErrRefreshTokenInvalid", err)
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tokens, err := svc.Register(ctx, "erin@example.com", "correct-password", "erin-co")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := svc.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("Refresh after logout = %v, want ErrRefreshTokenInvalid", err)
	}
}
