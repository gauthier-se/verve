package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Pin is a Catalog Metric the Account keeps in the sidebar (CONTEXT.md). It is a
// shortcut to that Metric's page and carries no time axis of its own: the whole
// point of the concept is that it is not a one-Panel Dashboard.
type Pin struct {
	ID        int64
	AccountID int64
	Metric    string
	Position  int
	CreatedAt string
}

// PinModel is the DAO for pins.
type PinModel struct {
	DB *sql.DB
}

// ListByAccount returns the Account's Pins in sidebar (position) order.
func (m PinModel) ListByAccount(ctx context.Context, accountID int64) ([]Pin, error) {
	const query = `
		SELECT id, account_id, metric, position, created_at
		FROM pins
		WHERE account_id = ?
		ORDER BY position, id`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list pins: %w", err)
	}
	defer rows.Close()

	pins := []Pin{}
	for rows.Next() {
		var p Pin
		if err := rows.Scan(&p.ID, &p.AccountID, &p.Metric, &p.Position, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("data: scan pin: %w", err)
		}
		pins = append(pins, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate pins: %w", err)
	}
	return pins, nil
}

// Add pins a Metric at the end of the Account's list and returns the Pin. It is
// idempotent: pinning an already-pinned Metric leaves the existing row and its
// position untouched, because the unique index absorbs the conflict rather than a
// read-then-write in the caller. A double click therefore cannot make two rows.
func (m PinModel) Add(ctx context.Context, accountID int64, metric string) (*Pin, error) {
	const query = `
		INSERT INTO pins (account_id, metric, position)
		VALUES (?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM pins WHERE account_id = ?))
		ON CONFLICT (account_id, metric) DO NOTHING`
	if _, err := m.DB.ExecContext(ctx, query, accountID, metric, accountID); err != nil {
		return nil, fmt.Errorf("data: add pin %s: %w", metric, err)
	}
	return m.get(ctx, accountID, metric)
}

// Delete unpins a Metric, scoped by Account so one Account can never touch
// another's row. Unpinning something that is not pinned is not an error: the
// caller asked for a state, and that state already holds.
func (m PinModel) Delete(ctx context.Context, accountID int64, metric string) error {
	const query = `DELETE FROM pins WHERE account_id = ? AND metric = ?`
	if _, err := m.DB.ExecContext(ctx, query, accountID, metric); err != nil {
		return fmt.Errorf("data: delete pin %s: %w", metric, err)
	}
	return nil
}

// get reads one of the Account's Pins by Metric, or ErrRecordNotFound.
func (m PinModel) get(ctx context.Context, accountID int64, metric string) (*Pin, error) {
	const query = `
		SELECT id, account_id, metric, position, created_at
		FROM pins
		WHERE account_id = ? AND metric = ?`
	var p Pin
	err := m.DB.QueryRowContext(ctx, query, accountID, metric).
		Scan(&p.ID, &p.AccountID, &p.Metric, &p.Position, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("data: get pin %s: %w", metric, err)
	}
	return &p, nil
}
