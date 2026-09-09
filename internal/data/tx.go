package data

import (
	"context"
	"database/sql"
	"fmt"
)

// Handle is what a model runs its statements against: the pool, or one
// transaction taken from it. *sql.DB and *sql.Tx both satisfy it.
//
// Models are written against this rather than against *sql.DB so that the same
// model, unchanged, serves a direct write and a write inside a transaction. The
// alternative was a parallel set of tx-taking functions, which is how a storage
// layer ends up with two ways to write every row and one of them forgotten.
type Handle interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// Tx runs fn inside one transaction and commits it, or rolls back and returns
// fn's error. The Models fn receives is bound to that transaction: every write
// through it is in the same unit, and every read through it sees the writes.
//
// Binding the whole Models, rather than handing out a bare *sql.Tx, is what makes
// this safe under Verve's single-connection pool (see Open). A transaction holds
// that one connection, so any statement issued against the pool while it is open
// waits for a connection that cannot be released until the transaction ends: a
// deadlock, not an error. A caller holding only the tx-bound Models has nothing
// to reach the pool with.
//
// Tx is reentrant: calling it on an already-bound Models runs fn in the
// transaction already in progress rather than deadlocking on a second one. That is
// what lets a batch insert keep its own atomicity when called directly and still
// compose when a caller wraps several of them together.
//
// Two rules for fn, both consequences of the single connection: do not hold a
// *sql.Rows open across another statement, and do not start a goroutine that
// touches the database.
func (m Models) Tx(ctx context.Context, fn func(tx Models) error) error {
	if m.tx != nil {
		return fn(m)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin transaction: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	bound := newModels(tx)
	bound.db, bound.tx = m.db, tx

	if err := fn(bound); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit transaction: %w", err)
	}
	return nil
}

// atomically runs fn against h in a transaction, or directly when h already is
// one. It is Tx for a single model: the same reentrance, without rebinding a
// whole Models, so a batch insert is atomic on its own and simply joins the unit
// when a caller has wrapped it in Tx.
func atomically(ctx context.Context, h Handle, fn func(Handle) error) error {
	db, ok := h.(*sql.DB)
	if !ok {
		return fn(h) // already inside a transaction: this is part of it
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("data: begin: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("data: commit: %w", err)
	}
	return nil
}

// insertBatch runs one prepared INSERT over rows and reports which were newly
// written. It is the shape every family's batch insert had, three times over:
// one statement, one transaction, one row of arguments per item, and a mask so a
// re-import can say what it skipped rather than only what it added (ADR 0006).
//
// what names the family in the error messages, which is the only thing the three
// differed in beyond their columns.
func insertBatch[T any](ctx context.Context, h Handle, what, query string, rows []T, args func(T) []any) ([]bool, error) {
	inserted := make([]bool, len(rows))
	if len(rows) == 0 {
		return inserted, nil
	}

	err := atomically(ctx, h, func(h Handle) error {
		stmt, err := h.PrepareContext(ctx, query)
		if err != nil {
			return fmt.Errorf("data: prepare %s insert: %w", what, err)
		}
		defer stmt.Close()

		for i, row := range rows {
			res, err := stmt.ExecContext(ctx, args(row)...)
			if err != nil {
				return fmt.Errorf("data: insert %s: %w", what, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("data: %s rows affected: %w", what, err)
			}
			inserted[i] = n == 1
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inserted, nil
}
