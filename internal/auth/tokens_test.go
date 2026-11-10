package auth

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// base62 is the alphabet of the random part of an API token.
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// homeAssistant is a read-only token of userID for a Home Assistant sensor, valid one year.
func homeAssistant(userID int64) NewAPIToken {
	return NewAPIToken{
		UserID:    userID,
		Name:      "Home Assistant",
		Scopes:    []store.TokenScope{store.ScopeRead},
		ExpiresAt: sessionsTestNow.Add(365 * day),
	}
}

// Given a new token, then it is vrv_ followed by 43 base62 characters, which hold at least 32
// random bytes, and two tokens differ.
func TestCreateAPIToken_IsVrvFollowedByBase62Characters(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")

	token, _, err := CreateAPIToken(ctx, s, homeAssistant(user.ID))
	require.NoError(t, err)
	require.Regexp(t, `^vrv_[0-9A-Za-z]{43}$`, token)

	other, _, err := CreateAPIToken(ctx, s, homeAssistant(user.ID))
	require.NoError(t, err)
	require.NotEqual(t, token, other)
}

// Given a new token, then the store holds it under its SHA-256 hash, with its first 8 characters
// as display prefix, and the name, scopes and expiry of the request.
func TestCreateAPIToken_StoresOnlyTheHashAndADisplayPrefix(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")

	token, created, err := CreateAPIToken(ctx, s, homeAssistant(user.ID))
	require.NoError(t, err)

	hash := sha256.Sum256([]byte(token))

	stored, _, err := s.APITokens().GetByHash(ctx, hash[:])
	require.NoError(t, err)
	require.Equal(t, created, stored)

	require.Equal(t, token[:8], stored.Prefix)
	require.True(t, strings.HasPrefix(stored.Prefix, "vrv_"))
	require.Equal(t, user.ID, stored.UserID)
	require.Equal(t, "Home Assistant", stored.Name)
	require.Equal(t, []store.TokenScope{store.ScopeRead}, stored.Scopes)
	require.Equal(t, sessionsTestNow.Add(365*day), stored.ExpiresAt)
}

// Given a token without scopes, then CreateAPIToken returns store.ErrInvalid and no token.
func TestCreateAPIToken_WithoutScopesReturnsErrInvalid(t *testing.T) {
	t.Parallel()

	s, _, _ := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")

	request := homeAssistant(user.ID)
	request.Scopes = nil

	token, _, err := CreateAPIToken(ctx, s, request)
	require.ErrorIs(t, err, store.ErrInvalid)
	require.Empty(t, token)
}

// Given many tokens, then their random parts only hold base62 characters, and each of the 62
// appears within 10% of the mean, which a modulo bias breaks: 256 mod 62 makes 8 characters 25%
// more frequent. With 5000 tokens, an unbiased count leaves the band about once in 10^7 runs.
func TestGenerateAPIToken_DrawsTheBase62AlphabetUniformly(t *testing.T) {
	t.Parallel()

	const tokens = 5000

	counts := make(map[rune]int, len(base62))

	for range tokens {
		token := generateAPIToken()
		require.True(t, strings.HasPrefix(token, "vrv_"), token)

		for _, c := range token[len("vrv_"):] {
			require.True(t, strings.ContainsRune(base62, c), "character %q of %s", c, token)

			counts[c]++
		}
	}

	require.Len(t, counts, len(base62))

	mean := float64(tokens*43) / float64(len(base62))
	for c, count := range counts {
		require.InDelta(t, mean, float64(count), mean/10, "character %q", c)
	}
}
