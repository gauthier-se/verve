package app

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// sourceKey is the secret key of the instance that the archives of the tests come from.
const sourceKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// restoredMapMode is the value of the setting map.mode in the archives of the tests, and none in the
// data directories they are restored to.
const restoredMapMode = "restored"

var preRestoreArchive = regexp.MustCompile(`^verve-.+-\d{8}T\d{6}Z-pre-restore\.tar\.zst$`)

// olderMigrations returns the embedded migrations but the latest: those of the previous version of
// Verve.
func olderMigrations(t *testing.T) fs.FS {
	t.Helper()

	embedded := sqlite.EmbeddedMigrations()
	names, err := fs.Glob(embedded, "*.sql")
	require.NoError(t, err)

	fsys := fstest.MapFS{}

	for _, name := range names[:len(names)-1] {
		data, err := fs.ReadFile(embedded, name)
		require.NoError(t, err)

		fsys[name] = &fstest.MapFile{Data: data}
	}

	return fsys
}

// archiveOf returns a manual backup of the data directory of cfg, made like verve backup but
// whatever its schema version, after setting map.mode to restoredMapMode.
func archiveOf(t *testing.T, cfg config.Config) string {
	t.Helper()

	db, err := sqlite.Open(t.Context(), newLayout(cfg.DataDir).DB)
	require.NoError(t, err)

	defer func() { require.NoError(t, db.Close(context.Background())) }()

	s := sqlite.New(db, clock.Real())
	require.NoError(t, s.InstanceSettings().Set(t.Context(), "map.mode", restoredMapMode))

	path, err := newBackups(cfg, clock.Real()).Create(t.Context(), s, backup.ReasonManual)
	require.NoError(t, err)

	return path
}

// sourceConfig returns the configuration of a data directory that the archives of a test come from,
// with sourceKey in its key file.
func sourceConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := testConfig(t.TempDir())

	layout, err := EnsureDataDir(cfg.DataDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(layout.SecretKey, []byte(sourceKey), 0o600))

	return cfg
}

// sourceArchive returns a backup of an instance whose schema is up to date and whose key file holds
// sourceKey.
func sourceArchive(t *testing.T) string {
	t.Helper()

	cfg := sourceConfig(t)

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)

	return archiveOf(t, cfg)
}

// targetConfig returns the configuration of a data directory with data, map.mode set to none, and
// a generated key file, and that key.
func targetConfig(t *testing.T) (config.Config, string) {
	t.Helper()

	cfg := dataDirWithData(t)

	key, err := ensureSecretKey(newLayout(cfg.DataDir).SecretKey, "")
	require.NoError(t, err)

	return cfg, key
}

// mapModeOf returns the setting map.mode of the database at path, which it closes at once.
func mapModeOf(t *testing.T, path string) string {
	t.Helper()

	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)

	defer func() { require.NoError(t, db.Close(context.Background())) }()

	setting, err := sqlite.New(db, clock.Real()).InstanceSettings().Get(t.Context(), "map.mode")
	require.NoError(t, err)

	return setting.Value
}

// dataDirFiles returns the content of every regular file of dataDir, by path, and the empty
// directories, so that a test sees any change.
func dataDirFiles(t *testing.T, dataDir string) map[string]string {
	t.Helper()

	files := map[string]string{}

	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			files[path] = "directory"

			return err
		}

		content, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the temporary directory of the test
		files[path] = string(content)

		return err
	})
	require.NoError(t, err)

	return files
}

// leaveWAL makes the database of dataDir look like the one of a server that crashed: its last write,
// map.mode set to crashed, is only in its WAL.
func leaveWAL(t *testing.T, dataDir string) {
	t.Helper()

	layout := newLayout(dataDir)
	scratch := filepath.Join(t.TempDir(), dbFile)

	content, err := os.ReadFile(layout.DB)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(scratch, content, 0o600)) //nolint:gosec // G703: the path is in the temporary directory of the test

	db, err := sqlite.Open(t.Context(), scratch)
	require.NoError(t, err)

	defer func() { require.NoError(t, db.CloseShared()) }()

	// The writer pool has a single connection, which keeps the pragma.
	_, err = db.Write().ExecContext(t.Context(), "PRAGMA wal_autocheckpoint = 0")
	require.NoError(t, err)
	require.NoError(t, sqlite.New(db, clock.Real()).InstanceSettings().Set(t.Context(), "map.mode", "crashed"))

	for _, suffix := range []string{"", "-wal"} {
		content, err := os.ReadFile(scratch + suffix) //nolint:gosec // G304: the path is in the temporary directory of the test
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(layout.DB+suffix, content, 0o600)) //nolint:gosec // G703: the path is in the temporary directory of the test
	}

	info, err := os.Stat(layout.DB + "-wal")
	require.NoError(t, err)
	require.Positive(t, info.Size())
}

