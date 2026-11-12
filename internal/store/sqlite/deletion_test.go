package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

var deletionTestNow = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// userRowSeeders store one row of a user in each table of the schema with a user_id column. A table
// without a seeder fails TestDeletion_RemovesEveryRowOfTheUserFromEveryTable, so that a new table
// cannot be forgotten by account deletion.
var userRowSeeders = map[string]func(ctx context.Context, t *testing.T, s store.Store, user store.User){
	"sessions": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		_, err := s.Sessions().Create(ctx, store.NewSession{
			IDHash:    []byte("session of " + user.Username),
			PublicID:  "session-" + user.Username,
			UserID:    user.ID,
			Method:    store.LoginPassword,
			ExpiresAt: deletionTestNow.Add(time.Hour),
		})
		require.NoError(t, err)
	},
	"user_preferences": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		_, err := s.Preferences().SetTimezone(ctx, user.ID, "Europe/Paris")
		require.NoError(t, err)
	},
	"invitations": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		_, err := s.Invitations().Create(ctx, store.NewInvitation{
			Kind:      store.InvitationReset,
			TokenHash: []byte("reset link of " + user.Username),
			UserID:    user.ID,
			ExpiresAt: deletionTestNow.Add(time.Hour),
			CreatedBy: user.ID,
		})
		require.NoError(t, err)
	},
	"api_tokens": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		_, err := s.APITokens().Create(ctx, store.NewAPIToken{
			UserID:    user.ID,
			Name:      "sync",
			Scopes:    []store.TokenScope{store.ScopeIngest},
			Prefix:    "vrv_" + user.Username,
			TokenHash: []byte("token of " + user.Username),
		})
		require.NoError(t, err)
	},
	// The alias resolves the source of the seeder of sources, so that each table holds one row.
	"source_aliases": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		source := seedSource(ctx, t, s, user)
		require.NoError(t, s.Sources().AddAlias(ctx, user.ID, store.SourceIdentity{Name: "Old watch", DeviceModel: "Watch"}, source.ID))
	},
	"sources": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		seedSource(ctx, t, s, user)
	},
	// The data availability is that of the sample of the seeder of samples.
	"sample_stats": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		seedSample(ctx, t, s, user)
	},
	"samples": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		seedSample(ctx, t, s, user)
	},
	"workouts": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		_, err := s.Workouts().Upsert(ctx, user.ID, store.NewWorkout{
			Workout: store.Workout{
				SourceID:     seedSource(ctx, t, s, user).ID,
				Origin:       store.OriginXML,
				Activity:     "running",
				Start:        deletionTestNow,
				End:          deletionTestNow.Add(30 * time.Minute),
				Duration:     1800,
				LocationType: store.LocationOutdoor,
				NaturalKey:   []byte("run of " + user.Username),
			},
			OnConflict: store.ConflictIgnore,
		})
		require.NoError(t, err)
	},
	"jobs": func(ctx context.Context, t *testing.T, s store.Store, user store.User) {
		t.Helper()

		_, err := s.Jobs().Enqueue(ctx, store.NewJob{Kind: "test.job", Payload: json.RawMessage(`{}`), UserID: user.ID})
		require.NoError(t, err)
	},
}

// seedSource resolves the watch source of user, the same source on every call.
func seedSource(ctx context.Context, t *testing.T, s store.Store, user store.User) store.Source {
	t.Helper()

	source, err := s.Sources().Resolve(ctx, user.ID, store.SourceInfo{Name: "Watch", DeviceModel: "Watch"},
		store.SourceWatch, deletionTestNow)
	require.NoError(t, err)

	return source
}

// seedSample stores a heart rate sample of the watch source of user, the same sample on every call.
func seedSample(ctx context.Context, t *testing.T, s store.Store, user store.User) {
	t.Helper()

	heart, err := s.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierHeartRate", catalog.KindQuantity, "count/min")
	require.NoError(t, err)

	_, err = s.Samples().Upsert(ctx, user.ID, []store.NewSample{{
		Sample: store.Sample{
			TypeID:     heart.ID,
			SourceID:   seedSource(ctx, t, s, user).ID,
			Start:      deletionTestNow,
			End:        deletionTestNow,
			Value:      60,
			Origin:     store.OriginXML,
			NaturalKey: []byte("heart rate of " + user.Username),
		},
		OnConflict: store.ConflictIgnore,
	}})
	require.NoError(t, err)
}

// tablesWithUserID returns the tables of the schema of db that have a user_id column.
func tablesWithUserID(ctx context.Context, t *testing.T, db *DB) []string {
	t.Helper()

	rows, err := db.Read().QueryContext(ctx, `
		SELECT m.name
		FROM sqlite_master AS m
		JOIN pragma_table_info(m.name) AS c
		WHERE m.type = 'table' AND c.name = 'user_id'
		ORDER BY m.name`)
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	var tables []string

	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))

		tables = append(tables, name)
	}

	require.NoError(t, rows.Err())

	return tables
}

// countRowsOf returns the number of rows of table whose user_id is userID.
func countRowsOf(ctx context.Context, t *testing.T, db *DB, table string, userID int64) int64 {
	t.Helper()

	var count int64

	// The table name comes from sqlite_master, never from an input.
	query := fmt.Sprintf(`SELECT count(*) FROM %q WHERE user_id = ?`, table) //nolint:gosec // G201: see above
	require.NoError(t, db.Read().QueryRowContext(ctx, query, userID).Scan(&count))

	return count
}

// Given every table of the schema with a user_id column, each holding a row of two users, when the
// account of one of them is deleted, then no table holds a row of that user, and the other user
// keeps every row. A table that this test does not seed fails it.
func TestDeletion_RemovesEveryRowOfTheUserFromEveryTable(t *testing.T) {
	t.Parallel()

	s, db := newTestStore(t, clock.NewFake(deletionTestNow))
	ctx := t.Context()

	tables := tablesWithUserID(ctx, t, db)
	require.NotEmpty(t, tables)

	alice, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	bob, err := s.Users().Create(ctx, store.NewUser{Username: "bob", Role: store.RoleMember})
	require.NoError(t, err)

	for _, table := range tables {
		seed, ok := userRowSeeders[table]
		require.True(t, ok, "table %s has a user_id column: seed it in userRowSeeders, and make account deletion remove its rows", table)

		seed(ctx, t, s, alice)
		seed(ctx, t, s, bob)
	}

	_, err = s.Users().SetStatus(ctx, bob.ID, store.UserDeleting)
	require.NoError(t, err)

	require.NoError(t, auth.DeleteAccount(ctx, s, bob.ID))

	for _, table := range tables {
		require.Zero(t, countRowsOf(ctx, t, db, table, bob.ID), "rows of the deleted user in %s", table)
		require.Equal(t, int64(1), countRowsOf(ctx, t, db, table, alice.ID), "rows of the other user in %s", table)
	}

	_, err = s.Users().Get(ctx, bob.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = s.Users().Get(ctx, alice.ID)
	require.NoError(t, err)
}
