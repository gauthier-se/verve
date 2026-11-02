package auth_test

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
)

const (
	// referencePassword and referenceHash come from the argon2 command of libargon2, the reference
	// implementation: printf '%s' "$password" | argon2 0123456789abcdef -id -t 2 -k 64 -p 1 -l 32 -e
	referencePassword = "correct horse battery staple"
	referenceHash     = "$argon2id$v=19$m=64,t=2,p=1$MDEyMzQ1Njc4OWFiY2RlZg$gAwJW5vx5itn2V9KHlEUjyUCrpDYgKyQf0gMaHGx1h4"

	referenceSalt   = "MDEyMzQ1Njc4OWFiY2RlZg"
	referenceDigest = "gAwJW5vx5itn2V9KHlEUjyUCrpDYgKyQf0gMaHGx1h4"
)

// cheapHasher returns a hasher that keeps the tests fast.
func cheapHasher() auth.Hasher {
	return auth.Hasher{Memory: 64, Iterations: 1, Parallelism: 1}
}

// Given a password, then the hash is an argon2id PHC string with its parameters, a salt of 16 bytes
// and a digest of 32 bytes.
func TestHasher_HashIsAnArgon2idPHCStringWithItsParameters(t *testing.T) {
	t.Parallel()

	phc := regexp.MustCompile(`^\$argon2id\$v=19\$m=64,t=1,p=1\$([A-Za-z0-9+/]+)\$([A-Za-z0-9+/]+)$`)

	match := phc.FindStringSubmatch(cheapHasher().Hash("a password"))
	require.NotNil(t, match)

	salt, err := base64.RawStdEncoding.DecodeString(match[1])
	require.NoError(t, err)
	require.Len(t, salt, 16)

	digest, err := base64.RawStdEncoding.DecodeString(match[2])
	require.NoError(t, err)
	require.Len(t, digest, 32)
}

// Given the same password twice, then the salts differ, and so do the hashes.
func TestHasher_HashUsesARandomSalt(t *testing.T) {
	t.Parallel()

	hasher := cheapHasher()

	first := strings.Split(hasher.Hash("a password"), "$")
	second := strings.Split(hasher.Hash("a password"), "$")

	require.NotEqual(t, first[4], second[4])
	require.NotEqual(t, first[5], second[5])
}

// Given the default hasher, then its parameters are the OWASP minimum: 19 MiB, 2 iterations and a
// parallelism of 1.
func TestDefaultHasher_MeetsTheOWASPMinimum(t *testing.T) {
	t.Parallel()

	hash := auth.DefaultHasher().Hash("a password")

	require.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"), hash)
}

// Given a hash, then verification accepts the right password and rejects a wrong one.
func TestHasher_VerifyAcceptsTheRightPasswordAndRejectsAWrongOne(t *testing.T) {
	t.Parallel()

	hasher := cheapHasher()
	hash := hasher.Hash("the right password")

	ok, err := hasher.Verify(hash, "the right password")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = hasher.Verify(hash, "a wrong password")
	require.NoError(t, err)
	require.False(t, ok)
}

// Given a hash from the reference implementation, then verification agrees with it, using the
// parameters of the hash rather than those of the hasher.
func TestHasher_VerifyUsesTheParametersOfTheHash(t *testing.T) {
	t.Parallel()

	hasher := auth.Hasher{Memory: 128, Iterations: 3, Parallelism: 2}

	ok, err := hasher.Verify(referenceHash, referencePassword)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = hasher.Verify(referenceHash, referencePassword+"!")
	require.NoError(t, err)
	require.False(t, ok)
}

