package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// tarEntry is a file of an archive built by hand.
type tarEntry struct {
	name     string
	typeflag byte
	content  []byte
}

// regular returns the regular file name of an archive, holding content.
func regular(name string, content []byte) tarEntry {
	return tarEntry{name: name, typeflag: tar.TypeReg, content: content}
}

// buildArchive returns a zstd-compressed tar archive of entries, as Create writes them.
func buildArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw, err := zstd.NewWriter(&buf)
	require.NoError(t, err)

	tw := tar.NewWriter(zw)

	for _, e := range entries {
		header := &tar.Header{
			Typeflag: e.typeflag,
			Name:     e.name,
			Mode:     0o600,
			Size:     int64(len(e.content)),
			ModTime:  time.Date(2026, 11, 10, 14, 3, 5, 0, time.UTC),
			Format:   tar.FormatPAX,
		}

		if e.typeflag != tar.TypeReg {
			header.Size = 0
			header.Linkname = "/etc/passwd"
		}

		require.NoError(t, tw.WriteHeader(header))

		if e.typeflag == tar.TypeReg {
			_, err := tw.Write(e.content)
			require.NoError(t, err)
		}
	}

	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())

	return buf.Bytes()
}

// writeFile writes data to a new file of a temporary directory and returns its path.
func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

// migratedDB returns the content of a migrated database.
func migratedDB(t *testing.T) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "verve.db")

	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)
	require.NoError(t, db.Close(t.Context()))

	content, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	return content
}

// corruptDB returns the content of a database that fails PRAGMA integrity_check: its index is
// declared on another column than the one it holds.
func corruptDB(t *testing.T) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "verve.db")

	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(), `
		CREATE TABLE t (a INTEGER, b INTEGER);
		CREATE INDEX t_a ON t (a);
		INSERT INTO t VALUES (1, 2), (3, 4);
		PRAGMA writable_schema = ON;
		UPDATE sqlite_schema SET sql = 'CREATE INDEX t_a ON t (b)' WHERE name = 't_a';
		PRAGMA writable_schema = OFF;`)
	require.NoError(t, err)
	require.NoError(t, db.Close(t.Context()))

	content, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	return content
}

// Given a valid archive, when it is extracted, then the database and the secret key are written to
// the directory with the permissions 0600, and the database passed the check.
func TestExtract_ValidArchive_WritesTheDatabaseAndTheSecretKey(t *testing.T) {
	t.Parallel()

	db := migratedDB(t)
	archive := writeFile(t, "backup.tar.zst", buildArchive(t, regular("verve.db", db), regular("secret.key", []byte(secretKey))))
	dir := t.TempDir()

	var checked string

	extracted, err := backup.Extract(t.Context(), archive, dir, func(ctx context.Context, path string) error {
		checked = path

		return sqlite.CheckRestorable(ctx, path)
	})
	require.NoError(t, err)

	require.Equal(t, backup.Extracted{DB: filepath.Join(dir, "verve.db"), SecretKey: filepath.Join(dir, "secret.key")}, extracted)
	require.Equal(t, extracted.DB, checked)

	for path, want := range map[string][]byte{extracted.DB: nil, extracted.SecretKey: []byte(secretKey)} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), path)

		if want != nil {
			content, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
			require.NoError(t, err)
			require.Equal(t, want, content)
		}
	}
}

// Given an archive without secret.key, when it is extracted, then only the database is written.
func TestExtract_WithoutSecretKey_WritesOnlyTheDatabase(t *testing.T) {
	t.Parallel()

	archive := writeFile(t, "backup.tar.zst", buildArchive(t, regular("verve.db", migratedDB(t))))
	dir := t.TempDir()

	extracted, err := backup.Extract(t.Context(), archive, dir, sqlite.CheckRestorable)
	require.NoError(t, err)
	require.Equal(t, backup.Extracted{DB: filepath.Join(dir, "verve.db")}, extracted)
	require.Equal(t, []string{"verve.db"}, dirEntries(t, dir))
}

