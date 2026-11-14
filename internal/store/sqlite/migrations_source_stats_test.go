package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// sourceStatsVersion is the migration that adds the sample counts of the sources.
const sourceStatsVersion = 25

// Given a database with samples of two sources of a user, a source without samples and a sample of
// another user, when the migration that adds source_stats runs, then it holds the number of samples
// of each source that has some.
func TestMigrate_SourceStats_FillsTheCountsFromTheStoredSamples(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	ctx := t.Context()

	mustMigrate(t, db, embeddedMigrationsBefore(t, sourceStatsVersion))

	for _, stmt := range []string{
		`INSERT INTO users (id, username, username_fold, role, status, created_at)
		 VALUES (1, 'camille', 'camille', 'admin', 'active', 0),
		        (2, 'dominique', 'dominique', 'member', 'active', 0)`,
		`INSERT INTO metric_types (id, key, identifier, kind, unit, aggregation, display_group, curated, created_at)
		 VALUES (1, 'steps', 'HKQuantityTypeIdentifierStepCount', 'quantity', 'count', 'cumulative', 'activity', 1, 0),
		        (2, 'heart_rate', 'HKQuantityTypeIdentifierHeartRate', 'quantity', 'count/min', 'discrete', 'heart', 1, 0)`,
		`INSERT INTO sources (id, user_id, name, device_model, display_name, bundle_id, device_name, manufacturer,
		 hardware_version, software_version, kind, kind_set_by_user, first_seen_at, last_seen_at)
		 VALUES (1, 1, 'Watch', 'Watch', 'Watch', '', '', '', '', '', 'watch', 0, 0, 0),
		        (2, 1, 'Phone', 'iPhone', 'Phone', '', '', '', '', '', 'phone', 0, 0, 0),
		        (3, 1, 'Scale', '', 'Scale', '', '', '', '', '', 'scale', 0, 0, 0),
		        (4, 2, 'Watch', 'Watch', 'Watch', '', '', '', '', '', 'watch', 0, 0, 0)`,
		`INSERT INTO samples (user_id, type_id, source_id, start_at, end_at, utc_offset, value, category, origin,
		 import_id, uuid, natural_key)
		 VALUES (1, 1, 1, 0, 60000, 0, 1, '', 'xml', 1, '', x'01'),
		        (1, 2, 1, 0, 0, 0, 60, '', 'xml', 1, '', x'02'),
		        (1, 1, 1, 60000, 120000, 0, 2, '', 'xml', 1, '', x'03'),
		        (1, 1, 2, 0, 60000, 0, 3, '', 'xml', 1, '', x'04'),
		        (2, 1, 4, 0, 60000, 0, 4, '', 'xml', 1, '', x'01')`,
	} {
		_, err := db.Write().ExecContext(ctx, stmt)
		require.NoError(t, err)
	}

	res, err := Migrate(ctx, db, MigrateOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, res.Applied)
	require.Equal(t, Migration{Version: sourceStatsVersion, Name: "source_stats"}, res.Applied[0])

	rows, err := db.Read().QueryContext(ctx, `SELECT user_id, source_id, count FROM source_stats ORDER BY user_id, source_id`)
	require.NoError(t, err)

	var counts [][3]int64

	for rows.Next() {
		var row [3]int64
		require.NoError(t, rows.Scan(&row[0], &row[1], &row[2]))
		counts = append(counts, row)
	}

	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, [][3]int64{{1, 1, 3}, {1, 2, 1}, {2, 4, 1}}, counts)
}
