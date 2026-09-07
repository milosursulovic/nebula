package common

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateOpaqueToken generates a new random opaque bearer token, returning
// both the raw value (to hand back to the client) and its SHA-256 hash (to
// store — the raw value is never persisted). Used for any long-lived,
// non-JWT credential: refresh tokens, node tokens, etc.
func GenerateOpaqueToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

// HashToken hashes a raw opaque token for lookup/comparison against a stored hash.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
