package sqlite

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const newerDatabaseError = "database schema version 3 is newer than this binary supports (2); " +
	"upgrade Verve or restore a backup"

// testMigrations is a set of three valid migrations for the mechanism tests.
func testMigrations() fstest.MapFS {
	return fstest.MapFS{
		"00001_create_a.sql": {Data: []byte("-- +goose Up\nCREATE TABLE a (id INTEGER PRIMARY KEY) STRICT;\n")},
		"00002_create_b.sql": {Data: []byte("-- +goose Up\nCREATE TABLE b (id INTEGER PRIMARY KEY) STRICT;\n")},
		"00003_create_c.sql": {Data: []byte("-- +goose Up\nCREATE TABLE c (id INTEGER PRIMARY KEY) STRICT;\n")},
	}
}

// firstTwoMigrations is testMigrations without its third migration.
func firstTwoMigrations() fstest.MapFS {
	fsys := testMigrations()
	delete(fsys, "00003_create_c.sql")

	return fsys
}

// brokenMigration creates a table, then fails on its second statement.
func brokenMigration() *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(
		"-- +goose Up\nCREATE TABLE half_done (id INTEGER PRIMARY KEY) STRICT;\nINSERT INTO no_such_table VALUES (1);\n",
	)}
}

// mustMigrate applies the migrations of fsys and fails the test on error.
func mustMigrate(t *testing.T, db *DB, fsys fstest.MapFS) {
	t.Helper()

	_, err := migrate(t.Context(), db, fsys, MigrateOptions{})
	require.NoError(t, err)
}

func requireVersion(t *testing.T, db *DB, want int64) {
	t.Helper()

	version, err := currentVersion(t.Context(), db, testMigrations())
	require.NoError(t, err)
	require.Equal(t, want, version)
}

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer

	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

// countingHook is a BeforeMigrate hook that records its calls and returns err.
type countingHook struct {
	calls   int
	pending []int64
	err     error
}

func (h *countingHook) run(_ context.Context, pending []int64) error {
	h.calls++
	h.pending = pending

	return h.err
}

func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()

	var n int
	require.NoError(t, db.Read().QueryRowContext(t.Context(),
		"SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", name).Scan(&n))

	return n == 1
}

// Given a fresh database, when migrations run, then all of them are applied in order and each
// applied migration is logged with its version and name.
func TestMigrate_FreshDatabaseAppliesAll(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	logger, logs := bufferLogger()

	res, err := migrate(t.Context(), db, testMigrations(), MigrateOptions{Logger: logger})
	require.NoError(t, err)

	require.Equal(t, int64(3), res.Version)
	require.Equal(t, []Migration{
		{Version: 1, Name: "create_a"},
		{Version: 2, Name: "create_b"},
		{Version: 3, Name: "create_c"},
	}, res.Applied)

	for _, table := range []string{"a", "b", "c"} {
		require.True(t, tableExists(t, db, table), table)
	}

	requireVersion(t, db, 3)

	out := logs.String()
	for _, line := range []string{
		`msg="applied migration" version=1 name=create_a`,
		`msg="applied migration" version=2 name=create_b`,
		`msg="applied migration" version=3 name=create_c`,
	} {
		require.Contains(t, out, line)
	}

	require.Less(t, strings.Index(out, "name=create_a"), strings.Index(out, "name=create_b"))
	require.Less(t, strings.Index(out, "name=create_b"), strings.Index(out, "name=create_c"))
}

// Given an up-to-date database, when migrations run again, then nothing is applied and
// `database schema up to date (version N)` is logged.
func TestMigrate_IsIdempotent(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	mustMigrate(t, db, testMigrations())

	logger, logs := bufferLogger()

	var hook countingHook

	res, err := migrate(t.Context(), db, testMigrations(), MigrateOptions{Logger: logger, BeforeMigrate: hook.run})
	require.NoError(t, err)

	require.Empty(t, res.Applied)
	require.Equal(t, int64(3), res.Version)
	require.Zero(t, hook.calls, "the hook runs only when migrations are pending")
	require.Contains(t, logs.String(), `msg="database schema up to date (version 3)"`)
	require.NotContains(t, logs.String(), "applied migration")
}

// Given a database whose version is higher than the latest embedded migration, then migrating
// fails with a message telling to upgrade Verve or restore a backup, and nothing runs.
func TestMigrate_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	mustMigrate(t, db, testMigrations())

	var hook countingHook

	_, err := migrate(t.Context(), db, firstTwoMigrations(), MigrateOptions{BeforeMigrate: hook.run})
	require.EqualError(t, err, newerDatabaseError)
	require.Zero(t, hook.calls)

	_, err = migrationStatus(t.Context(), db, firstTwoMigrations())
	require.EqualError(t, err, newerDatabaseError)
}