// dirEntries returns the names of the entries of dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// Given a corrupted archive, a truncated file, a database failing PRAGMA integrity_check, or an
// entry with another name or a path, when it is extracted, then it is refused, and nothing is
// written outside the directory.
func TestExtract_InvalidArchive_IsRefused(t *testing.T) {
	t.Parallel()

	db := migratedDB(t)
	key := []byte(secretKey)
	valid := buildArchive(t, regular("verve.db", db), regular("secret.key", key))

	notTar, err := zstd.NewWriter(nil)
	require.NoError(t, err)

	tests := []struct {
		name    string
		archive []byte
		err     string
	}{
		{name: "not zstd", archive: []byte("this is not an archive"), err: "read archive"},
		{name: "zstd but not tar", archive: notTar.EncodeAll(bytes.Repeat([]byte("not a tar header "), 64), nil), err: "read archive"},
		{name: "truncated in the database", archive: valid[:len(valid)/2], err: "read archive"},
		{name: "truncated at the end", archive: valid[:len(valid)-4], err: "read archive"},
		{name: "corrupted checksum", archive: flipLastByte(valid), err: "read archive"},
		{name: "data after the archive", archive: append(bytes.Clone(valid), "trailing data"...), err: "read archive"},
		{name: "empty file", archive: nil, err: "archive holds no verve.db"},
		{name: "database failing the integrity check", archive: buildArchive(t, regular("verve.db", corruptDB(t))), err: "check verve.db: check database integrity: "},
		{name: "database that is not one", archive: buildArchive(t, regular("verve.db", []byte("not a database"))), err: "check verve.db"},
		{name: "database without the schema of Verve", archive: buildArchive(t, regular("verve.db", emptyDB(t))), err: "check verve.db: the database has no Verve schema"},
		{name: "no database", archive: buildArchive(t, regular("secret.key", key)), err: "archive holds no verve.db"},
		{name: "another name", archive: buildArchive(t, regular("verve.db", db), regular("notes.txt", key)), err: `unexpected entry "notes.txt"`},
		{name: "directory path", archive: buildArchive(t, regular("data/verve.db", db)), err: `unexpected entry "data/verve.db"`},
		{name: "current directory path", archive: buildArchive(t, regular("./verve.db", db)), err: `unexpected entry "./verve.db"`},
		{name: "parent path", archive: buildArchive(t, regular("../verve.db", db)), err: `unexpected entry "../verve.db"`},
		{name: "absolute path", archive: buildArchive(t, regular("/verve.db", db)), err: `unexpected entry "/verve.db"`},
		{name: "symbolic link", archive: buildArchive(t, tarEntry{name: "verve.db", typeflag: tar.TypeSymlink}), err: "entry verve.db is not a regular file"},
		{name: "hard link", archive: buildArchive(t, tarEntry{name: "verve.db", typeflag: tar.TypeLink}), err: "entry verve.db is not a regular file"},
		{name: "directory", archive: buildArchive(t, tarEntry{name: "verve.db", typeflag: tar.TypeDir}), err: "entry verve.db is not a regular file"},
		{name: "database twice", archive: buildArchive(t, regular("verve.db", db), regular("verve.db", db)), err: "archive holds verve.db twice"},
		{name: "secret key twice", archive: buildArchive(t, regular("verve.db", db), regular("secret.key", key), regular("secret.key", key)), err: "archive holds secret.key twice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parent := t.TempDir()
			dir := filepath.Join(parent, "restore")
			require.NoError(t, os.Mkdir(dir, 0o700))

			archive := writeFile(t, "backup.tar.zst", tt.archive)

			_, err := backup.Extract(t.Context(), archive, dir, sqlite.CheckRestorable)
			require.ErrorContains(t, err, tt.err)

			require.Equal(t, []string{"restore"}, dirEntries(t, parent))
		})
	}
}

// flipLastByte returns a copy of data with its last byte changed: in a zstd archive, the checksum of
// its content.
func flipLastByte(data []byte) []byte {
	flipped := bytes.Clone(data)
	flipped[len(flipped)-1] ^= 0xff

	return flipped
}

// emptyDB returns the content of a valid SQLite database with a table but no migration: a database
// that is not one of Verve.
func emptyDB(t *testing.T) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "other.db")

	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(), "CREATE TABLE notes (body TEXT)")
	require.NoError(t, err)
	require.NoError(t, db.Close(t.Context()))

	content, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
	require.NoError(t, err)

	return content
}
