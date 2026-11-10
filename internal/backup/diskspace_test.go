package backup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
)

// createSized creates the file at path with size bytes.
func createSized(t *testing.T, path string, size int64) {
	t.Helper()

	f, err := os.Create(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	require.NoError(t, f.Close())
}

// Given less free space in the backups directory than the size of the database and its WAL, then
// the backup aborts with the space needed and the space available, before writing anything.
func TestCreate_NotEnoughFreeSpace_Aborts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wal     int64
		free    uint64
		wantErr string
	}{
		{name: "with a WAL", wal: 1_000_000, free: 1_200_000,
			wantErr: "back up: not enough free space: the backup needs about 4.5 MB, 1.2 MB available in "},
		{name: "without a WAL", wal: -1, free: 999,
			wantErr: "back up: not enough free space: the backup needs about 3.5 MB, 999 B available in "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			cfg := f.config

			// The size of a database of the tests, without the files that SQLite holds open.
			cfg.DB = filepath.Join(t.TempDir(), "sized.db")
			createSized(t, cfg.DB, 3_500_000)

			if tt.wal >= 0 {
				createSized(t, cfg.DB+"-wal", tt.wal)
			}

			var asked string

			cfg.FreeSpace = func(dir string) (uint64, error) {
				asked = dir

				return tt.free, nil
			}

			path, err := backup.New(cfg, f.clock).Create(t.Context(), f.store, backup.ReasonManual)
			require.EqualError(t, err, tt.wantErr+cfg.Dir)
			require.Empty(t, path)

			require.Equal(t, cfg.Dir, asked)
			require.Empty(t, dirNames(t, cfg.Dir))
		})
	}
}

// Given exactly the estimated size free, then the backup runs.
func TestCreate_EstimatedSizeFree_Runs(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cfg := f.config

	cfg.DB = filepath.Join(t.TempDir(), "sized.db")
	createSized(t, cfg.DB, 3_500_000)
	cfg.FreeSpace = func(string) (uint64, error) { return 3_500_000, nil }

	_, err := backup.New(cfg, f.clock).Create(t.Context(), f.store, backup.ReasonManual)
	require.NoError(t, err)
}