// Given a malformed hash, then verification and NeedsRehash return an error rather than a verdict,
// and never panic: the parameters of a stored hash reach argon2, which panics on zero iterations or
// zero parallelism.
func TestHasher_MalformedHashIsAnError(t *testing.T) {
	t.Parallel()

	phc := func(algorithm, version, params, salt, digest string) string {
		return strings.Join([]string{"", algorithm, version, params, salt, digest}, "$")
	}

	tests := []struct {
		name string
		hash string
	}{
		{"Empty", ""},
		{"NotAHash", "a password in clear text"},
		{"NoLeadingSeparator", strings.TrimPrefix(referenceHash, "$")},
		{"Bcrypt", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"},
		{"OtherAlgorithm", phc("argon2i", "v=19", "m=64,t=2,p=1", referenceSalt, referenceDigest)},
		{"OtherVersion", phc("argon2id", "v=16", "m=64,t=2,p=1", referenceSalt, referenceDigest)},
		{"MissingDigest", "$argon2id$v=19$m=64,t=2,p=1$" + referenceSalt},
		{"ExtraField", referenceHash + "$extra"},
		{"ZeroMemory", phc("argon2id", "v=19", "m=0,t=2,p=1", referenceSalt, referenceDigest)},
		{"ZeroIterations", phc("argon2id", "v=19", "m=64,t=0,p=1", referenceSalt, referenceDigest)},
		{"ZeroParallelism", phc("argon2id", "v=19", "m=64,t=2,p=0", referenceSalt, referenceDigest)},
		{"MemoryOverflow", phc("argon2id", "v=19", "m=4294967296,t=2,p=1", referenceSalt, referenceDigest)},
		{"IterationsOverflow", phc("argon2id", "v=19", "m=64,t=4294967296,p=1", referenceSalt, referenceDigest)},
		{"ParallelismOverflow", phc("argon2id", "v=19", "m=64,t=2,p=256", referenceSalt, referenceDigest)},
		{"NegativeMemory", phc("argon2id", "v=19", "m=-1,t=2,p=1", referenceSalt, referenceDigest)},
		{"ParametersInAnotherOrder", phc("argon2id", "v=19", "t=2,m=64,p=1", referenceSalt, referenceDigest)},
		{"MissingParameter", phc("argon2id", "v=19", "m=64,t=2", referenceSalt, referenceDigest)},
		{"TrailingParameter", phc("argon2id", "v=19", "m=64,t=2,p=1,x=3", referenceSalt, referenceDigest)},
		{"ParameterWithLeadingZero", phc("argon2id", "v=19", "m=064,t=2,p=1", referenceSalt, referenceDigest)},
		{"SaltNotBase64", phc("argon2id", "v=19", "m=64,t=2,p=1", "not base64!", referenceDigest)},
		{"DigestNotBase64", phc("argon2id", "v=19", "m=64,t=2,p=1", referenceSalt, "not base64!")},
		{"EmptySalt", phc("argon2id", "v=19", "m=64,t=2,p=1", "", referenceDigest)},
		{"EmptyDigest", phc("argon2id", "v=19", "m=64,t=2,p=1", referenceSalt, "")},
		{"ShortSalt", phc("argon2id", "v=19", "m=64,t=2,p=1", "MDEyMzQ1Njc", referenceDigest)},
		{"ShortDigest", phc("argon2id", "v=19", "m=64,t=2,p=1", referenceSalt, "gAwJ")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hasher := cheapHasher()

			ok, err := hasher.Verify(tc.hash, referencePassword)
			require.ErrorIs(t, err, auth.ErrMalformedHash)
			require.False(t, ok)

			_, err = hasher.NeedsRehash(tc.hash)
			require.ErrorIs(t, err, auth.ErrMalformedHash)
		})
	}
}

// Given a hash with parameters below the current ones, then NeedsRehash reports it. A hash above
// the current ones is left alone: rehashing it would weaken it.
func TestHasher_NeedsRehashReportsParametersBelowTheCurrentOnes(t *testing.T) {
	t.Parallel()

	current := auth.Hasher{Memory: 64, Iterations: 2, Parallelism: 2}

	tests := []struct {
		name   string
		stored auth.Hasher
		want   bool
	}{
		{"SameParameters", current, false},
		{"LowerMemory", auth.Hasher{Memory: 32, Iterations: 2, Parallelism: 2}, true},
		{"LowerIterations", auth.Hasher{Memory: 64, Iterations: 1, Parallelism: 2}, true},
		{"LowerParallelism", auth.Hasher{Memory: 64, Iterations: 2, Parallelism: 1}, true},
		{"HigherParameters", auth.Hasher{Memory: 128, Iterations: 3, Parallelism: 3}, false},
		{"LowerMemoryAndHigherIterations", auth.Hasher{Memory: 32, Iterations: 3, Parallelism: 2}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := current.NeedsRehash(tc.stored.Hash("a password"))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
