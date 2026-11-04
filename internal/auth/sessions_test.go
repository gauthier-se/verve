package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

const day = 24 * time.Hour

// sessionsTestNow is when the sessions of the tests are created.
var sessionsTestNow = time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)

// newTestSessions returns a store on a migrated database in a fresh temporary directory, the
// sessions service, and the fake clock of both, at sessionsTestNow. The store is closed at the end
// of the test.
func newTestSessions(t *testing.T) (store.Store, Sessions, *clock.Fake) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	clk := clock.NewFake(sessionsTestNow)
	s := sqlite.New(db, clk)

	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s, NewSessions(clk), clk
}

// mustCreateUser stores an active member named username.
func mustCreateUser(ctx context.Context, t *testing.T, s store.Store, username string) store.User {
	t.Helper()

	user, err := s.Users().Create(ctx, store.NewUser{Username: username, Role: store.RoleMember})
	require.NoError(t, err)

	return user
}

// phoneLogin is a password login of userID from a phone.
func phoneLogin(userID int64) NewSession {
	return NewSession{
		UserID:    userID,
		Method:    store.LoginPassword,
		ClientIP:  "203.0.113.7",
		UserAgent: "Mozilla/5.0 (iPhone)",
	}
}

// mustCreateSession creates a phone session of userID and returns its identifier.
func mustCreateSession(ctx context.Context, t *testing.T, sessions Sessions, s store.Store, userID int64) string {
	t.Helper()

	id, _, err := sessions.Create(ctx, s, phoneLogin(userID))
	require.NoError(t, err)

	return id
}

// storedSession returns the session that the store holds under the SHA-256 hash of id.
func storedSession(ctx context.Context, t *testing.T, s store.Store, id string) store.Session {
	t.Helper()

	hash := sha256.Sum256([]byte(id))

	session, _, err := s.Sessions().Get(ctx, hash[:])
	require.NoError(t, err)

	return session
}

// requireNoSession checks that a lookup failed with store.ErrNotFound alone, so that callers cannot
// tell an unknown, expired or revoked session apart.
func requireNoSession(t *testing.T, err error) {
	t.Helper()

	require.ErrorIs(t, err, store.ErrNotFound)
	require.EqualError(t, err, store.ErrNotFound.Error())
}

// Given a new session, then Create returns a random 256-bit identifier once, and the store holds
// the session under the SHA-256 hash of the identifier, with its user id, method, times, client IP
// and user agent. It expires in 30 days.
func TestSessions_CreateReturnsTheIdentifierAndStoresOnlyItsHash(t *testing.T) {
	t.Parallel()

	s, sessions, _ := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")

	id, created, err := sessions.Create(ctx, s, phoneLogin(user.ID))
	require.NoError(t, err)

	decoded, err := base64.RawURLEncoding.DecodeString(id)
	require.NoError(t, err)
	require.Len(t, decoded, 32)

	want := store.Session{
		PublicID:   created.PublicID,
		UserID:     user.ID,
		Method:     store.LoginPassword,
		CreatedAt:  sessionsTestNow,
		LastSeenAt: sessionsTestNow,
		ExpiresAt:  sessionsTestNow.Add(30 * day),
		ClientIP:   "203.0.113.7",
		UserAgent:  "Mozilla/5.0 (iPhone)",
	}
	require.Equal(t, want, created)
	require.Equal(t, want, storedSession(ctx, t, s, id))
}

// Given two sessions, then their identifiers and their public ids differ, and a public id holds
// nothing of its identifier.
func TestSessions_CreateGivesEachSessionRandomIdentifiers(t *testing.T) {
	t.Parallel()

	s, sessions, _ := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")

	firstID, first, err := sessions.Create(ctx, s, phoneLogin(user.ID))
	require.NoError(t, err)

	secondID, second, err := sessions.Create(ctx, s, phoneLogin(user.ID))
	require.NoError(t, err)

	require.NotEqual(t, firstID, secondID)
	require.NotEqual(t, first.PublicID, second.PublicID)

	for _, session := range []struct{ id, publicID string }{{firstID, first.PublicID}, {secondID, second.PublicID}} {
		require.NotEmpty(t, session.publicID)
		require.NotContains(t, session.id, session.publicID)
		require.NotContains(t, session.publicID, session.id)
	}
}

// Given the identifier of a valid session, then Lookup returns the session and its user.
func TestSessions_LookupReturnsTheSessionAndItsUser(t *testing.T) {
	t.Parallel()

	s, sessions, _ := newTestSessions(t)
	ctx := t.Context()
	mustCreateUser(ctx, t, s, "bob")
	user := mustCreateUser(ctx, t, s, "alice")

	id, created, err := sessions.Create(ctx, s, phoneLogin(user.ID))
	require.NoError(t, err)

	session, gotUser, err := sessions.Lookup(ctx, s, id)
	require.NoError(t, err)
	require.Equal(t, created, session)
	require.Equal(t, user, gotUser)
}

