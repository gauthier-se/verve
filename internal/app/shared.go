package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/config"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// withSharedDB runs fn on a store of the database of cfg without taking the data directory lock,
// so that it can run next to a serving process. It applies no migration and fails unless the
// schema is the one this binary needs.
func withSharedDB[T any](ctx context.Context, cfg config.Config, fn func(s store.Store) (T, error)) (res T, err error) {
	layout, err := EnsureDataDir(cfg.DataDir)
	if err != nil {
		return res, err
	}

	db, err := sqlite.Open(ctx, layout.DB)
	if err != nil {
		return res, err
	}

	defer func() { err = errors.Join(err, db.CloseShared()) }()

	if err := checkSchemaVersion(ctx, db); err != nil {
		return res, err
	}

	// Not closed on its own: the deferred CloseShared closes the database under it, because the
	// Close of the store would checkpoint the WAL.
	return fn(sqlite.New(db, clock.Real()))
}

// checkSchemaVersion fails unless the schema version of db is the one this binary needs.
func checkSchemaVersion(ctx context.Context, db *sqlite.DB) error {
	current, err := sqlite.CurrentVersion(ctx, db)
	if err != nil {
		return err
	}

	latest := sqlite.LatestVersion()

	switch {
	case current < latest:
		return fmt.Errorf("database schema is at version %d, this binary needs %d: start verve serve or run verve migrate",
			current, latest)
	case current > latest:
		return sqlite.NewerSchemaError(current, latest)
	}

	return nil
}
