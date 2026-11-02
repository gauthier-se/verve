// Package auth holds what authentication relies on: password hashing and the rules of passwords and
// usernames.
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

const (
	saltLength   = 16
	digestLength = 32
)

// ErrMalformedHash is returned for a password hash that Hasher.Hash cannot have written. Its message
// never holds the hash.
var ErrMalformedHash = errors.New("malformed password hash")

// Hasher hashes passwords with argon2id. Its parameters are those of new hashes: each hash stores
// its own, so raising them leaves the existing hashes valid.
type Hasher struct {
	// Memory is in KiB.
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultHasher returns the hasher of production, at the OWASP minimum: 19 MiB, 2 iterations and a
// parallelism of 1.
func DefaultHasher() Hasher {
	return Hasher{Memory: 19 * 1024, Iterations: 2, Parallelism: 1}
}

// Hash returns the PHC string of password, with a random salt.
func (h Hasher) Hash(password string) string {
	salt := make([]byte, saltLength)
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(salt)

	return fmt.Sprintf("$argon2id$v=%d$%s$%s$%s", argon2.Version, h.encodeParams(),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(h.digest(password, salt)))
}

// Verify reports whether password is the one of hash, with the parameters stored in hash. It returns
// ErrMalformedHash when hash is malformed.
func (Hasher) Verify(hash, password string) (bool, error) {
	stored, err := parseHash(hash)
	if err != nil {
		return false, err
	}

	return subtle.ConstantTimeCompare(stored.params.digest(password, stored.salt), stored.digest) == 1, nil
}

// NeedsRehash reports whether a parameter of hash is below the one of h. It returns ErrMalformedHash
// when hash is malformed.
func (h Hasher) NeedsRehash(hash string) (bool, error) {
	stored, err := parseHash(hash)
	if err != nil {
		return false, err
	}

	return stored.params.Memory < h.Memory ||
		stored.params.Iterations < h.Iterations ||
		stored.params.Parallelism < h.Parallelism, nil
}

func (h Hasher) digest(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, h.Iterations, h.Memory, h.Parallelism, digestLength)
}

func (h Hasher) encodeParams() string {
	return fmt.Sprintf("m=%d,t=%d,p=%d", h.Memory, h.Iterations, h.Parallelism)
}

// parsedHash is a decoded PHC string.
type parsedHash struct {
	params Hasher
	salt   []byte
	digest []byte
}

// parseHash decodes what Hash writes and nothing else. Its checks stand between a stored hash and
// argon2.IDKey, which panics on zero iterations or zero parallelism, and they tell a damaged hash
// from a wrong password.
func parseHash(hash string) (parsedHash, error) {
	fields := strings.Split(hash, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" || fields[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return parsedHash{}, ErrMalformedHash
	}

	var (
		parsed parsedHash
		err    error
	)

	// Sscanf stops at the end of its format: encoding the values back rejects what follows, as well
	// as leading zeros.
	p := &parsed.params
	if _, err = fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil ||
		p.encodeParams() != fields[3] || p.Memory == 0 || p.Iterations == 0 || p.Parallelism == 0 {
		return parsedHash{}, ErrMalformedHash
	}

	if parsed.salt, err = base64.RawStdEncoding.DecodeString(fields[4]); err != nil || len(parsed.salt) != saltLength {
		return parsedHash{}, ErrMalformedHash
	}

	if parsed.digest, err = base64.RawStdEncoding.DecodeString(fields[5]); err != nil || len(parsed.digest) != digestLength {
		return parsedHash{}, ErrMalformedHash
	}

	return parsed, nil
}