// Given an identifier that no session has, then Lookup returns store.ErrNotFound.
func TestSessions_LookupOfAnUnknownIdentifierFails(t *testing.T) {
	t.Parallel()

	s, sessions, _ := newTestSessions(t)
	ctx := t.Context()
	mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

	for _, id := range []string{"unknown", ""} {
		_, _, err := sessions.Lookup(ctx, s, id)
		requireNoSession(t, err)
	}
}

// Given a session without activity, then Lookup finds it while it was idle for up to 30 days, and
// fails once it was idle for more.
func TestSessions_LookupFailsAfter30IdleDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		idle  time.Duration
		found bool
	}{
		{name: "IdleFor30Days", idle: 30 * day, found: true},
		{name: "IdleForMoreThan30Days", idle: 30*day + time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, sessions, clk := newTestSessions(t)
			ctx := t.Context()
			id := mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

			clk.Advance(tt.idle)

			_, _, err := sessions.Lookup(ctx, s, id)
			if !tt.found {
				requireNoSession(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

// Given activity on a session, then Lookup writes its last seen time and its expiry, 30 days from
// then, at most once per minute.
func TestSessions_LookupRenewsTheSessionAtMostOncePerMinute(t *testing.T) {
	t.Parallel()

	s, sessions, clk := newTestSessions(t)
	ctx := t.Context()
	id := mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

	steps := []struct {
		advance  time.Duration
		lastSeen time.Time
	}{
		{advance: time.Minute - time.Millisecond, lastSeen: sessionsTestNow},
		{advance: time.Millisecond, lastSeen: sessionsTestNow.Add(time.Minute)},
		{advance: 30 * time.Second, lastSeen: sessionsTestNow.Add(time.Minute)},
		{advance: 30 * time.Second, lastSeen: sessionsTestNow.Add(2 * time.Minute)},
	}

	for i, step := range steps {
		clk.Advance(step.advance)

		_, _, err := sessions.Lookup(ctx, s, id)
		require.NoError(t, err, "step %d", i)

		stored := storedSession(ctx, t, s, id)
		require.Equal(t, step.lastSeen, stored.LastSeenAt, "step %d", i)
		require.Equal(t, step.lastSeen.Add(30*day), stored.ExpiresAt, "step %d", i)
	}
}

// Given a session used every day, then its expiry never passes 180 days after its creation, and
// Lookup finds it until it is 180 days old, then fails.
func TestSessions_LookupFailsOnceAnActiveSessionIsOlderThan180Days(t *testing.T) {
	t.Parallel()

	s, sessions, clk := newTestSessions(t)
	ctx := t.Context()
	id := mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

	// The expiry after the lookup of some of the days.
	expiries := map[int]time.Time{
		1:   sessionsTestNow.Add(31 * day),
		149: sessionsTestNow.Add(179 * day),
		150: sessionsTestNow.Add(180 * day),
		151: sessionsTestNow.Add(180 * day),
		180: sessionsTestNow.Add(180 * day),
	}

	for age := 1; age <= 180; age++ {
		clk.Advance(day)

		_, _, err := sessions.Lookup(ctx, s, id)
		require.NoError(t, err, "day %d", age)

		if expiry, ok := expiries[age]; ok {
			require.Equal(t, expiry, storedSession(ctx, t, s, id).ExpiresAt, "day %d", age)
		}
	}

	clk.Advance(time.Millisecond)

	_, _, err := sessions.Lookup(ctx, s, id)
	requireNoSession(t, err)
}

// Given a revoked session, then the next lookup fails, and the other sessions of its user stay
// valid.
func TestSessions_LookupOfARevokedSessionFails(t *testing.T) {
	t.Parallel()

	s, sessions, _ := newTestSessions(t)
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s, "alice")
	phone := mustCreateSession(ctx, t, sessions, s, user.ID)
	laptop := mustCreateSession(ctx, t, sessions, s, user.ID)

	require.NoError(t, sessions.Revoke(ctx, s, phone))

	_, _, err := sessions.Lookup(ctx, s, phone)
	requireNoSession(t, err)

	_, _, err = sessions.Lookup(ctx, s, laptop)
	require.NoError(t, err)
}

// withUserStatus wraps the repositories of a real store, so that the sessions they return have a
// user of status. No repository changes the status of a user yet.
type withUserStatus struct {
	store.Repositories

	status store.UserStatus
}

func (r withUserStatus) Sessions() store.SessionsRepository {
	return sessionsWithUserStatus{SessionsRepository: r.Repositories.Sessions(), status: r.status}
}

type sessionsWithUserStatus struct {
	store.SessionsRepository

	status store.UserStatus
}

func (r sessionsWithUserStatus) Get(ctx context.Context, idHash []byte) (store.Session, store.User, error) {
	session, user, err := r.SessionsRepository.Get(ctx, idHash)
	user.Status = r.status

	return session, user, err
}

// Given a user who is not active, then the lookup of their session fails.
func TestSessions_LookupFailsForAUserWhoIsNotActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status store.UserStatus
		found  bool
	}{
		{status: store.UserActive, found: true},
		{status: store.UserDisabled},
		{status: store.UserDeleting},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			s, sessions, _ := newTestSessions(t)
			ctx := t.Context()
			id := mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

			_, user, err := sessions.Lookup(ctx, withUserStatus{Repositories: s, status: tt.status}, id)
			if !tt.found {
				requireNoSession(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, store.UserActive, user.Status)
		})
	}
}

