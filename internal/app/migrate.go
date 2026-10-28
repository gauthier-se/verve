package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// beforeMigrate runs before pending migrations are applied. M1 takes a backup of the database here.
func beforeMigrate(context.Context, []int64) error {
	return nil
}

// Migrate takes the data directory lock, opens the database and applies the pending migrations.
func Migrate(ctx context.Context, cfg config.Config, logger *slog.Logger) (res sqlite.MigrateResult, err error) {
	err = withOfflineDB(ctx, cfg, func(db *sqlite.DB) error {
		res, err = migrateDB(ctx, db, logger)

		return err
	})

	return res, err
}

// MigrationStatus takes the data directory lock, opens the database and lists the migrations
// with the time they were applied.
func MigrationStatus(ctx context.Context, cfg config.Config) (states []sqlite.MigrationState, err error) {
	err = withOfflineDB(ctx, cfg, func(db *sqlite.DB) error {
		states, err = sqlite.MigrationStatus(ctx, db)

		return err
	})

	return states, err
}

func migrateDB(ctx context.Context, db *sqlite.DB, logger *slog.Logger) (sqlite.MigrateResult, error) {
	return sqlite.Migrate(ctx, db, sqlite.MigrateOptions{BeforeMigrate: beforeMigrate, Logger: logger})
}

// withOfflineDB runs fn on the database of cfg while holding the data directory lock, so that it
// never runs next to a serving process.
func withOfflineDB(ctx context.Context, cfg config.Config, fn func(db *sqlite.DB) error) (err error) {
	layout, err := EnsureDataDir(cfg.DataDir)
	if err != nil {
		return err
	}

	lock, err := AcquireLock(layout.Root)
	if err != nil {
		return fmt.Errorf("%w: stop verve serve before running migrations", err)
	}

	defer func() { err = errors.Join(err, lock.Release()) }()

	db, err := sqlite.Open(ctx, layout.DB)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, db.Close(context.WithoutCancel(ctx))) }()

	return fn(db)
}
