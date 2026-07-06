// Package auth holds Verve's local-authentication primitives: argon2id password
// hashing and opaque session tokens. It is pure crypto with no database or HTTP
// dependency, so it stays trivially testable and reusable by both the CLI
// (setting passwords) and the API (verifying logins). ADR 0008.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ErrInvalidHash means a stored hash is not a well-formed argon2id PHC string.
// It is distinct from a mere password mismatch (which returns ok=false, nil).
var ErrInvalidHash = errors.New("auth: invalid password hash")

// Params are the argon2id cost parameters. They are encoded into every hash, so
// verification uses the parameters the hash was created with and DefaultParams
// can be raised over time without invalidating existing hashes.
type Params struct {
	Memory      uint32 // KiB of memory to use
	Iterations  uint32 // number of passes over memory
	Parallelism uint8  // number of lanes
	SaltLength  uint32 // bytes of random salt
	KeyLength   uint32 // bytes of derived key
}

// DefaultParams are sensible argon2id parameters for an interactive login on
// self-hosted hardware: 64 MiB, 3 passes, 2 lanes (OWASP-aligned).
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

// HashPassword derives an argon2id hash of password under DefaultParams with a
// fresh random salt, returning it as a self-describing PHC string
// ($argon2id$v=19$m=…,t=…,p=…$salt$hash).
func HashPassword(password string) (string, error) {
	return DefaultParams.hash(password)
}

func (p Params) hash(password string) (string, error) {
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches the argon2id PHC-encoded hash.
// A well-formed hash that simply does not match returns (false, nil); only a
// malformed or unsupported hash returns an error. The comparison is
// constant-time to avoid leaking how much of the hash matched.
func VerifyPassword(password, encoded string) (bool, error) {
	params, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	other := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(key, other) == 1, nil
}

// decodeHash parses a PHC argon2id string back into its parameters, salt, and
// derived key.
func decodeHash(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// A well-formed PHC string is "$argon2id$v=…$m=…,t=…,p=…$salt$hash", which
	// Split renders as ["", "argon2id", "v=…", "m=…,t=…,p=…", salt, hash].
	if len(parts) != 6 {
		return Params{}, nil, nil, fmt.Errorf("%w: expected 6 fields, got %d", ErrInvalidHash, len(parts))
	}
	if parts[1] != "argon2id" {
		return Params{}, nil, nil, fmt.Errorf("%w: algorithm %q is not argon2id", ErrInvalidHash, parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: bad version field", ErrInvalidHash)
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("%w: unsupported argon2 version %d", ErrInvalidHash, version)
	}

	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: bad parameter field", ErrInvalidHash)
	}

	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: salt: %v", ErrInvalidHash, err)
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: hash: %v", ErrInvalidHash, err)
	}

	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
