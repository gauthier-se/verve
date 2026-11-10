package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// restoreDirPrefix starts the name of the temporary directory of a restore, in the data directory
// so that moving its files into place is an atomic rename. A restore removes those that a crashed
// one left.
const restoreDirPrefix = ".verve-restore-"

// RestoreResult is what Restore did.
type RestoreResult struct {
	// PreRestore is the path of the backup of the database that the restore replaced, empty when the
	// data directory had no database.
	PreRestore string
	// SecretKey reports whether the archive held a secret key, which replaced the key file.
	SecretKey bool
	// Migrations are the migrations applied to the restored database.
	Migrations MigrateResult
}

// Restore replaces the database and the secret key of the data directory of cfg with those of the
// backup archive, while holding the data directory lock (FR-6 and FR-7 of backups). It first
// validates the archive: it refuses an archive that does not read to its end, holds other files
// than verve.db and secret.key, whose database fails PRAGMA integrity_check or has a schema newer
// than this binary, or whose secret key is not one. Then it keeps the current database and key as a
// pre-restore backup, moves the files of the archive into place, removing the -wal and -shm files of
// the replaced database, and migrates the restored database. An archive without secret key leaves
// the key file as it is. A refused archive changes nothing.
//
// The migrations take no pre-migration backup: the archive restored is the way back, and the
// retention of pre-migration backups could remove it. Once they are applied, the restored database
// records the pre-restore backup and the restore.
func Restore(ctx context.Context, cfg config.Config, archive string, logger *slog.Logger) (res RestoreResult, err error) {
	layout, err := EnsureDataDir(cfg.DataDir)
	if err != nil {
		return res, err
	}

	lock, err := AcquireLock(layout.Root)
	if err != nil {
		if _, inUse := errors.AsType[*dataDirInUseError](err); inUse {
			err = fmt.Errorf("%w: stop verve serve before restoring a backup", err)
		}

		return res, err
	}

	defer func() { err = errors.Join(err, lock.Release()) }()

	tmpDir, err := createRestoreDir(layout.Root)
	if err != nil {
		return res, err
	}

	defer func() { err = errors.Join(err, os.RemoveAll(tmpDir)) }()

	name := filepath.Base(archive)
	logger.InfoContext(ctx, "checking the backup", slog.String("file", name))

	extracted, err := backup.Extract(ctx, archive, tmpDir, sqlite.CheckRestorable)
	if err != nil {
		return res, err
	}

	if extracted.SecretKey != "" {
		if _, err := readSecretKey(extracted.SecretKey); err != nil {
			return res, fmt.Errorf("invalid archive %s: %w", name, err)
		}
	}

	clk := clock.Real()

	res.PreRestore, err = backUpBeforeRestoring(ctx, newBackups(cfg, clk), layout.DB, logger)
	if err != nil {
		return res, err
	}

	if err := moveIntoPlace(layout, extracted); err != nil {
		return res, err
	}

	res.SecretKey = extracted.SecretKey != ""
	logger.InfoContext(ctx, "restored the backup", slog.String("file", name))

	res.Migrations, err = migrateRestored(ctx, cfg, layout.DB, clk, logger, res.PreRestore, name)

	return res, err
}

// createRestoreDir removes the temporary directories of crashed restores from dataDir, whose lock
// the caller holds, and creates the one of a new restore, with the permissions 0700 of os.MkdirTemp.
func createRestoreDir(dataDir string) (string, error) {
	leftovers, err := filepath.Glob(filepath.Join(dataDir, restoreDirPrefix+"*"))
	if err != nil {
		return "", fmt.Errorf("find temporary files: %w", err)
	}

	for _, dir := range leftovers {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("remove temporary files %s: %w", dir, err)
		}
	}

	dir, err := os.MkdirTemp(dataDir, restoreDirPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("create temporary directory: %w", err)
	}

	return dir, nil
}

// backUpBeforeRestoring creates the pre-restore backup of the database at path with backups and
// returns the path of its archive. A database that does not exist or has no schema has nothing to
// keep: it returns an empty path. The database may be newer than this binary, when a restore rolls
// back an upgrade. Closing it checkpoints its WAL.
func backUpBeforeRestoring(ctx context.Context, backups *backup.Backups, path string, logger *slog.Logger) (archive string, err error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return "", err
	}

	defer func() { err = errors.Join(err, db.Close(ctx)) }()

	current, err := sqlite.CurrentVersion(ctx, db)
	if err != nil || current == 0 {
		return "", err
	}

	logger.InfoContext(ctx, "backing up the current database before restoring")

	archive, err = backups.CreatePreRestore(ctx, db)
	if err != nil {
		return "", err
	}

	logger.InfoContext(ctx, "backed up the current database before restoring", slog.String("file", filepath.Base(archive)))

	return archive, nil
}

// moveIntoPlace removes the -wal and -shm files of the database of layout, which SQLite would
// otherwise apply to the restored one, then renames the extracted files over the current ones.
func moveIntoPlace(layout Layout, extracted backup.Extracted) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(layout.DB + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("restore: %w", err)
		}
	}

	if err := os.Rename(extracted.DB, layout.DB); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	if extracted.SecretKey == "" {
		return nil
	}

	if err := os.Rename(extracted.SecretKey, layout.SecretKey); err != nil {
		return fmt.Errorf("restore secret key: %w", err)
	}

	return nil
}

// migrateRestored applies the pending migrations to the restored database at path, without
// pre-migration backup, then records the pre-restore backup, when there is one, and the restore of
// the archive named name.
func migrateRestored(ctx context.Context, cfg config.Config, path string, clk clock.Clock, logger *slog.Logger,
	preRestore, name string,
) (res MigrateResult, err error) {
	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return res, err
	}

	defer func() { err = errors.Join(err, db.Close(ctx)) }()

	noBackup := func(context.Context, []int64) error { return nil }

	res, err = migrateDB(ctx, cfg, db, clk, logger, migrationPlan{before: noBackup})
	if err != nil {
		return res, fmt.Errorf("migrate restored database: %w", err)
	}

	events := sqlite.New(db, clk).AuditEvents()

	if preRestore != "" {
		event := audit.BackupCreated(string(backup.ReasonPreRestore), filepath.Base(preRestore))
		if err := audit.Record(ctx, events, event); err != nil {
			return res, fmt.Errorf("record pre-restore backup: %w", err)
		}
	}

	if err := audit.Record(ctx, events, audit.BackupRestored(name)); err != nil {
		return res, fmt.Errorf("record restore: %w", err)
	}

	return res, nil
}
