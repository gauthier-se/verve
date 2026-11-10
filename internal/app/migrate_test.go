package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// openDataDirDB opens the database of dataDir and closes it at the end of the test.
func openDataDirDB(t *testing.T, dataDir string) *sqlite.DB {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(dataDir, dbFile))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close(context.Background())) })

	return db
}

// schemaVersion returns the schema version of the database of dataDir.
func schemaVersion(t *testing.T, dataDir string) int64 {
	t.Helper()

	version, err := sqlite.CurrentVersion(t.Context(), openDataDirDB(t, dataDir))
	require.NoError(t, err)

	return version
}

// setFutureSchemaVersion migrates the database of dataDir and records a version newer than the
// latest embedded migration, as a newer Verve would.
func setFutureSchemaVersion(t *testing.T, dataDir string) {
	t.Helper()

	db := openDataDirDB(t, dataDir)

	_, err := sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	_, err = db.Write().ExecContext(t.Context(),
		"INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", sqlite.LatestVersion()+1)
	require.NoError(t, err)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Given `verve migrate` on an empty data directory, then it creates the database and applies
// every embedded migration.
func TestMigrate_AppliesEmbeddedMigrations(t *testing.T) {
	t.Parallel()

	cfg := testConfig(filepath.Join(t.TempDir(), "data"))

	res, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)
	require.NotEmpty(t, res.Applied)
	require.Equal(t, sqlite.LatestVersion(), res.Version)

	states, err := MigrationStatus(t.Context(), cfg)
	require.NoError(t, err)
	require.Len(t, states, len(res.Applied))

	for _, s := range states {
		require.False(t, s.AppliedAt.IsZero(), s.Name)
	}
}

// Given a CLI process, then nothing is scheduled: only the server schedules recurring jobs.
func TestMigrate_SchedulesNothing(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)

	require.Empty(t, storedSchedules(t, cfg))
}

// Given a running `verve serve` holding the data directory lock, then migrate and migrate status
// fail with a clear error.
func TestMigrate_DataDirLockedFails(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	lock, err := AcquireLock(cfg.DataDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	want := "another Verve process is using " + cfg.DataDir + ": stop verve serve before running migrations"

	_, err = Migrate(t.Context(), cfg, discardLogger())
	require.EqualError(t, err, want)

	_, err = MigrationStatus(t.Context(), cfg)
	require.EqualError(t, err, want)
}

// Given a database whose version is newer than this binary supports, then migrate fails.
func TestMigrate_NewerDatabaseFails(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())
	setFutureSchemaVersion(t, cfg.DataDir)

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.ErrorContains(t, err, "is newer than this binary supports")
}

// upgradeTable is the table that the migration of upgradeMigrations creates.
const upgradeTable = "upgrade"

// upgradeMigrations returns the embedded migrations followed by one more, which creates
// upgradeTable: the migrations of the next version of Verve.
func upgradeMigrations(t *testing.T) fs.FS {
	t.Helper()

	return embeddedMigrationsAnd(t, "CREATE TABLE "+upgradeTable+" (id INTEGER PRIMARY KEY) STRICT;")
}

// failingUpgradeMigrations returns the embedded migrations followed by one more, which fails.
func failingUpgradeMigrations(t *testing.T) fs.FS {
	t.Helper()

	return embeddedMigrationsAnd(t, "INSERT INTO no_such_table VALUES (1);")
}

// embeddedMigrationsAnd returns the embedded migrations followed by one more, named after
// upgradeTable, which runs statement.
func embeddedMigrationsAnd(t *testing.T, statement string) fs.FS {
	t.Helper()

	embedded := sqlite.EmbeddedMigrations()
	names, err := fs.Glob(embedded, "*.sql")
	require.NoError(t, err)

	fsys := fstest.MapFS{}

	for _, name := range names {
		data, err := fs.ReadFile(embedded, name)
		require.NoError(t, err)

		fsys[name] = &fstest.MapFile{Data: data}
	}

	fsys[fmt.Sprintf("%05d_%s.sql", sqlite.LatestVersion()+1, upgradeTable)] = &fstest.MapFile{
		Data: []byte("-- +goose Up\n" + statement + "\n"),
	}

	return fsys
}

