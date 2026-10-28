package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/storetest"
)

// newTestStore returns a store on a migrated database in a fresh temporary directory. The store
// is closed at the end of the test.
func newTestStore(t *testing.T, clk clock.Clock) store.Store {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = Migrate(t.Context(), db, MigrateOptions{})
	require.NoError(t, err)

	s := New(db, clk)
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s
}

// Given the contract suite storetest.RunInstanceSettings, then the SQLite implementation passes it.
func TestStore_InstanceSettingsContract(t *testing.T) {
	t.Parallel()

	storetest.RunInstanceSettings(t, func(t *testing.T, clk clock.Clock) store.Store {
		t.Helper()

		return newTestStore(t, clk)
	})
}