// restoreEvents returns the backup events of the database of dataDir.
func restoreEvents(t *testing.T, dataDir string) []store.AuditEvent {
	t.Helper()

	db, err := sqlite.Open(t.Context(), newLayout(dataDir).DB)
	require.NoError(t, err)

	defer func() { require.NoError(t, db.Close(context.Background())) }()

	events, err := sqlite.New(db, clock.Real()).AuditEvents().
		List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 100})
	require.NoError(t, err)

	var backups []store.AuditEvent

	for _, event := range events {
		if event.Type == "backup.created" || event.Type == "backup.restored" {
			backups = append(backups, event)
		}
	}

	return backups
}

// Given a running server, when verve restore runs, then it refuses with the lock error and nothing
// changes.
func TestRestore_ServerRunning_IsRefused(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg, _ := targetConfig(t)

	lock, err := AcquireLock(cfg.DataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	before := dataDirFiles(t, cfg.DataDir)

	_, err = Restore(t.Context(), cfg, archive, discardLogger())
	require.EqualError(t, err, "another Verve process is using "+cfg.DataDir+": stop verve serve before restoring a backup")

	require.Equal(t, before, dataDirFiles(t, cfg.DataDir))
}

// Given a valid archive, when verve restore runs, then the database and secret.key are replaced, and
// the old -wal and -shm files are gone.
func TestRestore_ValidArchive_ReplacesTheDatabaseAndTheSecretKey(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg, _ := targetConfig(t)
	leaveWAL(t, cfg.DataDir)

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)
	require.True(t, res.SecretKey)

	layout := newLayout(cfg.DataDir)

	for _, suffix := range []string{"-wal", "-shm"} {
		_, err := os.Stat(layout.DB + suffix)
		require.ErrorIs(t, err, fs.ErrNotExist, suffix)
	}

	require.Equal(t, restoredMapMode, mapModeOf(t, layout.DB))
	requireFileContent(t, layout.SecretKey, sourceKey)

	for _, path := range []string{layout.DB, layout.SecretKey} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), path)
	}

	require.Equal(t, sqlite.LatestVersion(), schemaVersion(t, cfg.DataDir))
	require.Empty(t, res.Migrations.Applied)

	entries, err := os.ReadDir(cfg.DataDir)
	require.NoError(t, err)

	for _, entry := range entries {
		require.NotContains(t, entry.Name(), "restore", "the temporary files of the restore must be gone")
	}
}

// Given a restore, then the current database, with the writes of its WAL, and its key are first kept
// as a pre-restore backup, which retention never deletes.
func TestRestore_KeepsTheCurrentDatabaseAsAPreRestoreBackup(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg, key := targetConfig(t)
	leaveWAL(t, cfg.DataDir)

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)

	layout := newLayout(cfg.DataDir)
	require.Equal(t, layout.Backups, filepath.Dir(res.PreRestore))
	require.Regexp(t, preRestoreArchive, filepath.Base(res.PreRestore))
	require.Equal(t, []string{"verve.db", "secret.key"}, archiveNames(t, res.PreRestore))

	setting, err := sqlite.New(archivedDB(t, res.PreRestore), clock.Real()).InstanceSettings().Get(t.Context(), "map.mode")
	require.NoError(t, err)
	require.Equal(t, "crashed", setting.Value)

	backups := newBackups(cfg, clock.Real())
	require.NoError(t, backups.ApplyRetention(t.Context(), 1))
	require.NoError(t, backups.ApplyPreMigrationRetention(t.Context()))

	require.Equal(t, []string{filepath.Base(res.PreRestore)}, backupArchives(t, cfg.DataDir))

	// The key of the pre-restore backup is the one the restore replaced.
	restoredKeyDir := t.TempDir()
	extracted, err := backup.Extract(t.Context(), res.PreRestore, restoredKeyDir, func(context.Context, string) error { return nil })
	require.NoError(t, err)
	requireFileContent(t, extracted.SecretKey, key)
}

