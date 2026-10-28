package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// migratedDB opens the database of a new data directory and applies every migration.
func migratedDB(t *testing.T) *sqlite.DB {
	t.Helper()

	db := openDataDirDB(t, t.TempDir())

	_, err := sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	return db
}

// skipUnlessPermissionsEnforced skips tests that rely on a directory without write permission.
func skipUnlessPermissionsEnforced(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced on Windows")
	}

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// setWritable gives dir its default permission, or removes its write permission.
func setWritable(t *testing.T, dir string, writable bool) {
	t.Helper()

	if writable {
		require.NoError(t, os.Chmod(dir, 0o750)) //nolint:gosec // G302: the default permission of the data directory

		return
	}

	require.NoError(t, os.Chmod(dir, 0o500)) //nolint:gosec // G302: a directory needs the execute bit
}

// Given the database reachable on both pools, then the database check passes.
func TestDatabaseChecker_BothPoolsReachable(t *testing.T) {
	t.Parallel()

	c := newDatabaseChecker(migratedDB(t))

	require.Equal(t, "database", c.Name())
	require.NoError(t, c.Check(t.Context()))
}

// Given a database pool that is closed, then the database check fails and names the pool.
func TestDatabaseChecker_ClosedPoolFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pool func(*sqlite.DB) *sql.DB
		want string
	}{
		{name: "reader", pool: (*sqlite.DB).Read, want: "ping reader: sql: database is closed"},
		{name: "writer", pool: (*sqlite.DB).Write, want: "ping writer: sql: database is closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), dbFile))
			require.NoError(t, err)
			// Closing fails once a pool is closed: only the file descriptors matter here.
			t.Cleanup(func() { _ = db.Close(context.Background()) })

			require.NoError(t, tt.pool(db).Close())
			require.EqualError(t, newDatabaseChecker(db).Check(t.Context()), tt.want)
		})
	}
}

// Given every migration applied, then the migrations check passes.
func TestMigrationsChecker_AllAppliedPasses(t *testing.T) {
	t.Parallel()

	c := newMigrationsChecker(migratedDB(t))

	require.Equal(t, "migrations", c.Name())
	require.NoError(t, c.Check(t.Context()))
}

// Given a migration not applied, then the migrations check fails with the number of pending
// migrations.
func TestMigrationsChecker_PendingFails(t *testing.T) {
	t.Parallel()

	db := migratedDB(t)

	_, err := db.Write().ExecContext(t.Context(),
		"DELETE FROM goose_db_version WHERE version_id = ?", sqlite.LatestVersion())
	require.NoError(t, err)

	require.EqualError(t, newMigrationsChecker(db).Check(t.Context()), "1 pending")
}

// Given a database whose version is newer than this binary supports, then the migrations check fails.
func TestMigrationsChecker_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	setFutureSchemaVersion(t, dataDir)

	err := newMigrationsChecker(openDataDirDB(t, dataDir)).Check(t.Context())
	require.ErrorContains(t, err, "is newer than this binary supports")
}

// Given a writable data directory, then the data directory check passes and leaves no file behind.
func TestDataDirChecker_WritablePasses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := newDataDirChecker(dir, clock.Real())

	require.Equal(t, "data_dir", c.Name())
	require.NoError(t, c.Check(t.Context()))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// Given a data directory that is not writable, then the data directory check fails with
// `not writable` and the cause.
func TestDataDirChecker_NotWritableFails(t *testing.T) {
	t.Parallel()
	skipUnlessPermissionsEnforced(t)

	dir := t.TempDir()
	setWritable(t, dir, false)
	t.Cleanup(func() { setWritable(t, dir, true) })

	err := newDataDirChecker(dir, clock.Real()).Check(t.Context())
	require.EqualError(t, err, "not writable: permission denied")
}

// Given the data directory check, then it touches the disk at most once every 10 seconds and
// returns the cached result in between.
func TestDataDirChecker_CachesResultForTenSeconds(t *testing.T) {
	t.Parallel()
	skipUnlessPermissionsEnforced(t)

	dir := t.TempDir()
	t.Cleanup(func() { setWritable(t, dir, true) })

	clk := clock.NewFake(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	c := newDataDirChecker(dir, clk)

	require.NoError(t, c.Check(t.Context()))

	setWritable(t, dir, false)
	clk.Advance(10*time.Second - time.Millisecond)
	require.NoError(t, c.Check(t.Context()), "the result must be cached for 10 seconds")

	clk.Advance(time.Millisecond)
	require.EqualError(t, c.Check(t.Context()), "not writable: permission denied")

	setWritable(t, dir, true)
	clk.Advance(9 * time.Second)
	require.EqualError(t, c.Check(t.Context()), "not writable: permission denied",
		"a failure must be cached for 10 seconds too")

	clk.Advance(time.Second)
	require.NoError(t, c.Check(t.Context()))
}
