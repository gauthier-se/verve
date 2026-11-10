package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/gauthier-se/verve/internal/audit"
	"github.com/gauthier-se/verve/internal/backup"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// The migration types of the SQLite store are aliased here, so that the command line does not
// import it.
type (
	// MigrateResult lists the migrations applied by Migrate and the resulting schema version.
	MigrateResult = sqlite.MigrateResult
	// Migration identifies a schema migration.
	Migration = sqlite.Migration
	// MigrationState is a migration with the time it was applied, zero when it is pending.
	MigrationState = sqlite.MigrationState
)

// beforeMigrateFunc runs before pending migrations are applied, with their versions.
type beforeMigrateFunc func(ctx context.Context, pending []int64) error

// migrationPlan is what migrateDB applies. Its zero value applies the embedded migrations after a
// backup of the database.
type migrationPlan struct {
	// source holds the migrations, nil for the embedded ones.
	source fs.FS
	// before, when not nil, runs before the pending migrations in place of the backup.
	before beforeMigrateFunc
}

// Migrate takes the data directory lock, opens the database and applies the pending migrations,
// after a backup of a database that has data.
func Migrate(ctx context.Context, cfg config.Config, logger *slog.Logger) (MigrateResult, error) {
	return migrate(ctx, cfg, logger, migrationPlan{})
}

// migrate is Migrate with the migrations of plan.
func migrate(ctx context.Context, cfg config.Config, logger *slog.Logger, plan migrationPlan) (MigrateResult, error) {
	return withOfflineDB(ctx, cfg, func(db *sqlite.DB) (MigrateResult, error) {
		return migrateDB(ctx, cfg, db, clock.Real(), logger, plan)
	})
}

// MigrationStatus takes the data directory lock, opens the database and lists the migrations
// with the time they were applied.
func MigrationStatus(ctx context.Context, cfg config.Config) ([]MigrationState, error) {
	return withOfflineDB(ctx, cfg, func(db *sqlite.DB) ([]MigrationState, error) {
		return sqlite.MigrationStatus(ctx, db)
	})
}

// migrateDB applies the migrations of plan to db, the database of the data directory of cfg. Before
// the first pending migration of a database that has data, it creates a pre-migration backup, which
// it records in the audit log once the migrations are applied: the schema before them may not hold
// the event. A failed backup applies no migration. A failure to record it keeps the archive, the way
// back from migrations that are already applied.
func migrateDB(ctx context.Context, cfg config.Config, db *sqlite.DB, clk clock.Clock, logger *slog.Logger,
	plan migrationPlan,
) (MigrateResult, error) {
	var archive string

	before := plan.before
	if before == nil {
		before = func(ctx context.Context, _ []int64) error {
			var err error

			archive, err = backUpBeforeMigrating(ctx, cfg, db, clk, logger)

			return err
		}
	}

	res, err := sqlite.Migrate(ctx, db, sqlite.MigrateOptions{Source: plan.source, BeforeMigrate: before, Logger: logger})
	if err != nil || archive == "" {
		return res, err
	}

	event := audit.BackupCreated(string(backup.ReasonPreMigration), archive)
	if err := audit.Record(ctx, sqlite.New(db, clk).AuditEvents(), event); err != nil {
		return res, fmt.Errorf("record pre-migration backup %s: %w", archive, err)
	}

	return res, nil
}

// backUpBeforeMigrating creates the pre-migration backup of db, the database of the data directory
// of cfg, and returns the name of its archive. A new database, at version 0, has nothing to back
// up: it returns an empty name.
func backUpBeforeMigrating(ctx context.Context, cfg config.Config, db *sqlite.DB, clk clock.Clock,
	logger *slog.Logger,
) (string, error) {
	current, err := sqlite.CurrentVersion(ctx, db)
	if err != nil || current == 0 {
		return "", err
	}

	logger.InfoContext(ctx, "backing up the database before migrating")

	path, err := newBackups(cfg, clk).CreatePreMigration(ctx, db)
	if err != nil {
		return "", err
	}

	name := filepath.Base(path)
	logger.InfoContext(ctx, "backed up the database before migrating", slog.String("file", name))

	return name, nil
}

// withOfflineDB runs fn on the database of cfg while holding the data directory lock, so that it
// never runs next to a serving process.
func withOfflineDB[T any](ctx context.Context, cfg config.Config, fn func(db *sqlite.DB) (T, error)) (res T, err error) {
	db, closeDB, err := openDatabase(ctx, cfg.DataDir)
	if err != nil {
		if _, inUse := errors.AsType[*dataDirInUseError](err); inUse {
			err = fmt.Errorf("%w: stop verve serve before running migrations", err)
		}

		return res, err
	}

	defer func() { err = errors.Join(err, closeDB(context.WithoutCancel(ctx))) }()

	return fn(db)
}
