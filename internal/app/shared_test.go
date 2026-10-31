package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// setOlderSchemaVersion migrates the database of dataDir and forgets the latest migration, as if
// an older Verve had migrated it.
func setOlderSchemaVersion(t *testing.T, dataDir string) {
	t.Helper()

	db := openDataDirDB(t, dataDir)

	_, err := sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(),
		"DELETE FROM goose_db_version WHERE version_id = ?", sqlite.LatestVersion())
	require.NoError(t, err)
}

// setAndGet writes a setting through s and reads it back.
func setAndGet(ctx context.Context, s store.Store) (string, error) {
	if err := s.InstanceSettings().Set(ctx, "shared.key", "shared value"); err != nil {
		return "", err
	}

	setting, err := s.InstanceSettings().Get(ctx, "shared.key")
	if err != nil {
		return "", err
	}

	return setting.Value, nil
}

// Given serve holds the data directory lock, when a command opens the database in shared mode,
// then it reads and writes through a store and leaves the lock with serve.
func TestWithSharedDB_LockHeld_ReadsAndWritesThroughTheStore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	lock, err := AcquireLock(cfg.DataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	value, err := withSharedDB(ctx, cfg, func(s store.Store) (string, error) {
		return setAndGet(ctx, s)
	})
	require.NoError(t, err)
	require.Equal(t, "shared value", value)

	_, err = AcquireLock(cfg.DataDir)
	require.EqualError(t, err, "another Verve process is using "+cfg.DataDir)
}

// Given a running verve serve, when a command writes in shared mode, then the write lands and the
// server still stops cleanly.
func TestWithSharedDB_NextToRunningServer_WriteLands(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(filepath.Join(t.TempDir(), "data"))
	_, _, stop := startRun(t, cfg)

	value, err := withSharedDB(ctx, cfg, func(s store.Store) (string, error) {
		return setAndGet(ctx, s)
	})
	require.NoError(t, err)
	require.Equal(t, "shared value", value)

	stop()
}

// Given no server is running, then shared mode works the same, without taking the lock.
func TestWithSharedDB_NoLock_WorksTheSame(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	value, err := withSharedDB(ctx, cfg, func(s store.Store) (string, error) {
		lock, err := AcquireLock(cfg.DataDir)
		if err != nil {
			return "", err
		}

		if err := lock.Release(); err != nil {
			return "", err
		}

		return setAndGet(ctx, s)
	})
	require.NoError(t, err)
	require.Equal(t, "shared value", value)
}

// Given a schema older than the binary, then shared mode fails with the versions and what to do,
// does not call fn and applies nothing.
func TestWithSharedDB_OlderSchema_FailsAndAppliesNothing(t *testing.T) {
	t.Parallel()

	latest := sqlite.LatestVersion()

	tests := []struct {
		name    string
		setup   func(t *testing.T, dataDir string)
		current int64
	}{
		{name: "migrated by an older binary", setup: setOlderSchemaVersion, current: latest - 1},
		{name: "never migrated", setup: func(*testing.T, string) {}, current: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig(t.TempDir())
			tt.setup(t, cfg.DataDir)

			called := false

			_, err := withSharedDB(t.Context(), cfg, func(store.Store) (struct{}, error) {
				called = true

				return struct{}{}, nil
			})
			require.EqualError(t, err, fmt.Sprintf(
				"database schema is at version %d, this binary needs %d: start verve serve or run verve migrate",
				tt.current, latest))
			require.False(t, called)
			require.Equal(t, tt.current, schemaVersion(t, cfg.DataDir))
		})
	}
}

// Given a schema newer than the binary, then shared mode fails with the refusal of migrate and
// does not call fn.
func TestWithSharedDB_NewerSchema_FailsWithTheExistingRefusal(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	setFutureSchemaVersion(t, cfg.DataDir)

	called := false

	_, err := withSharedDB(t.Context(), cfg, func(store.Store) (struct{}, error) {
		called = true

		return struct{}{}, nil
	})

	_, migrateErr := Migrate(t.Context(), cfg, discardLogger())
	require.ErrorContains(t, migrateErr, "is newer than this binary supports")
	require.EqualError(t, err, migrateErr.Error())
	require.False(t, called)
}

// Given fn fails, then shared mode returns its error.
func TestWithSharedDB_FnFails_ReturnsItsError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := testConfig(t.TempDir())

	_, err := Migrate(ctx, cfg, discardLogger())
	require.NoError(t, err)

	errFn := errors.New("fn failed")

	_, err = withSharedDB(ctx, cfg, func(store.Store) (struct{}, error) {
		return struct{}{}, errFn
	})
	require.ErrorIs(t, err, errFn)
}