// dataDirWithData returns the configuration of a data directory whose database is migrated and
// holds an instance setting.
func dataDirWithData(t *testing.T) config.Config {
	t.Helper()

	cfg := testConfig(t.TempDir())

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)

	_, err = withSharedDB(t.Context(), cfg, func(s store.Store) (struct{}, error) {
		return struct{}{}, s.InstanceSettings().Set(t.Context(), "map.mode", "none")
	})
	require.NoError(t, err)

	return cfg
}

// backupArchives returns the names of the archives in the backups directory of dataDir.
func backupArchives(t *testing.T, dataDir string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(newLayout(dataDir).Backups, "*.tar.zst"))
	require.NoError(t, err)

	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}

	return names
}

// hasUpgradeTable reports whether the database db has upgradeTable, which the upgrade migration creates.
func hasUpgradeTable(t *testing.T, db *sqlite.DB) bool {
	t.Helper()

	var n int
	require.NoError(t, db.Read().QueryRowContext(t.Context(),
		"SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", upgradeTable).Scan(&n))

	return n == 1
}

// lockBackupsDir takes the lock of the backups directory of dataDir, which a backup waits for, and
// returns the function that releases it.
func lockBackupsDir(t *testing.T, dataDir string) func() {
	t.Helper()

	guard := flock.New(filepath.Join(newLayout(dataDir).Backups, ".verve-backup.lock"))

	locked, err := guard.TryLock()
	require.NoError(t, err)
	require.True(t, locked)

	var once sync.Once

	unlock := func() { once.Do(func() { require.NoError(t, guard.Unlock()) }) }
	t.Cleanup(unlock)

	return unlock
}

// breakBackups makes every backup of dataDir fail: the lock of its backups directory is a
// directory, which cannot be opened as a file.
func breakBackups(t *testing.T, dataDir string) {
	t.Helper()

	require.NoError(t, os.Mkdir(filepath.Join(newLayout(dataDir).Backups, ".verve-backup.lock"), 0o700))
}

// failingBackupError is the start of the error of a backup of a data directory broken by
// breakBackups.
const failingBackupError = "back up: lock backups directory"

var preMigrationArchive = regexp.MustCompile(`^verve-.+-\d{8}T\d{6}Z-pre-migration\.tar\.zst$`)

// Given pending migrations on a database with data, when verve migrate runs, then a
// pre-migration backup of the database as it was is created before the first migration, and the
// audit log records it once the schema is migrated.
func TestMigrate_PendingMigrationsOnADatabaseWithData_BacksUpFirst(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)

	res, err := migrate(t.Context(), cfg, discardLogger(), migrationPlan{source: upgradeMigrations(t)})
	require.NoError(t, err)
	require.Equal(t, []Migration{{Version: sqlite.LatestVersion() + 1, Name: upgradeTable}}, res.Applied)

	archives := backupArchives(t, cfg.DataDir)
	require.Len(t, archives, 1)
	require.Regexp(t, preMigrationArchive, archives[0])

	archived := archivedDB(t, filepath.Join(newLayout(cfg.DataDir).Backups, archives[0]))
	require.False(t, hasUpgradeTable(t, archived), "the backup must hold the database before the migration")

	version, err := sqlite.CurrentVersion(t.Context(), archived)
	require.NoError(t, err)
	require.Equal(t, sqlite.LatestVersion(), version)

	setting, err := sqlite.New(archived, clock.Real()).InstanceSettings().Get(t.Context(), "map.mode")
	require.NoError(t, err)
	require.Equal(t, "none", setting.Value)

	events, err := sqlite.New(openDataDirDB(t, cfg.DataDir), clock.Real()).AuditEvents().
		List(t.Context(), store.AuditEventsFilter{}, store.PageRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "backup.created", events[0].Type)
	require.Zero(t, events[0].ActorID)
	require.JSONEq(t, `{"reason":"pre-migration","file":"`+archives[0]+`"}`, string(events[0].Payload))
}

