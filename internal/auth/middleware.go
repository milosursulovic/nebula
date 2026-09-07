package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey int

const identityContextKey contextKey = iota

// Authenticate validates the Authorization: Bearer <jwt> header and, on
// success, stores the resolved Identity in the request context. Requests
// with a missing or invalid token are rejected with 401 before reaching the
// wrapped handler.
func Authenticate(tokens TokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r.Header.Get("Authorization"))
			if raw == "" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}

			claims, err := tokens.ParseAccessToken(raw)
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			identity := Identity{
				UserID:   claims.Subject,
				TenantID: claims.TenantID,
				Role:     claims.Role,
			}
			next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), identity)))
		})
	}
}

// RequireRole rejects requests whose authenticated Identity does not hold
// one of the given roles. It must run after Authenticate.
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	allowed := make(map[Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := IdentityFromContext(r.Context())
			if !ok || !allowed[identity.Role] {
				http.Error(w, "insufficient role", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IdentityFromContext retrieves the Identity stored by Authenticate.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey).(Identity)
	return identity, ok
}

func withIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, identity)
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