// Given a backup from an older schema, when verve restore runs, then the migrations run after the
// restore, without a pre-migration backup: the archive restored is the way back.
func TestRestore_OlderSchema_RunsTheMigrations(t *testing.T) {
	t.Parallel()

	source := sourceConfig(t)

	_, err := migrate(t.Context(), source, discardLogger(), migrationPlan{source: olderMigrations(t)})
	require.NoError(t, err)
	require.Equal(t, sqlite.LatestVersion()-1, schemaVersion(t, source.DataDir))

	archive := archiveOf(t, source)
	cfg, _ := targetConfig(t)

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)

	require.Len(t, res.Migrations.Applied, 1)
	require.Equal(t, sqlite.LatestVersion(), res.Migrations.Applied[0].Version)
	require.Equal(t, sqlite.LatestVersion(), res.Migrations.Version)
	require.Equal(t, sqlite.LatestVersion(), schemaVersion(t, cfg.DataDir))
	require.Equal(t, restoredMapMode, mapModeOf(t, newLayout(cfg.DataDir).DB))

	require.Equal(t, []string{filepath.Base(res.PreRestore)}, backupArchives(t, cfg.DataDir))
}

// Given a backup from a newer schema, when verve restore runs, then it is refused and nothing
// changes.
func TestRestore_NewerSchema_IsRefusedAndNothingChanges(t *testing.T) {
	t.Parallel()

	source := sourceConfig(t)
	setFutureSchemaVersion(t, source.DataDir)

	archive := archiveOf(t, source)
	cfg, _ := targetConfig(t)
	before := dataDirFiles(t, cfg.DataDir)

	_, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.ErrorContains(t, err, "is newer than this binary supports")

	require.Equal(t, before, dataDirFiles(t, cfg.DataDir))
}

// Given an archive that is not one, a missing file, or a secret key that is not one, when verve
// restore runs, then it is refused and nothing changes.
func TestRestore_InvalidArchive_IsRefusedAndNothingChanges(t *testing.T) {
	t.Parallel()

	invalidKey := sourceConfig(t)
	require.NoError(t, os.WriteFile(newLayout(invalidKey.DataDir).SecretKey, []byte("short"), 0o600))

	_, err := Migrate(t.Context(), invalidKey, discardLogger())
	require.NoError(t, err)

	tests := []struct {
		name    string
		archive string
		err     string
	}{
		{name: "not an archive", archive: writeTestFile(t, "verve.tar.zst", "not an archive"), err: "read archive"},
		{name: "missing file", archive: filepath.Join(t.TempDir(), "missing.tar.zst"), err: "no such file"},
		{name: "invalid secret key", archive: archiveOf(t, invalidKey), err: "secret key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _ := targetConfig(t)
			before := dataDirFiles(t, cfg.DataDir)

			_, err := Restore(t.Context(), cfg, tt.archive, discardLogger())
			require.ErrorContains(t, err, tt.err)

			require.Equal(t, before, dataDirFiles(t, cfg.DataDir))
		})
	}
}

// writeTestFile writes content to a new file name of a temporary directory and returns its path.
func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// Given a backup without secret.key, when verve restore runs, then the current key stays.
func TestRestore_WithoutSecretKey_KeepsTheCurrentKey(t *testing.T) {
	t.Parallel()

	source := sourceConfig(t)

	_, err := Migrate(t.Context(), source, discardLogger())
	require.NoError(t, err)

	// With the key configured, a backup holds only the database.
	source.SecretKey = configuredKey
	archive := archiveOf(t, source)
	require.Equal(t, []string{"verve.db"}, archiveNames(t, archive))

	cfg, key := targetConfig(t)

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)
	require.False(t, res.SecretKey)

	layout := newLayout(cfg.DataDir)
	requireFileContent(t, layout.SecretKey, key)
	require.Equal(t, restoredMapMode, mapModeOf(t, layout.DB))
}

// Given a restore, then the restored database records the pre-restore backup and the restore, with
// the system as actor.
func TestRestore_RecordsTheAuditEventWithTheSystemAsActor(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg, _ := targetConfig(t)

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)

	events := restoreEvents(t, cfg.DataDir)
	require.Len(t, events, 2)

	payloads := map[string]string{}

	for _, event := range events {
		require.Zero(t, event.ActorID, event.Type)
		require.Zero(t, event.SubjectID, event.Type)

		payloads[event.Type] = string(event.Payload)
	}

	require.JSONEq(t, `{"reason":"pre-restore","file":"`+filepath.Base(res.PreRestore)+`"}`, payloads["backup.created"])
	require.JSONEq(t, `{"file":"`+filepath.Base(archive)+`"}`, payloads["backup.restored"])
}

