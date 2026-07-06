package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// sessionTokenBytes is the entropy of a session token. 256 bits is far beyond
// brute-force reach, so the opaque token needs no additional signing: it is a
// random secret looked up server-side, not a bearer of claims.
const sessionTokenBytes = 32

// NewSessionToken returns a fresh, URL-safe opaque session token to hand to the
// client in a cookie. Only its HashToken digest is ever stored, so a leak of the
// sessions table cannot reconstruct live cookies.
func NewSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the hex SHA-256 of a session token — the value stored and
// looked up in the sessions table. SHA-256 (not argon2id) is right here: the
// token is high-entropy random, so it needs a fast pre-image-resistant digest,
// not a slow password hash.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
