package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// preferencesTime is when the users of the suite are created.
var preferencesTime = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

// requireTimezone checks that the user of userID has the timezone want, empty for none.
func requireTimezone(ctx context.Context, t *testing.T, preferences store.PreferencesRepository, userID int64, want string) {
	t.Helper()

	got, err := preferences.Get(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, store.Preferences{Timezone: want}, got)
}

// RunPreferences runs the PreferencesRepository contract against the stores returned by newStore.
func RunPreferences(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a new user, then Get returns their preferences without a timezone.
	t.Run("GetOnANewUserReturnsNoTimezone", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(preferencesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))

		requireTimezone(ctx, t, s.Preferences(), user.ID, "")
	})

	// Given SetTimezone, then it returns the preferences as stored, Get returns them, and the other
	// users keep none. Given SetTimezone again, then the timezone is replaced.
	t.Run("SetTimezoneStoresTheTimezoneOfTheUser", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(preferencesTime))
		ctx := t.Context()
		alice := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		bob := mustCreateUser(ctx, t, s.Users(), newUser("bob", ""))

		set, err := s.Preferences().SetTimezone(ctx, alice.ID, "Europe/Paris")
		require.NoError(t, err)
		require.Equal(t, store.Preferences{Timezone: "Europe/Paris"}, set)

		requireTimezone(ctx, t, s.Preferences(), alice.ID, "Europe/Paris")
		requireTimezone(ctx, t, s.Preferences(), bob.ID, "")

		set, err = s.Preferences().SetTimezone(ctx, alice.ID, "America/New_York")
		require.NoError(t, err)
		require.Equal(t, store.Preferences{Timezone: "America/New_York"}, set)

		requireTimezone(ctx, t, s.Preferences(), alice.ID, "America/New_York")
	})

	// Given an empty timezone, then SetTimezone leaves the user without one.
	t.Run("EmptyTimezoneLeavesTheUserWithoutOne", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(preferencesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))

		_, err := s.Preferences().SetTimezone(ctx, user.ID, "Europe/Paris")
		require.NoError(t, err)

		set, err := s.Preferences().SetTimezone(ctx, user.ID, "")
		require.NoError(t, err)
		require.Equal(t, store.Preferences{}, set)

		requireTimezone(ctx, t, s.Preferences(), user.ID, "")
	})

	// Given a user id that no account has, then Get and SetTimezone return store.ErrNotFound.
	t.Run("MissingUserReturnsErrNotFound", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(preferencesTime))
		ctx := t.Context()

		_, err := s.Preferences().Get(ctx, 42)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = s.Preferences().SetTimezone(ctx, 42, "Europe/Paris")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given a timezone set through the repositories of WithTx, when the transaction rolls back, then
	// the user has none.
	t.Run("RolledBackTransactionLeavesNoTimezone", func(t *testing.T) {
		t.Parallel()

		s := newStore(t, clock.NewFake(preferencesTime))
		ctx := t.Context()
		user := mustCreateUser(ctx, t, s.Users(), newUser("alice", ""))
		errRollback := errors.New("roll back")

		err := s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			_, err := tx.Preferences().SetTimezone(ctx, user.ID, "Europe/Paris")
			require.NoError(t, err)
			requireTimezone(ctx, t, tx.Preferences(), user.ID, "Europe/Paris")

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		requireTimezone(ctx, t, s.Preferences(), user.ID, "")
	})
}
