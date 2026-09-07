package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Error("expected correct password to verify")
	}

	if VerifyPassword(hash, "wrong-password") {
		t.Error("expected wrong password to fail verification")
	}
}
