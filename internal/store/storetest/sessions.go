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

// sessionsTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of a session are truncated to the millisecond and returned in UTC.
var sessionsTime = time.Date(2026, 10, 28, 10, 30, 15, 123_456_789, time.FixedZone("CET", 3600))

// sessionsUTC is sessionsTime truncated to the millisecond, in UTC.
var sessionsUTC = time.Date(2026, 10, 28, 9, 30, 15, 123_000_000, time.UTC)

// idHash returns the hash of the session identifier id.
func idHash(id string) []byte {
	hash := sha256.Sum256([]byte(id))

	return hash[:]
}

// newSession returns a session of userID for the identifier id, which expires in 30 days.
func newSession(userID int64, id string) store.NewSession {
	return store.NewSession{
		IDHash:    idHash(id),
		PublicID:  "public-" + id,
		UserID:    userID,
		Method:    store.LoginPassword,
		ExpiresAt: sessionsTime.Add(30 * 24 * time.Hour),
		ClientIP:  "203.0.113.7",
		UserAgent: "Mozilla/5.0 (iPhone)",
	}
}

func mustCreateSession(ctx context.Context, t *testing.T, sessions store.SessionsRepository, session store.NewSession) store.Session {
	t.Helper()

	got, err := sessions.Create(ctx, session)
	require.NoError(t, err)

	return got
}

// requireSessionsRemoved checks that no session has any of the identifiers ids.
func requireSessionsRemoved(ctx context.Context, t *testing.T, sessions store.SessionsRepository, ids ...string) {
	t.Helper()

	for _, id := range ids {
		_, _, err := sessions.Get(ctx, idHash(id))
		require.ErrorIs(t, err, store.ErrNotFound, id)
	}
}

// requireSessionsKept checks that a session of each of the identifiers ids still exists.
func requireSessionsKept(ctx context.Context, t *testing.T, sessions store.SessionsRepository, ids ...string) {
	t.Helper()

	for _, id := range ids {
		_, _, err := sessions.Get(ctx, idHash(id))
		require.NoError(t, err, id)
	}
}

