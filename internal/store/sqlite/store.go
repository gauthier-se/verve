package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// sqliteStore implements store.Store on a DB. Its repositories read on the reader pool and run
// each write in its own transaction on the writer.
type sqliteStore struct {
	repositories

	pool poolQueries
	db   *DB
}

// New returns a store on db, which must be migrated, taking the current time from clk. Closing
// the store closes db.
func New(db *DB, clk clock.Clock) store.Store {
	pool := poolQueries{reader: sqlcgen.New(db.Read()), writer: db.Write()}

	return &sqliteStore{repositories: repositories{q: pool, clk: clk}, pool: pool, db: db}
}

// WithTx runs fn in a transaction on the writer. Reads through tx see the writes of fn.
func (s *sqliteStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx store.Repositories) error) error {
	return s.pool.write(ctx, func(q *sqlcgen.Queries) error {
		return fn(ctx, repositories{q: txQueries{q: q}, clk: s.clk})
	})
}

// Close closes the database.
func (s *sqliteStore) Close(ctx context.Context) error {
	return s.db.Close(ctx)
}

// repositories implements store.Repositories on q.
type repositories struct {
	q   queries
	clk clock.Clock
}

func (r repositories) InstanceSettings() store.InstanceSettingsRepository {
	return instanceSettings(r)
}

// queries gives the repositories the sqlc queries to run, either on the pools outside WithTx or
// on the transaction of WithTx.
type queries interface {
	// read returns the queries for reads.
	read() *sqlcgen.Queries
	// write runs fn with the queries for writes. It returns the error of fn.
	write(ctx context.Context, fn func(q *sqlcgen.Queries) error) error
}

// poolQueries reads on the reader pool and runs each write in its own transaction on the writer.
type poolQueries struct {
	reader *sqlcgen.Queries
	writer *sql.DB
}

func (p poolQueries) read() *sqlcgen.Queries {
	return p.reader
}

func (p poolQueries) write(ctx context.Context, fn func(q *sqlcgen.Queries) error) error {
	return inTx(ctx, p.writer, func(tx *sql.Tx) error {
		return fn(sqlcgen.New(tx))
	})
}

// txQueries reads and writes on the transaction of WithTx.
type txQueries struct {
	q *sqlcgen.Queries
}

func (t txQueries) read() *sqlcgen.Queries {
	return t.q
}

func (t txQueries) write(_ context.Context, fn func(q *sqlcgen.Queries) error) error {
	return fn(t.q)
}

// inTx runs fn in a transaction of db and commits it when fn returns nil. When fn returns an
// error, the transaction is rolled back and that error is returned. When fn panics, the
// transaction is rolled back before the panic propagates.
func inTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Deferred so that a panic also rolls back, which releases the writer connection. After a
	// commit, or a rollback done by database/sql when ctx is cancelled, Rollback returns ErrTxDone.
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back transaction: %w", rbErr))
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
