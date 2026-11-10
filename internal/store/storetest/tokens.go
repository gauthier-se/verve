package storetest

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// tokensTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of a token are truncated to the millisecond and returned in UTC.
var tokensTime = time.Date(2026, 11, 10, 10, 30, 15, 123_456_789, time.FixedZone("CET", 3600))

// tokensUTC is tokensTime truncated to the millisecond, in UTC.
var tokensUTC = time.Date(2026, 11, 10, 9, 30, 15, 123_000_000, time.UTC)

// apiTokenHash returns the hash of the API token token.
func apiTokenHash(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}

// newAPIToken returns an ingest token of userID named name, for the token token, without expiry.
func newAPIToken(userID int64, name, token string) store.NewAPIToken {
	return store.NewAPIToken{
		UserID:    userID,
		Name:      name,
		Scopes:    []store.TokenScope{store.ScopeIngest},
		Prefix:    token[:min(len(token), 8)],
		TokenHash: apiTokenHash(token),
	}
}

func mustCreateAPIToken(ctx context.Context, t *testing.T, tokens store.APITokensRepository, token store.NewAPIToken) store.APIToken {
	t.Helper()

	got, err := tokens.Create(ctx, token)
	require.NoError(t, err)

	return got
}

// RunTokens runs the APITokensRepository contract against the stores returned by newStore.
func RunTokens(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a created token, then it holds its name, scopes, prefix and expiry (millisecond
	// precision, UTC), the clock's now as CreatedAt, and no use or revocation yet, and GetByHash
	// returns it with its user.
	t.Run("CreateStoresEveryFieldAndGetByHashReturnsTheUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", "alice@example.com"))

		created := mustCreateAPIToken(ctx, t, s.APITokens(), store.NewAPIToken{
			UserID:    user.ID,
			Name:      "Health Auto Export",
			Scopes:    []store.TokenScope{store.ScopeIngest, store.ScopeRead},
			Prefix:    "vrv_3fK9",
			TokenHash: apiTokenHash("vrv_3fK9secret"),
			ExpiresAt: time.Date(2027, 11, 10, 10, 30, 15, 987_654_321, time.FixedZone("CET", 3600)),
		})

		require.NotZero(t, created.ID)

		want := store.APIToken{
			ID:        created.ID,
			UserID:    user.ID,
			Name:      "Health Auto Export",
			Scopes:    []store.TokenScope{store.ScopeIngest, store.ScopeRead},
			Prefix:    "vrv_3fK9",
			ExpiresAt: time.Date(2027, 11, 10, 9, 30, 15, 987_000_000, time.UTC),
			CreatedAt: tokensUTC,
		}
		require.Equal(t, want, created)

		got, gotUser, err := s.APITokens().GetByHash(ctx, apiTokenHash("vrv_3fK9secret"))
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, user, gotUser)
	})

	// Given a token without expiry, then its ExpiresAt stays zero.
	t.Run("ExpiryIsOptional", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))

		created := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(user.ID, "sync", "vrv_secret"))
		require.True(t, created.ExpiresAt.IsZero())

		got, _, err := s.APITokens().GetByHash(ctx, apiTokenHash("vrv_secret"))
		require.NoError(t, err)
		require.True(t, got.ExpiresAt.IsZero())
	})

	// Given a hash that no token has, then GetByHash returns store.ErrNotFound.
	t.Run("GetByHashOnUnknownHashReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(user.ID, "sync", "vrv_secret"))

		_, _, err := s.APITokens().GetByHash(ctx, apiTokenHash("vrv_other"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given a revoked token, an expired token and a token of a disabled user, then GetByHash
	// returns each of them with its user, so that callers can tell the cases apart.
	t.Run("GetByHashReturnsRevokedAndExpiredTokensWithTheUserStatus", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(tokensTime)
		s := newStore(t, clk)
		ctx := t.Context()
		alice := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))

		revoked := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(alice.ID, "revoked", "vrv_revoked"))

		expiring := newAPIToken(alice.ID, "expired", "vrv_expired")
		expiring.ExpiresAt = tokensTime.Add(time.Hour)
		mustCreateAPIToken(ctx, t, s.APITokens(), expiring)

		mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(bob.ID, "disabled", "vrv_disabled"))

		clk.Advance(2 * time.Hour)

		_, err := s.APITokens().Revoke(ctx, alice.ID, revoked.ID)
		require.NoError(t, err)

		bob, err = s.Users().SetStatus(ctx, bob.ID, store.UserDisabled)
		require.NoError(t, err)

		got, user, err := s.APITokens().GetByHash(ctx, apiTokenHash("vrv_revoked"))
		require.NoError(t, err)
		require.Equal(t, tokensUTC.Add(2*time.Hour), got.RevokedAt)
		require.Equal(t, alice, user)

		got, user, err = s.APITokens().GetByHash(ctx, apiTokenHash("vrv_expired"))
		require.NoError(t, err)
		require.Equal(t, tokensUTC.Add(time.Hour), got.ExpiresAt)
		require.True(t, got.RevokedAt.IsZero())
		require.Equal(t, alice, user)

		got, user, err = s.APITokens().GetByHash(ctx, apiTokenHash("vrv_disabled"))
		require.NoError(t, err)
		require.True(t, got.RevokedAt.IsZero())
		require.Equal(t, store.UserDisabled, user.Status)
		require.Equal(t, bob, user)
	})

	// Given a token, then another token with its hash returns store.ErrConflict, and is not
	// stored.
	t.Run("TakenHashConflicts", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(user.ID, "first", "vrv_secret"))

		_, err := s.APITokens().Create(ctx, newAPIToken(user.ID, "second", "vrv_secret"))
		require.ErrorIs(t, err, store.ErrConflict)

		list, err := s.APITokens().ListByUser(ctx, user.ID)
		require.NoError(t, err)
		require.Len(t, list, 1)
		require.Equal(t, "first", list[0].Name)
	})

	// Given a token without a name, without scopes, with an unknown scope or with a repeated
	// scope, then Create returns store.ErrInvalid and stores nothing.
	t.Run("CreateRefusesAnEmptyNameAndInvalidScopes", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))

		for name, change := range map[string]func(token *store.NewAPIToken){
			"empty name":     func(token *store.NewAPIToken) { token.Name = "" },
			"no scopes":      func(token *store.NewAPIToken) { token.Scopes = nil },
			"unknown scope":  func(token *store.NewAPIToken) { token.Scopes = []store.TokenScope{"admin"} },
			"repeated scope": func(token *store.NewAPIToken) { token.Scopes = []store.TokenScope{store.ScopeRead, store.ScopeRead} },
		} {
			token := newAPIToken(user.ID, "sync", "vrv_secret")
			change(&token)

			_, err := s.APITokens().Create(ctx, token)
			require.ErrorIs(t, err, store.ErrInvalid, name)
		}

		list, err := s.APITokens().ListByUser(ctx, user.ID)
		require.NoError(t, err)
		require.Empty(t, list)
	})

	// Given a user id that no account has, then Create returns store.ErrNotFound and stores
	// nothing.
	t.Run("CreateForAMissingUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()

		_, err := s.APITokens().Create(ctx, newAPIToken(42, "sync", "vrv_secret"))
		require.ErrorIs(t, err, store.ErrNotFound)

		_, _, err = s.APITokens().GetByHash(ctx, apiTokenHash("vrv_secret"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given tokens of two users, created at different times, then ListByUser returns the tokens of
	// one user, revoked ones included, newest first, and an empty list for a user without tokens.
	t.Run("ListByUserReturnsOnlyTheTokensOfTheUserNewestFirst", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(tokensTime)
		s := newStore(t, clk)
		ctx := t.Context()
		alice := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))
		carol := mustCreateUser(ctx, t, s.Users(), newUser("carol", ""))

		export := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(alice.ID, "export", "vrv_export"))
		mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(bob.ID, "bob", "vrv_bob"))

		clk.Advance(time.Minute)
		sensor := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(alice.ID, "sensor", "vrv_sensor"))
		// Created at the same time as sensor: the id breaks the tie.
		script := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(alice.ID, "script", "vrv_script"))

		clk.Advance(time.Minute)
		export, err := s.APITokens().Revoke(ctx, alice.ID, export.ID)
		require.NoError(t, err)

		got, err := s.APITokens().ListByUser(ctx, alice.ID)
		require.NoError(t, err)
		require.Equal(t, []store.APIToken{script, sensor, export}, got)

		got, err = s.APITokens().ListByUser(ctx, carol.ID)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	// Given a token, when the clock moves and Revoke runs, then the token stays, with the clock's
	// now as RevokedAt and the rest unchanged, and a second Revoke keeps the first RevokedAt.
	t.Run("RevokeKeepsTheTokenAsRevoked", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(tokensTime)
		s := newStore(t, clk)
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		created := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(user.ID, "sync", "vrv_secret"))

		clk.Advance(time.Hour)
		revoked, err := s.APITokens().Revoke(ctx, user.ID, created.ID)
		require.NoError(t, err)

		want := created
		want.RevokedAt = tokensUTC.Add(time.Hour)
		require.Equal(t, want, revoked)

		clk.Advance(time.Hour)
		again, err := s.APITokens().Revoke(ctx, user.ID, created.ID)
		require.NoError(t, err)
		require.Equal(t, want, again)

		got, _, err := s.APITokens().GetByHash(ctx, apiTokenHash("vrv_secret"))
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// Given tokens of two users, then Revoke returns store.ErrNotFound for the token of another user
	// and for an unknown id, and leaves the token of the other user unrevoked.
	t.Run("RevokeOfAnotherUsersTokenReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		alice := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))
		mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(alice.ID, "alice", "vrv_alice"))
		bobToken := mustCreateAPIToken(ctx, t, s.APITokens(), newAPIToken(bob.ID, "bob", "vrv_bob"))

		_, err := s.APITokens().Revoke(ctx, alice.ID, bobToken.ID)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.APITokens().Revoke(ctx, alice.ID, bobToken.ID+100)
		require.ErrorIs(t, err, store.ErrNotFound)

		got, _, err := s.APITokens().GetByHash(ctx, apiTokenHash("vrv_bob"))
		require.NoError(t, err)
		require.Equal(t, bobToken, got)
	})

	// Given a token created through the repositories of WithTx, when the transaction rolls back,
	// then no token exists.
	t.Run("RolledBackTransactionLeavesNoToken", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(tokensTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		errRollback := errors.New("roll back")

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustCreateAPIToken(ctx, t, tx.APITokens(), newAPIToken(user.ID, "sync", "vrv_secret"))

			_, _, err := tx.APITokens().GetByHash(ctx, apiTokenHash("vrv_secret"))
			require.NoError(t, err)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		_, _, err = s.APITokens().GetByHash(ctx, apiTokenHash("vrv_secret"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}