// Given a brand new database, when verve migrate applies every migration, then no backup is taken.
func TestMigrate_NewDatabase_TakesNoBackup(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t.TempDir())

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)

	require.Empty(t, backupArchives(t, cfg.DataDir))
}

// Given a schema up to date, when verve migrate runs, then no backup is taken.
func TestMigrate_UpToDate_TakesNoBackup(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)

	_, err := Migrate(t.Context(), cfg, discardLogger())
	require.NoError(t, err)

	require.Empty(t, backupArchives(t, cfg.DataDir))
}

// Given pending migrations and a failing backup, when verve migrate runs, then no migration runs
// and it fails with the reason.
func TestMigrate_FailingBackup_RunsNoMigrationAndFails(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)
	breakBackups(t, cfg.DataDir)

	_, err := migrate(t.Context(), cfg, discardLogger(), migrationPlan{source: upgradeMigrations(t)})
	require.ErrorContains(t, err, failingBackupError)

	require.Equal(t, sqlite.LatestVersion(), schemaVersion(t, cfg.DataDir))
	require.False(t, hasUpgradeTable(t, openDataDirDB(t, cfg.DataDir)))
	require.Empty(t, backupArchives(t, cfg.DataDir))
}

// olderPreMigrationArchives are pre-migration archives of earlier upgrades, oldest first.
var olderPreMigrationArchives = []string{
	"verve-0.1.0-20250101T120000Z-pre-migration.tar.zst",
	"verve-0.2.0-20250201T120000Z-pre-migration.tar.zst",
	"verve-0.3.0-20250301T120000Z-pre-migration.tar.zst",
}

// writeOlderPreMigrationArchives writes olderPreMigrationArchives, empty, in the backups directory
// of dataDir.
func writeOlderPreMigrationArchives(t *testing.T, dataDir string) {
	t.Helper()

	for _, name := range olderPreMigrationArchives {
		require.NoError(t, os.WriteFile(filepath.Join(newLayout(dataDir).Backups, name), nil, 0o600))
	}
}

// Given the pre-migration backups of 3 earlier upgrades, when the migrations succeed, then only
// the backups of the last 3 migrations stay, the new one included.
func TestMigrate_SuccessfulMigrations_KeepTheBackupsOfTheLastThreeMigrations(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)
	writeOlderPreMigrationArchives(t, cfg.DataDir)

	_, err := migrate(t.Context(), cfg, discardLogger(), migrationPlan{source: upgradeMigrations(t)})
	require.NoError(t, err)

	archives := backupArchives(t, cfg.DataDir)
	require.Len(t, archives, 3)
	require.Equal(t, olderPreMigrationArchives[1:], archives[:2])
	require.Regexp(t, preMigrationArchive, archives[2])
}

// Given the pre-migration backups of 3 earlier upgrades, when a migration fails after the backup,
// then every pre-migration backup stays: retention counts only the migrations that succeed, so
// that restarts that keep failing never remove the backup from before the upgrade.
func TestMigrate_FailingMigration_KeepsEveryPreMigrationBackup(t *testing.T) {
	t.Parallel()

	cfg := dataDirWithData(t)
	writeOlderPreMigrationArchives(t, cfg.DataDir)

	_, err := migrate(t.Context(), cfg, discardLogger(), migrationPlan{source: failingUpgradeMigrations(t)})
	require.ErrorContains(t, err, "no such table")

	archives := backupArchives(t, cfg.DataDir)
	require.Len(t, archives, 4)
	require.Equal(t, olderPreMigrationArchives, archives[:3])
	require.Regexp(t, preMigrationArchive, archives[3])
}
