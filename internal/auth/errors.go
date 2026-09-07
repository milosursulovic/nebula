package auth

import "errors"

var (
	// ErrEmailTaken is returned when registering with an email that already exists.
	ErrEmailTaken = errors.New("email already registered")

	// ErrInvalidCredentials is returned for a wrong email/password on login,
	// and deliberately does not distinguish "no such user" from "wrong password".
	ErrInvalidCredentials = errors.New("invalid email or password")

	// ErrRefreshTokenInvalid is returned when a refresh token is unknown,
	// expired, or already revoked.
	ErrRefreshTokenInvalid = errors.New("invalid or expired refresh token")

	// ErrNoMembership is returned when a user has no tenant membership,
	// which should not happen for accounts created via Register.
	ErrNoMembership = errors.New("user has no tenant membership")

	// errNotFound is an internal repository-layer sentinel for "no row
	// matched". Service methods translate it into a domain-appropriate
	// error (ErrInvalidCredentials, ErrRefreshTokenInvalid, ...) so callers
	// never see raw persistence details.
	errNotFound = errors.New("not found")
)