// setLifetimes stores the instance settings session.idle_days and session.max_days.
func setLifetimes(ctx context.Context, t *testing.T, s store.Store, idleDays, maxDays string) {
	t.Helper()

	require.NoError(t, s.InstanceSettings().Set(ctx, "session.idle_days", idleDays))
	require.NoError(t, s.InstanceSettings().Set(ctx, "session.max_days", maxDays))
}

// Given the instance settings session.idle_days and session.max_days, then they replace the
// defaults: a session idle for more than idle_days fails, and an active session expires max_days
// after its creation.
func TestSessions_InstanceSettingsReplaceTheDefaultLifetimes(t *testing.T) {
	t.Parallel()

	t.Run("IdleDays", func(t *testing.T) {
		t.Parallel()

		s, sessions, clk := newTestSessions(t)
		ctx := t.Context()
		setLifetimes(ctx, t, s, "7", "10")

		id, created, err := sessions.Create(ctx, s, phoneLogin(mustCreateUser(ctx, t, s, "alice").ID))
		require.NoError(t, err)
		require.Equal(t, sessionsTestNow.Add(7*day), created.ExpiresAt)

		clk.Advance(7*day + time.Millisecond)

		_, _, err = sessions.Lookup(ctx, s, id)
		requireNoSession(t, err)
	})

	t.Run("MaxDays", func(t *testing.T) {
		t.Parallel()

		s, sessions, clk := newTestSessions(t)
		ctx := t.Context()
		setLifetimes(ctx, t, s, "7", "10")
		id := mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

		clk.Advance(4 * day)

		_, _, err := sessions.Lookup(ctx, s, id)
		require.NoError(t, err)
		require.Equal(t, sessionsTestNow.Add(10*day), storedSession(ctx, t, s, id).ExpiresAt)

		clk.Advance(6 * day)

		_, _, err = sessions.Lookup(ctx, s, id)
		require.NoError(t, err)

		clk.Advance(time.Millisecond)

		_, _, err = sessions.Lookup(ctx, s, id)
		requireNoSession(t, err)
	})
}

// Given an instance setting that is not a whole number of days from 1 to 36500, then the default
// replaces it: 30 days idle and 180 days at most.
func TestSessions_InvalidInstanceSettingsFallBackToTheDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "Empty", value: ""},
		{name: "Zero", value: "0"},
		{name: "Negative", value: "-1"},
		{name: "MoreThan100Years", value: "36501"},
		{name: "Word", value: "seven"},
		{name: "Fraction", value: "7.5"},
		{name: "Space", value: " 7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			value := tt.value

			s, sessions, _ := newTestSessions(t)
			ctx := t.Context()
			user := mustCreateUser(ctx, t, s, "alice")

			// A valid max_days of 36500 lets an invalid idle_days show through the expiry, and a valid
			// idle_days of 36500 lets an invalid max_days show through it.
			setLifetimes(ctx, t, s, value, "36500")

			_, idle, err := sessions.Create(ctx, s, phoneLogin(user.ID))
			require.NoError(t, err)
			require.Equal(t, sessionsTestNow.Add(30*day), idle.ExpiresAt)

			setLifetimes(ctx, t, s, "36500", value)

			_, maximum, err := sessions.Create(ctx, s, phoneLogin(user.ID))
			require.NoError(t, err)
			require.Equal(t, sessionsTestNow.Add(180*day), maximum.ExpiresAt)
		})
	}
}

// Given a session older than a newly lowered session.max_days, then its next renewal fails the
// lookup, instead of writing an expiry that has passed.
func TestSessions_LoweredMaxDaysEndsAnOlderSessionAtItsNextRenewal(t *testing.T) {
	t.Parallel()

	s, sessions, clk := newTestSessions(t)
	ctx := t.Context()
	id := mustCreateSession(ctx, t, sessions, s, mustCreateUser(ctx, t, s, "alice").ID)

	clk.Advance(20 * day)

	_, _, err := sessions.Lookup(ctx, s, id)
	require.NoError(t, err)

	setLifetimes(ctx, t, s, "30", "10")
	clk.Advance(time.Minute)

	_, _, err = sessions.Lookup(ctx, s, id)
	requireNoSession(t, err)
	require.Equal(t, sessionsTestNow.Add(50*day), storedSession(ctx, t, s, id).ExpiresAt)
}
