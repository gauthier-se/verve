package preferences

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// newTestUser returns a store on a migrated database in a fresh temporary directory, and a user of
// it without preferences. The store is closed at the end of the test.
func newTestUser(t *testing.T) (store.Store, store.User) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clock.NewFake(time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)))
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	user, err := s.Users().Create(t.Context(), store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	return s, user
}

// Given a user without a timezone, then Location reports UTC as their effective zone.
func TestLocation_UserWithoutTimezoneIsInUTC(t *testing.T) {
	t.Parallel()

	s, user := newTestUser(t)

	loc, err := Location(t.Context(), s.Preferences(), user.ID)
	require.NoError(t, err)
	require.Equal(t, time.UTC, loc)
}

// Given a user who confirmed a timezone, then Location returns it: a time of the user falls on the
// local day and hour of that zone.
func TestLocation_ReturnsTheConfirmedTimezone(t *testing.T) {
	t.Parallel()

	s, user := newTestUser(t)

	_, err := SetTimezone(t.Context(), s.Preferences(), user.ID, "America/New_York")
	require.NoError(t, err)

	loc, err := Location(t.Context(), s.Preferences(), user.ID)
	require.NoError(t, err)
	require.Equal(t, "America/New_York", loc.String())

	local := time.Date(2026, 11, 6, 2, 30, 0, 0, time.UTC).In(loc)
	require.Equal(t, "2026-11-05 21:30 EST", local.Format("2006-01-02 15:04 MST"))
}

// Given a valid IANA name, then SetTimezone stores it and returns the preferences. Given a name
// that is not one, then ErrTimezoneInvalid, and the user keeps the timezone they had. The posix/ and
// right/ trees that many systems install beside the IANA names load from their database, but not
// from the embedded one, and right/ counts leap seconds.
func TestSetTimezone_StoresOnlyValidNames(t *testing.T) {
	t.Parallel()

	s, user := newTestUser(t)
	ctx := t.Context()

	for _, name := range []string{"Europe/Paris", "UTC", "Etc/GMT+5", "Asia/Kathmandu"} {
		got, err := SetTimezone(ctx, s.Preferences(), user.ID, name)
		require.NoError(t, err, name)
		require.Equal(t, store.Preferences{Timezone: name}, got)
	}

	for _, name := range []string{
		"", "Local", "Mars/Olympus_Mons", "Europe/../Europe/Paris", "/etc/localtime",
		"posix/Europe/Paris", "right/Europe/Paris",
	} {
		_, err := SetTimezone(ctx, s.Preferences(), user.ID, name)
		require.ErrorIs(t, err, ErrTimezoneInvalid, name)
	}

	got, err := s.Preferences().Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, store.Preferences{Timezone: "Asia/Kathmandu"}, got)
}
