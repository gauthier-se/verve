package data

import (
	"context"
	"database/sql"
	"fmt"
)

// State is a categorical value that holds over an interval, owned by one Account
// — a sleep stage or a stand hour. Kind groups the family ("sleep", "stand");
// StateValue is the neutral phase within it ("asleep_rem", "in_bed", "stood"…).
// StartAt/EndAt are RFC 3339 strings. ContentKey is the dedup identity (ADR 0006).
type State struct {
	AccountID  int64
	Kind       string
	StateValue string
	StartAt    string
	EndAt      string
	Source     string
	ContentKey string
}

// StateModel is the DAO for states (sleep, stand hours).
type StateModel struct {
	DB *sql.DB
}

// InsertStateBatch inserts a batch of States in one transaction, skipping any
// whose (account, content_key) already exists so re-import is idempotent (ADR
// 0006). It returns a mask parallel to ss: inserted[i] is true iff ss[i] was a
// new row. Batching bounds memory and keeps the WAL small during a large import,
// exactly like measurements — sleep alone is tens of thousands of rows.
func (m StateModel) InsertStateBatch(ctx context.Context, ss []State) ([]bool, error) {
	inserted := make([]bool, len(ss))
	if len(ss) == 0 {
		return inserted, nil
	}

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("data: begin state batch: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	const query = `
		INSERT OR IGNORE INTO states
			(account_id, kind, state_value, start_at, end_at, source, content_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("data: prepare state insert: %w", err)
	}
	defer stmt.Close()

	for i, row := range ss {
		res, err := stmt.ExecContext(ctx,
			row.AccountID, row.Kind, row.StateValue,
			row.StartAt, row.EndAt, row.Source, row.ContentKey)
		if err != nil {
			return nil, fmt.Errorf("data: insert state: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("data: state rows affected: %w", err)
		}
		inserted[i] = n == 1
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("data: commit state batch: %w", err)
	}
	return inserted, nil
}