// RunSessions runs the SessionsRepository contract against the stores returned by newStore.
func RunSessions(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a created session, then Get by the hash of its identifier returns it, with the clock's
	// now (millisecond precision, UTC) as CreatedAt and LastSeenAt, and returns its user.
	t.Run("CreateStoresEveryFieldAndGetReturnsTheUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", "alice@example.com"))

		created := mustCreateSession(ctx, t, s.Sessions(), store.NewSession{
			IDHash:    idHash("secret"),
			PublicID:  "public",
			UserID:    user.ID,
			Method:    store.LoginPassword,
			ExpiresAt: time.Date(2026, 11, 27, 10, 30, 15, 987_654_321, time.FixedZone("CET", 3600)),
			ClientIP:  "203.0.113.7",
			UserAgent: "Mozilla/5.0 (iPhone)",
		})

		want := store.Session{
			PublicID:   "public",
			UserID:     user.ID,
			Method:     store.LoginPassword,
			CreatedAt:  sessionsUTC,
			LastSeenAt: sessionsUTC,
			ExpiresAt:  time.Date(2026, 11, 27, 9, 30, 15, 987_000_000, time.UTC),
			ClientIP:   "203.0.113.7",
			UserAgent:  "Mozilla/5.0 (iPhone)",
		}
		require.Equal(t, want, created)

		got, gotUser, err := s.Sessions().Get(ctx, idHash("secret"))
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, user, gotUser)
	})

	// Given a session whose client is unknown, then its client IP and user agent stay empty.
	t.Run("ClientIPAndUserAgentAreOptional", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))

		session := newSession(user.ID, "secret")
		session.ClientIP, session.UserAgent = "", ""
		mustCreateSession(ctx, t, s.Sessions(), session)

		got, _, err := s.Sessions().Get(ctx, idHash("secret"))
		require.NoError(t, err)
		require.Empty(t, got.ClientIP)
		require.Empty(t, got.UserAgent)
	})

	// Given a hash that no session has, then Get returns store.ErrNotFound.
	t.Run("GetOnUnknownHashReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		mustCreateSession(ctx, t, s.Sessions(), newSession(user.ID, "secret"))

		_, _, err := s.Sessions().Get(ctx, idHash("other"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given a session, then another session with its hash or its public id returns
	// store.ErrConflict, and is not stored.
	t.Run("TakenHashOrPublicIDConflicts", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		mustCreateSession(ctx, t, s.Sessions(), newSession(user.ID, "secret"))

		sameHash := newSession(user.ID, "secret")
		sameHash.PublicID = "another public id"

		samePublicID := newSession(user.ID, "other")
		samePublicID.PublicID = "public-secret"

		for name, session := range map[string]store.NewSession{"hash": sameHash, "public id": samePublicID} {
			_, err := s.Sessions().Create(ctx, session)
			require.ErrorIs(t, err, store.ErrConflict, name)
		}

		_, _, err := s.Sessions().Get(ctx, idHash("other"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given a session, when the clock moves and Touch runs, then LastSeenAt is the clock's now and
	// ExpiresAt the given time, and the rest is unchanged.
	t.Run("TouchSetsLastSeenAtAndExpiresAt", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(sessionsTime)
		s := newStore(t, clk)
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		created := mustCreateSession(ctx, t, s.Sessions(), newSession(user.ID, "secret"))

		clk.Advance(time.Hour)
		require.NoError(t, s.Sessions().Touch(ctx, idHash("secret"), sessionsTime.Add(90*24*time.Hour)))

		want := created
		want.LastSeenAt = sessionsUTC.Add(time.Hour)
		want.ExpiresAt = sessionsUTC.Add(90 * 24 * time.Hour)

		got, _, err := s.Sessions().Get(ctx, idHash("secret"))
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	// Given a hash that no session has, then Touch returns store.ErrNotFound.
	t.Run("TouchOnUnknownHashReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))

		err := s.Sessions().Touch(t.Context(), idHash("secret"), sessionsTime)
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given two sessions, when one is deleted, then Get no longer finds it, and the other stays.
	t.Run("DeleteRemovesOnlyTheSession", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		mustCreateSession(ctx, t, s.Sessions(), newSession(user.ID, "phone"))
		mustCreateSession(ctx, t, s.Sessions(), newSession(user.ID, "laptop"))

		require.NoError(t, s.Sessions().Delete(ctx, idHash("phone")))

		requireSessionsRemoved(ctx, t, s.Sessions(), "phone")
		requireSessionsKept(ctx, t, s.Sessions(), "laptop")
	})

	// Given a hash that no session has, then Delete returns store.ErrNotFound.
	t.Run("DeleteOnUnknownHashReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))

		err := s.Sessions().Delete(t.Context(), idHash("secret"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given sessions of two users, then DeleteOthers removes the sessions of one user except the
	// given one, and keeps the sessions of the other user.
	t.Run("DeleteOthersKeepsTheGivenSessionAndTheOtherUsers", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		alice := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))

		for _, id := range []string{"alice-phone", "alice-laptop", "alice-tablet"} {
			mustCreateSession(ctx, t, s.Sessions(), newSession(alice.ID, id))
		}

		mustCreateSession(ctx, t, s.Sessions(), newSession(bob.ID, "bob-phone"))

		require.NoError(t, s.Sessions().DeleteOthers(ctx, alice.ID, idHash("alice-phone")))

		requireSessionsRemoved(ctx, t, s.Sessions(), "alice-laptop", "alice-tablet")
		requireSessionsKept(ctx, t, s.Sessions(), "alice-phone", "bob-phone")
	})

	// Given sessions that expire before, at and after a time, then PurgeExpired at that time removes
	// only the session that expired before it.
	t.Run("PurgeExpiredRemovesOnlyTheSessionsExpiredBeforeNow", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		now := sessionsTime.Add(30 * 24 * time.Hour)

		for id, expiresAt := range map[string]time.Time{
			"before": now.Add(-time.Millisecond),
			"at":     now,
			"after":  now.Add(time.Millisecond),
		} {
			session := newSession(user.ID, id)
			session.ExpiresAt = expiresAt
			mustCreateSession(ctx, t, s.Sessions(), session)
		}

		require.NoError(t, s.Sessions().PurgeExpired(ctx, now))

		requireSessionsRemoved(ctx, t, s.Sessions(), "before")
		requireSessionsKept(ctx, t, s.Sessions(), "at", "after")
	})

	// Given a session created through the repositories of WithTx, when the transaction rolls back,
	// then no session exists.
	t.Run("RolledBackTransactionLeavesNoSession", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		errRollback := errors.New("roll back")

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			mustCreateSession(ctx, t, tx.Sessions(), newSession(user.ID, "secret"))
			requireSessionsKept(ctx, t, tx.Sessions(), "secret")

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		requireSessionsRemoved(ctx, t, s.Sessions(), "secret")
	})

	// Given a user id that no account has, then Create returns store.ErrNotFound and stores nothing.
	t.Run("CreateForAMissingUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(sessionsTime))
		ctx := t.Context()

		_, err := s.Sessions().Create(ctx, newSession(42, "secret"))
		require.ErrorIs(t, err, store.ErrNotFound)

		_, _, err = s.Sessions().Get(ctx, idHash("secret"))
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}