// Given a migration that fails, then its changes are rolled back, the schema version is
// unchanged and the error names the migration file.
func TestMigrate_FailingMigrationRollsBack(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	fsys := firstTwoMigrations()
	mustMigrate(t, db, fsys)

	fsys["00003_broken.sql"] = brokenMigration()

	_, err := migrate(t.Context(), db, fsys, MigrateOptions{})
	require.ErrorContains(t, err, "00003_broken.sql")

	require.False(t, tableExists(t, db, "half_done"), "the failed migration must be rolled back")
	requireVersion(t, db, 2)
}

// Given several pending migrations of which a later one fails, then the earlier ones stay
// applied and are logged and returned.
func TestMigrate_FailureKeepsEarlierMigrationsOfTheRun(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	fsys := firstTwoMigrations()
	fsys["00003_broken.sql"] = brokenMigration()
	logger, logs := bufferLogger()

	res, err := migrate(t.Context(), db, fsys, MigrateOptions{Logger: logger})
	require.ErrorContains(t, err, "00003_broken.sql")

	require.Equal(t, []Migration{{Version: 1, Name: "create_a"}, {Version: 2, Name: "create_b"}}, res.Applied)
	require.Contains(t, logs.String(), `msg="applied migration" version=2 name=create_b`)
	requireVersion(t, db, 2)
}

// Given pending migrations, then the BeforeMigrate hook runs first with the pending versions;
// when it returns an error, no migration runs and the error is returned.
func TestMigrate_BeforeHookErrorPreventsMigration(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	mustMigrate(t, db, firstTwoMigrations())

	hook := countingHook{err: errors.New("backup failed")}

	_, err := migrate(t.Context(), db, testMigrations(), MigrateOptions{BeforeMigrate: hook.run})
	require.ErrorIs(t, err, hook.err)
	require.Equal(t, []int64{3}, hook.pending)
	require.False(t, tableExists(t, db, "c"))
	requireVersion(t, db, 2)
}

// Given a partly migrated database, then the status lists each migration with its applied time
// or as pending.
func TestMigrationStatus_ListsAppliedAndPending(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	mustMigrate(t, db, firstTwoMigrations())

	states, err := migrationStatus(t.Context(), db, testMigrations())
	require.NoError(t, err)
	require.Len(t, states, 3)

	for i, want := range []Migration{{1, "create_a"}, {2, "create_b"}, {3, "create_c"}} {
		require.Equal(t, want, states[i].Migration)
	}

	require.False(t, states[0].AppliedAt.IsZero())
	require.False(t, states[1].AppliedAt.IsZero())
	require.True(t, states[2].AppliedAt.IsZero(), "a pending migration has no applied time")
}

var migrationFileName = regexp.MustCompile(`^\d{5}_[a-z0-9]+(_[a-z0-9]+)*\.sql$`)

// Given the embedded migrations, then they apply cleanly to a fresh database, are numbered
// sequentially and the first one creates instance_settings.
func TestMigrate_EmbeddedMigrationsApplyCleanly(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	ctx := t.Context()

	entries, err := embeddedMigrations.ReadDir("migrations")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		require.Regexp(t, migrationFileName, entry.Name())
	}

	res, err := Migrate(ctx, db, MigrateOptions{})
	require.NoError(t, err)
	require.Len(t, res.Applied, len(entries))
	require.Equal(t, Migration{Version: 1, Name: "instance_settings"}, res.Applied[0])

	for i, m := range res.Applied {
		require.Equal(t, int64(i+1), m.Version, "migrations are numbered sequentially")
	}

	require.Equal(t, LatestVersion(), res.Version)

	version, err := CurrentVersion(ctx, db)
	require.NoError(t, err)
	require.Equal(t, LatestVersion(), version)

	states, err := MigrationStatus(ctx, db)
	require.NoError(t, err)
	require.Len(t, states, len(entries))

	type column struct {
		name    string
		typ     string
		notNull bool
		pk      bool
	}

	rows, err := db.Read().QueryContext(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info('instance_settings')`)
	require.NoError(t, err)

	var columns []column

	for rows.Next() {
		var c column
		require.NoError(t, rows.Scan(&c.name, &c.typ, &c.notNull, &c.pk))
		columns = append(columns, c)
	}

	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	require.Equal(t, []column{
		{name: "key", typ: "TEXT", notNull: true, pk: true},
		{name: "value", typ: "TEXT", notNull: true},
		{name: "updated_at", typ: "INTEGER", notNull: true},
	}, columns)

	var strict int
	require.NoError(t, db.Read().QueryRowContext(ctx,
		"SELECT strict FROM pragma_table_list WHERE name = 'instance_settings'").Scan(&strict))
	require.Equal(t, 1, strict)
}
