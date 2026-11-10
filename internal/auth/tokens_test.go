package auth

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

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

// Given a valid token, when requests use it from changing addresses, then its last used date and IP
// are written by the first request, kept for a minute, and written again by the first request
// after that minute.
func TestTokens_AuthenticateWritesTheLastUseAtMostOncePerMinute(t *testing.T) {
	t.Parallel()

	s, _, clk := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")
	tokens := NewTokens(clk)

	token, _, err := CreateAPIToken(ctx, s, homeAssistant(user.ID))
	require.NoError(t, err)

	steps := []struct {
		name    string
		advance time.Duration
		ip      string
		// wantAt is how long after sessionsTestNow the last use is, and wantIP its address.
		wantAt time.Duration
		wantIP string
	}{
		{name: "first use", ip: "203.0.113.7", wantAt: 0, wantIP: "203.0.113.7"},
		{name: "30 seconds later", advance: 30 * time.Second, ip: "198.51.100.2", wantAt: 0, wantIP: "203.0.113.7"},
		{name: "59.999 seconds after the write", advance: 29_999 * time.Millisecond, ip: "198.51.100.2", wantAt: 0, wantIP: "203.0.113.7"},
		{name: "a minute after the write", advance: time.Millisecond, ip: "198.51.100.2", wantAt: time.Minute, wantIP: "198.51.100.2"},
		{name: "without an address, a minute later", advance: time.Minute, wantAt: 2 * time.Minute, wantIP: ""},
	}

	for _, step := range steps {
		clk.Advance(step.advance)

		var ip netip.Addr
		if step.ip != "" {
			ip = netip.MustParseAddr(step.ip)
		}

		got, gotUser, err := tokens.Authenticate(ctx, s, token, ip)
		require.NoError(t, err, step.name)
		require.Equal(t, user, gotUser, step.name)
		require.Equal(t, sessionsTestNow.Add(step.wantAt), got.LastUsedAt, step.name)
		require.Equal(t, step.wantIP, got.LastUsedIP, step.name)

		stored, _, err := s.APITokens().GetByHash(ctx, hashAPIToken(token))
		require.NoError(t, err)
		require.Equal(t, got, stored, step.name)
	}
}

// Given an unknown token, then Authenticate returns store.ErrNotFound. Given a revoked token, an
// expired token or a token of a user who is not active, then it returns ErrTokenRevoked,
// ErrTokenExpired or ErrUserDisabled, records the token.rejected event of that reason about the
// owner, and leaves the last use unwritten.
func TestTokens_AuthenticateTellsTheRejectionsApart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare changes the token of user, or the user, before the request.
		prepare func(t *testing.T, s store.Store, user store.User, token store.APIToken)
		// expiresIn is the lifetime of the token, 0 for none.
		expiresIn time.Duration
		want      error
		reason    string
	}{
		{
			name: "revoked",
			prepare: func(t *testing.T, s store.Store, user store.User, token store.APIToken) {
				t.Helper()

				_, err := s.APITokens().Revoke(t.Context(), user.ID, token.ID)
				require.NoError(t, err)
			},
			want:   ErrTokenRevoked,
			reason: "revoked",
		},
		{name: "expired", expiresIn: time.Hour, want: ErrTokenExpired, reason: "expired"},
		{
			name: "disabled user",
			prepare: func(t *testing.T, s store.Store, user store.User, _ store.APIToken) {
				t.Helper()

				_, err := s.Users().SetStatus(t.Context(), user.ID, store.UserDisabled)
				require.NoError(t, err)
			},
			want:   ErrUserDisabled,
			reason: "user_disabled",
		},
		{
			name: "user being deleted",
			prepare: func(t *testing.T, s store.Store, user store.User, _ store.APIToken) {
				t.Helper()

				_, err := s.Users().SetStatus(t.Context(), user.ID, store.UserDeleting)
				require.NoError(t, err)
			},
			want:   ErrUserDisabled,
			reason: "user_disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _, clk := newTestSessions(t)
			ctx := t.Context()
			user := mustCreateUser(ctx, t, s, "alice")
			tokens := NewTokens(clk)

			request := homeAssistant(user.ID)
			request.ExpiresAt = time.Time{}
			if tt.expiresIn != 0 {
				request.ExpiresAt = sessionsTestNow.Add(tt.expiresIn)
			}

			token, created, err := CreateAPIToken(ctx, s, request)
			require.NoError(t, err)

			if tt.prepare != nil {
				tt.prepare(t, s, user, created)
			}

			clk.Advance(2 * time.Hour)

			_, _, err = tokens.Authenticate(ctx, s, token, netip.MustParseAddr("203.0.113.7"))
			require.ErrorIs(t, err, tt.want)

			stored, _, err := s.APITokens().GetByHash(ctx, hashAPIToken(token))
			require.NoError(t, err)
			require.True(t, stored.LastUsedAt.IsZero())

			events, err := s.AuditEvents().List(ctx, store.AuditEventsFilter{Type: "token.rejected"}, store.PageRequest{Limit: 10})
			require.NoError(t, err)
			require.Len(t, events, 1)
			require.Zero(t, events[0].ActorID)
			require.Equal(t, user.ID, events[0].SubjectID)
			require.JSONEq(t, fmt.Sprintf(`{"id":%d,"reason":%q}`, created.ID, tt.reason), string(events[0].Payload))
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		s, _, clk := newTestSessions(t)

		_, _, err := NewTokens(clk).Authenticate(t.Context(), s, "vrv_unknown", netip.Addr{})
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

// Given a token that expires at a time, then it is valid until that time included, and expired
// after it.
func TestTokens_AuthenticateAcceptsATokenUntilItsExpiry(t *testing.T) {
	t.Parallel()

	s, _, clk := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")
	tokens := NewTokens(clk)

	token, _, err := CreateAPIToken(ctx, s, homeAssistant(user.ID))
	require.NoError(t, err)

	clk.Set(sessionsTestNow.Add(365 * day))
	_, _, err = tokens.Authenticate(ctx, s, token, netip.Addr{})
	require.NoError(t, err)

	clk.Advance(time.Millisecond)
	_, _, err = tokens.Authenticate(ctx, s, token, netip.Addr{})
	require.ErrorIs(t, err, ErrTokenExpired)
}
