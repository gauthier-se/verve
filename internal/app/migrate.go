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
func Migrate(ctx context.Context, cfg config.Config, logger *slog.Logger) (sqlite.MigrateResult, error) {
	return withOfflineDB(ctx, cfg, func(db *sqlite.DB) (sqlite.MigrateResult, error) {
		return migrateDB(ctx, db, logger)
	})
}

// MigrationStatus takes the data directory lock, opens the database and lists the migrations
// with the time they were applied.
func MigrationStatus(ctx context.Context, cfg config.Config) ([]sqlite.MigrationState, error) {
	return withOfflineDB(ctx, cfg, func(db *sqlite.DB) ([]sqlite.MigrationState, error) {
		return sqlite.MigrationStatus(ctx, db)
	})
}

func migrateDB(ctx context.Context, db *sqlite.DB, logger *slog.Logger) (sqlite.MigrateResult, error) {
	return sqlite.Migrate(ctx, db, sqlite.MigrateOptions{BeforeMigrate: beforeMigrate, Logger: logger})
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
