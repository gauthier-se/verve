package app

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// archiveNames returns the names of the files of the archive at path.
func archiveNames(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	defer func() { require.NoError(t, f.Close()) }()

	zr, err := zstd.NewReader(f)
	require.NoError(t, err)

	defer zr.Close()

	var names []string

	tr := tar.NewReader(zr)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}

		require.NoError(t, err)

		names = append(names, header.Name)
	}
}

// Given the secret key file of the data directory, then the backup holds it. Given the key set
// through VERVE_SECRET_KEY, then it holds only the database, even when an old key file remains.
func TestBackup_HoldsTheKeyFileUnlessTheKeyIsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured string
		want       []string
	}{
		{name: "key file", want: []string{"verve.db", "secret.key"}},
		{name: "key configured", configured: "0123456789abcdef0123456789abcdef", want: []string{"verve.db"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig(t.TempDir())

			_, err := Migrate(t.Context(), cfg, discardLogger())
			require.NoError(t, err)

			_, err = ensureSecretKey(newLayout(cfg.DataDir).SecretKey, "")
			require.NoError(t, err)

			cfg.SecretKey = tt.configured

			path, err := Backup(t.Context(), cfg)
			require.NoError(t, err)
			require.Equal(t, tt.want, archiveNames(t, path))
		})
	}
}

// archivedDB opens the database of the archive at path.
func archivedDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()

	f, err := os.Open(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	defer func() { require.NoError(t, f.Close()) }()

	zr, err := zstd.NewReader(f)
	require.NoError(t, err)

	defer zr.Close()

	tr := tar.NewReader(zr)

	for {
		header, err := tr.Next()
		require.NoError(t, err, "the archive must hold verve.db")

		if header.Name != dbFile {
			continue
		}

		content, err := io.ReadAll(tr)
		require.NoError(t, err)

		dbPath := filepath.Join(t.TempDir(), dbFile)
		require.NoError(t, os.WriteFile(dbPath, content, 0o600))

		db, err := sqlite.Open(t.Context(), dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close(context.Background())) })

		return db
	}
}
