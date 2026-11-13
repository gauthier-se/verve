package sqlite

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// sourcesManualKindVersion is the migration that adds the manual kind of a source.
const sourcesManualKindVersion = 20

// embeddedMigrationsBefore returns the embedded migrations older than version.
func embeddedMigrationsBefore(t *testing.T, version int64) fstest.MapFS {
	t.Helper()

	names, err := fs.Glob(migrationsFS(), "*.sql")
	require.NoError(t, err)

	fsys := fstest.MapFS{}

	for _, name := range names {
		v, err := goose.NumericComponent(name)
		require.NoError(t, err)

		if v >= version {
			continue
		}

		data, err := fs.ReadFile(migrationsFS(), name)
		require.NoError(t, err)

		fsys[name] = &fstest.MapFile{Data: data}
	}

	return fsys
}

// Given a database with sources, an alias, a sample and a workout, when the migration that adds the
// manual kind runs, then every row and id is kept, a source accepts the manual kind, ids of deleted
// sources are not reused, and foreign keys are enforced and consistent again.
func TestMigrate_SourcesManualKind_KeepsSourcesAndTheirData(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	ctx := t.Context()

	mustMigrate(t, db, embeddedMigrationsBefore(t, sourcesManualKindVersion))

	for _, stmt := range []string{
		`INSERT INTO users (id, username, username_fold, role, status, created_at)
		 VALUES (1, 'camille', 'camille', 'admin', 'active', 0)`,
		`INSERT INTO metric_types (id, key, identifier, kind, unit, aggregation, display_group, curated, created_at)
		 VALUES (1, 'steps', 'HKQuantityTypeIdentifierStepCount', 'quantity', 'count', 'cumulative', 'activity', 1, 0)`,
		`INSERT INTO sources (id, user_id, name, device_model, display_name, bundle_id, device_name, manufacturer,
		 hardware_version, software_version, kind, kind_set_by_user, first_seen_at, last_seen_at)
		 VALUES (1, 1, 'Watch', 'Watch', 'Watch', '', '', '', '', '', 'watch', 0, 0, 0),
		        (2, 1, 'Phone', 'iPhone', 'Phone', '', '', '', '', '', 'phone', 1, 0, 0),
		        (3, 1, 'Deleted', '', 'Deleted', '', '', '', '', '', 'unknown', 0, 0, 0)`,
		`DELETE FROM sources WHERE id = 3`,
		`INSERT INTO source_aliases (user_id, name, device_model, source_id) VALUES (1, 'Old watch', 'Watch', 1)`,
		`INSERT INTO samples (id, user_id, type_id, source_id, start_at, end_at, utc_offset, value, category,
		 origin, import_id, uuid, natural_key)
		 VALUES (1, 1, 1, 1, 0, 60000, 3600, 42, '', 'xml', 1, '', x'01')`,
		`INSERT INTO workouts (id, user_id, source_id, origin, external_id, activity, start_at, end_at, utc_offset,
		 duration, location_type, events, import_id, natural_key)
		 VALUES (1, 1, 2, 'xml', '', 'running', 0, 60000, 3600, 60, 'outdoor', '[]', 1, x'02')`,
	} {
		_, err := db.Write().ExecContext(ctx, stmt)
		require.NoError(t, err)
	}

	res, err := Migrate(ctx, db, MigrateOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, res.Applied)
	require.Equal(t, Migration{Version: sourcesManualKindVersion, Name: "sources_manual_kind"}, res.Applied[0],
		"the migration runs first, before the newer ones")

	type sourceRow struct {
		id            int64
		name          string
		kind          string
		kindSetByUser bool
	}

	rows, err := db.Read().QueryContext(ctx, `SELECT id, name, kind, kind_set_by_user FROM sources ORDER BY id`)
	require.NoError(t, err)

	var sources []sourceRow

	for rows.Next() {
		var r sourceRow
		require.NoError(t, rows.Scan(&r.id, &r.name, &r.kind, &r.kindSetByUser))
		sources = append(sources, r)
	}

	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, []sourceRow{
		{id: 1, name: "Watch", kind: "watch"},
		{id: 2, name: "Phone", kind: "phone", kindSetByUser: true},
	}, sources)

	for table, want := range map[string]int{"source_aliases": 1, "samples": 1, "workouts": 1} {
		var n int
		require.NoError(t, db.Read().QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		require.Equal(t, want, n, table)
	}

	var id int64
	require.NoError(t, db.Write().QueryRowContext(ctx,
		`INSERT INTO sources (user_id, name, device_model, display_name, bundle_id, device_name, manufacturer,
		 hardware_version, software_version, kind, kind_set_by_user, first_seen_at, last_seen_at)
		 VALUES (1, 'Health', 'iPhone', 'Health', 'com.apple.Health', '', '', '', '', 'manual', 0, 0, 0)
		 RETURNING id`).Scan(&id))
	require.Equal(t, int64(4), id, "the id of the deleted source is not reused")

	var foreignKeys int
	require.NoError(t, db.Write().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
	require.Equal(t, 1, foreignKeys)

	violations, err := db.Write().QueryContext(ctx, "PRAGMA foreign_key_check")
	require.NoError(t, err)
	require.False(t, violations.Next(), "no foreign key violation")
	require.NoError(t, violations.Err())
	require.NoError(t, violations.Close())

	_, err = db.Write().ExecContext(ctx, `DELETE FROM sources WHERE id = 1`)
	require.NoError(t, err)

	for _, table := range []string{"source_aliases", "samples"} {
		var n int
		require.NoError(t, db.Read().QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		require.Zero(t, n, "deleting a source still deletes its %s", table)
	}
}
