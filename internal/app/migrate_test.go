package app

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// openDataDirDB opens the database of dataDir and closes it at the end of the test.
func openDataDirDB(t *testing.T, dataDir string) *sqlite.DB {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(dataDir, dbFile))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close(context.Background())) })

	return db
}

// schemaVersion returns the schema version of the database of dataDir.
func schemaVersion(t *testing.T, dataDir string) int64 {
	t.Helper()

	version, err := sqlite.CurrentVersion(t.Context(), openDataDirDB(t, dataDir))
	require.NoError(t, err)

	return version
}

// setFutureSchemaVersion migrates the database of dataDir and records a version newer than the
// latest embedded migration, as a newer Verve would.
func setFutureSchemaVersion(t *testing.T, dataDir string) {
	t.Helper()

	db := openDataDirDB(t, dataDir)

	_, err := sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(),
		"INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", sqlite.LatestVersion()+1)
	require.NoError(t, err)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Given `verve migrate` on an empty data directory, then it creates the database and applies
// every embedded migration.
func TestMigrate_AppliesEmbeddedMigrations(t *testing.T) {
	t.Parallel()

	cfg := testConfig(filepath.Join(t.TempDir(), "data"))

	res, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)
	require.NotEmpty(t, res.Applied)
	require.Equal(t, sqlite.LatestVersion(), res.Version)

	states, err := MigrationStatus(t.Context(), cfg)
	require.NoError(t, err)
	require.Len(t, states, len(res.Applied))

	for _, s := range states {
		require.False(t, s.AppliedAt.IsZero(), s.Name)
	}
}

// Given a running `verve serve` holding the data directory lock, then migrate and migrate status
// fail with a clear error.
func TestMigrate_DataDirLockedFails(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	lock, err := AcquireLock(cfg.DataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	want := "another Verve process is using " + cfg.DataDir + ": stop verve serve before running migrations"

	_, err = Migrate(t.Context(), cfg, discardLogger())
	require.EqualError(t, err, want)

	_, err = MigrationStatus(t.Context(), cfg)
	require.EqualError(t, err, want)
}

// Given a database whose version is newer than this binary supports, then migrate fails.
func TestMigrate_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	setFutureSchemaVersion(t, cfg.DataDir)

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.ErrorContains(t, err, "is newer than this binary supports")
}
