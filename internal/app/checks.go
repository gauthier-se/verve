package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// Readiness check settings.
const (
	// databaseCheckTimeout bounds the pings of the database check.
	databaseCheckTimeout = 2 * time.Second
	// dataDirCheckInterval is how long the data directory check reuses its last result.
	dataDirCheckInterval = 10 * time.Second
)

// databaseChecker checks that both pools of the database reach it.
type databaseChecker struct {
	db *sqlite.DB
}

func newDatabaseChecker(db *sqlite.DB) databaseChecker {
	return databaseChecker{db: db}
}

func (databaseChecker) Name() string {
	return "database"
}

func (c databaseChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, databaseCheckTimeout)
	defer cancel()

	if err := c.db.Write().PingContext(ctx); err != nil {
		return fmt.Errorf("ping writer: %w", err)
	}

	if err := c.db.Read().PingContext(ctx); err != nil {
		return fmt.Errorf("ping reader: %w", err)
	}

	return nil
}

// migrationsChecker checks that every embedded migration is applied.
type migrationsChecker struct {
	db *sqlite.DB
}

func newMigrationsChecker(db *sqlite.DB) migrationsChecker {
	return migrationsChecker{db: db}
}

func (migrationsChecker) Name() string {
	return "migrations"
}

func (c migrationsChecker) Check(ctx context.Context) error {
	states, err := sqlite.MigrationStatus(ctx, c.db)
	if err != nil {
		return err
	}

	pending := 0

	for _, s := range states {
		if s.AppliedAt.IsZero() {
			pending++
		}
	}

	if pending > 0 {
		return fmt.Errorf("%d pending", pending)
	}

	return nil
}

// dataDirChecker checks that the data directory is writable. It touches the disk at most once
// per dataDirCheckInterval and returns its last result in between.
type dataDirChecker struct {
	dir string
	clk clock.Clock

	mu        sync.Mutex
	checkedAt time.Time // zero before the first check
	err       error
}

func newDataDirChecker(dir string, clk clock.Clock) *dataDirChecker {
	return &dataDirChecker{dir: dir, clk: clk}
}

func (*dataDirChecker) Name() string {
	return "data_dir"
}

func (c *dataDirChecker) Check(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clk.Now()
	if !c.checkedAt.IsZero() && now.Sub(c.checkedAt) < dataDirCheckInterval {
		return c.err
	}

	c.checkedAt = now
	c.err = nil

	if err := checkWritable(c.dir); err != nil {
		// The path names a temporary file: only the cause is worth reporting.
		if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
			err = pathErr.Err
		}

		c.err = fmt.Errorf("not writable: %w", err)
	}

	return c.err
}