// Given a data directory without database, as on a new host, when verve restore runs, then it
// restores the backup of the other instance, database and key, with no pre-restore backup.
func TestRestore_EmptyDataDir_RestoresWithoutPreRestoreBackup(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg := testConfig(filepath.Join(t.TempDir(), "data"))

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)
	require.Empty(t, res.PreRestore)

	layout := newLayout(cfg.DataDir)
	require.Equal(t, restoredMapMode, mapModeOf(t, layout.DB))
	requireFileContent(t, layout.SecretKey, sourceKey)
	require.Empty(t, backupArchives(t, cfg.DataDir))

	events := restoreEvents(t, cfg.DataDir)
	require.Len(t, events, 1)
	require.Equal(t, "backup.restored", events[0].Type)
}

// Given a database migrated by a newer Verve, when the previous version restores its pre-migration
// backup, which is the way to roll back, then it is restored and the newer database is kept as the
// pre-restore backup.
func TestRestore_CurrentDatabaseNewer_RollsBack(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)

	cfg := testConfig(t.TempDir())

	_, err := EnsureDataDir(cfg.DataDir)
	require.NoError(t, err)

	// Closed before the restore, unlike the database of setFutureSchemaVersion.
	db, err := sqlite.Open(t.Context(), newLayout(cfg.DataDir).DB)
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(),
		"INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", sqlite.LatestVersion()+1)
	require.NoError(t, err)
	require.NoError(t, db.Close(t.Context()))

	res, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)

	require.Equal(t, sqlite.LatestVersion(), schemaVersion(t, cfg.DataDir))
	require.Equal(t, restoredMapMode, mapModeOf(t, newLayout(cfg.DataDir).DB))

	version, err := sqlite.CurrentVersion(t.Context(), archivedDB(t, res.PreRestore))
	require.NoError(t, err)
	require.Equal(t, sqlite.LatestVersion()+1, version)
}

// Given the temporary directory of a restore that crashed, when verve restore runs, then it removes
// it.
func TestRestore_RemovesTheTemporaryFilesOfACrashedRestore(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg, _ := targetConfig(t)

	crashed := filepath.Join(cfg.DataDir, ".verve-restore-123")
	require.NoError(t, os.Mkdir(crashed, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(crashed, "verve.db"), []byte("partial"), 0o600))

	_, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)

	_, err = os.Stat(crashed)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// Given the WAL of a database removed by hand, when verve restore runs, then the WAL is removed
// before the restored database takes its place, which SQLite would apply it to.
func TestRestore_OrphanWAL_IsNotAppliedToTheRestoredDatabase(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg, _ := targetConfig(t)
	leaveWAL(t, cfg.DataDir)

	layout := newLayout(cfg.DataDir)
	require.NoError(t, os.Remove(layout.DB))

	_, err := Restore(t.Context(), cfg, archive, discardLogger())
	require.NoError(t, err)

	require.Equal(t, restoredMapMode, mapModeOf(t, layout.DB))
	require.NoError(t, sqlite.CheckRestorable(t.Context(), layout.DB))
}

// Given a current database that cannot be read, when verve restore runs, then the pre-restore
// backup fails, and the restore stops and changes nothing.
func TestRestore_UnreadableCurrentDatabase_StopsAndChangesNothing(t *testing.T) {
	t.Parallel()

	archive := sourceArchive(t)
	cfg := testConfig(t.TempDir())

	layout, err := EnsureDataDir(cfg.DataDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(layout.DB, []byte("not a database, but the only copy of the data"), 0o600))

	// The lock file stays once a command has taken it.
	lock, err := AcquireLock(cfg.DataDir)
	require.NoError(t, err)
	require.NoError(t, lock.Release())

	before := dataDirFiles(t, cfg.DataDir)

	_, err = Restore(t.Context(), cfg, archive, discardLogger())
	require.ErrorContains(t, err, "not a database")

	require.Equal(t, before, dataDirFiles(t, cfg.DataDir))
}
