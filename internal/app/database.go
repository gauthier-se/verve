package app

import (
	"context"
	"errors"

	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// openDatabase creates the data directory layout, takes its lock and opens the database. It
// returns the database and a function that closes it, then releases the lock.
func openDatabase(ctx context.Context, dataDir string) (*sqlite.DB, func(context.Context) error, error) {
	layout, err := EnsureDataDir(dataDir)
	if err != nil {
		return nil, nil, err
	}

	lock, err := AcquireLock(layout.Root)
	if err != nil {
		return nil, nil, err
	}

	db, err := sqlite.Open(ctx, layout.DB)
	if err != nil {
		return nil, nil, errors.Join(err, lock.Release())
	}

	closeDB := func(ctx context.Context) error {
		return errors.Join(db.Close(ctx), lock.Release())
	}

	return db, closeDB, nil
}
