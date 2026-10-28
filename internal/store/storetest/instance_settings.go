// Package storetest holds contract test suites that every store.Store implementation must pass,
// so that a second implementation behaves like the SQLite one.
package storetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// NewStoreFunc returns an empty, migrated store whose time comes from clk. The store is closed
// at the end of the test.
type NewStoreFunc func(t *testing.T, clk clock.Clock) store.Store

// settingsTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that
// UpdatedAt is truncated to the millisecond and returned in UTC.
var settingsTime = time.Date(2026, 10, 28, 10, 30, 15, 123_456_789, time.FixedZone("CET", 3600))

// RunInstanceSettings runs the InstanceSettingsRepository contract against the stores returned
// by newStore.
func RunInstanceSettings(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given Set(ctx, "map.mode", "none"), then Get(ctx, "map.mode") returns the value and
	// UpdatedAt equals the clock's now (millisecond precision, UTC).
	t.Run("GetReturnsTheValueSetAtTheClockNow", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(settingsTime)
		settings := newStore(t, clk).InstanceSettings()
		ctx := t.Context()

		require.NoError(t, settings.Set(ctx, "map.mode", "none"))

		got, err := settings.Get(ctx, "map.mode")
		require.NoError(t, err)
		require.Equal(t, store.Setting{
			Key:       "map.mode",
			Value:     "none",
			UpdatedAt: time.Date(2026, 10, 28, 9, 30, 15, 123_000_000, time.UTC),
		}, got)
	})

	// Given Set on an existing key, then the value and UpdatedAt are replaced.
	t.Run("SetOnExistingKeyReplacesValueAndUpdatedAt", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(settingsTime)
		settings := newStore(t, clk).InstanceSettings()
		ctx := t.Context()

		require.NoError(t, settings.Set(ctx, "map.mode", "none"))

		clk.Advance(90 * time.Minute)
		require.NoError(t, settings.Set(ctx, "map.mode", "pmtiles"))

		got, err := settings.Get(ctx, "map.mode")
		require.NoError(t, err)
		require.Equal(t, store.Setting{
			Key:       "map.mode",
			Value:     "pmtiles",
			UpdatedAt: time.Date(2026, 10, 28, 11, 0, 15, 123_000_000, time.UTC),
		}, got)

		all, err := settings.List(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
	})

	// Given Get or Delete on a missing key, then the error satisfies
	// errors.Is(err, store.ErrNotFound).
	t.Run("GetAndDeleteOnMissingKeyReturnErrNotFound", func(t *testing.T) {
		t.Parallel()

		settings := newStore(t, clock.NewFake(settingsTime)).InstanceSettings()
		ctx := t.Context()

		require.NoError(t, settings.Set(ctx, "map.mode", "none"))

		_, err := settings.Get(ctx, "missing")
		require.ErrorIs(t, err, store.ErrNotFound)

		err = settings.Delete(ctx, "missing")
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	// Given Delete on an existing key, then Get no longer finds it.
	t.Run("DeleteRemovesTheKey", func(t *testing.T) {
		t.Parallel()

		settings := newStore(t, clock.NewFake(settingsTime)).InstanceSettings()
		ctx := t.Context()

		require.NoError(t, settings.Set(ctx, "map.mode", "none"))
		require.NoError(t, settings.Set(ctx, "smtp.host", "mail.example.com"))

		require.NoError(t, settings.Delete(ctx, "map.mode"))

		_, err := settings.Get(ctx, "map.mode")
		require.ErrorIs(t, err, store.ErrNotFound)

		err = settings.Delete(ctx, "map.mode")
		require.ErrorIs(t, err, store.ErrNotFound)

		got, err := settings.Get(ctx, "smtp.host")
		require.NoError(t, err)
		require.Equal(t, "mail.example.com", got.Value)
	})

	// Given List, then settings are returned sorted by key.
	t.Run("ListReturnsSettingsSortedByKey", func(t *testing.T) {
		t.Parallel()

		clk := clock.NewFake(settingsTime)
		settings := newStore(t, clk).InstanceSettings()
		ctx := t.Context()

		empty, err := settings.List(ctx)
		require.NoError(t, err)
		require.Empty(t, empty)

		require.NoError(t, settings.Set(ctx, "smtp.host", "mail.example.com"))
		clk.Advance(time.Second)
		require.NoError(t, settings.Set(ctx, "map.mode", "none"))
		clk.Advance(time.Second)
		require.NoError(t, settings.Set(ctx, "auth.oidc.enabled", "true"))

		got, err := settings.List(ctx)
		require.NoError(t, err)
		require.Equal(t, []store.Setting{
			{Key: "auth.oidc.enabled", Value: "true", UpdatedAt: time.Date(2026, 10, 28, 9, 30, 17, 123_000_000, time.UTC)},
			{Key: "map.mode", Value: "none", UpdatedAt: time.Date(2026, 10, 28, 9, 30, 16, 123_000_000, time.UTC)},
			{Key: "smtp.host", Value: "mail.example.com", UpdatedAt: time.Date(2026, 10, 28, 9, 30, 15, 123_000_000, time.UTC)},
		}, got)
	})
}
