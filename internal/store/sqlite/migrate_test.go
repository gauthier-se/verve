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

// testMigrations is a set of three valid migrations for the mechanism tests.
func testMigrations() fstest.MapFS {
	return fstest.MapFS{
		"00001_create_a.sql": {Data: []byte("-- +goose Up\nCREATE TABLE a (id INTEGER PRIMARY KEY) STRICT;\n")},
		"00002_create_b.sql": {Data: []byte("-- +goose Up\nCREATE TABLE b (id INTEGER PRIMARY KEY) STRICT;\n")},
		"00003_create_c.sql": {Data: []byte("-- +goose Up\nCREATE TABLE c (id INTEGER PRIMARY KEY) STRICT;\n")},
	}
}

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer

	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
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

	version, err := currentVersion(t.Context(), db, testMigrations())
	require.NoError(t, err)
	require.Equal(t, int64(3), version)

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

	_, err := migrate(t.Context(), db, testMigrations(), MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	logger, logs := bufferLogger()
	hookCalls := 0

	res, err := migrate(t.Context(), db, testMigrations(), MigrateOptions{
		Logger: logger,
		BeforeMigrate: func(context.Context, []int64) error {
			hookCalls++

			return nil
		},
	})
	require.NoError(t, err)

	require.Empty(t, res.Applied)
	require.Equal(t, int64(3), res.Version)
	require.Zero(t, hookCalls, "the hook runs only when migrations are pending")
	require.Contains(t, logs.String(), `msg="database schema up to date (version 3)"`)
	require.NotContains(t, logs.String(), "applied migration")
}

// Given a database whose version is higher than the latest embedded migration, then migrating
// fails with a message telling to upgrade Verve or restore a backup, and nothing runs.
func TestMigrate_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)

	_, err := migrate(t.Context(), db, testMigrations(), MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	older := testMigrations()
	delete(older, "00003_create_c.sql")

	hookCalls := 0

	_, err = migrate(t.Context(), db, older, MigrateOptions{
		Logger: slog.New(slog.DiscardHandler),
		BeforeMigrate: func(context.Context, []int64) error {
			hookCalls++

			return nil
		},
	})
	require.EqualError(t, err,
		"database schema version 3 is newer than this binary supports (2); upgrade Verve or restore a backup")
	require.Zero(t, hookCalls)

	_, err = migrationStatus(t.Context(), db, older)
	require.EqualError(t, err,
		"database schema version 3 is newer than this binary supports (2); upgrade Verve or restore a backup")
}

// Given a migration that fails, then its changes are rolled back, the schema version is
// unchanged and the error names the migration file.
func TestMigrate_FailingMigrationRollsBack(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	fsys := testMigrations()
	delete(fsys, "00003_create_c.sql")

	_, err := migrate(t.Context(), db, fsys, MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	fsys["00003_broken.sql"] = &fstest.MapFile{Data: []byte(
		"-- +goose Up\nCREATE TABLE half_done (id INTEGER PRIMARY KEY) STRICT;\nINSERT INTO no_such_table VALUES (1);\n",
	)}

	_, err = migrate(t.Context(), db, fsys, MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "00003_broken.sql")

	require.False(t, tableExists(t, db, "half_done"), "the failed migration must be rolled back")

	version, err := currentVersion(t.Context(), db, fsys)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
}

// Given pending migrations, then the BeforeMigrate hook runs first with the pending versions;
// when it returns an error, no migration runs and the error is returned.
func TestMigrate_BeforeHookErrorPreventsMigration(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	fsys := testMigrations()
	delete(fsys, "00003_create_c.sql")

	_, err := migrate(t.Context(), db, fsys, MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	fsys = testMigrations()
	errBackup := errors.New("backup failed")

	var pending []int64

	_, err = migrate(t.Context(), db, fsys, MigrateOptions{
		Logger: slog.New(slog.DiscardHandler),
		BeforeMigrate: func(_ context.Context, versions []int64) error {
			pending = versions

			return errBackup
		},
	})
	require.ErrorIs(t, err, errBackup)
	require.Equal(t, []int64{3}, pending)
	require.False(t, tableExists(t, db, "c"))

	version, err := currentVersion(t.Context(), db, fsys)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
}

// Given a partly migrated database, then the status lists each migration with its applied time
// or as pending.
func TestMigrationStatus_ListsAppliedAndPending(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	fsys := testMigrations()
	delete(fsys, "00003_create_c.sql")

	_, err := migrate(t.Context(), db, fsys, MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

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

	res, err := Migrate(ctx, db, MigrateOptions{Logger: slog.New(slog.DiscardHandler)})
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
