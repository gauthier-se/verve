package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Phase is a bounded stretch of time over which the Account pursues one Target rate —
// a cut, a bulk, a maintenance stretch (CONTEXT.md). EndedAt nil means open; at most
// one Phase per Account is open at a time, enforced by a partial unique index.
//
// RatePctPerWeek is signed: positive is a bulk, negative a cut. It is a rate and not a
// calorie figure because the same deficit is trivial at one body size and dangerous at
// another — the calorie target is derived on read, against the Expenditure estimate.
type Phase struct {
	ID             int64
	AccountID      int64
	RatePctPerWeek float64
	StartedAt      string
	EndedAt        *string
	CreatedAt      string
}

// PhaseModel is the DAO for phases.
type PhaseModel struct {
	DB *sql.DB
}

const phaseColumns = `id, account_id, rate_pct_per_week, started_at, ended_at, created_at`

// Open starts a Phase at the given rate, closing whatever Phase is currently open. Both
// happen in one transaction: a history with two open Phases, or with a gap where the old
// one was closed but the new one never landed, would make every later adherence figure
// ambiguous. The partial unique index is the backstop if two callers race.
func (m PhaseModel) Open(ctx context.Context, accountID int64, rate float64, startedAt string) (*Phase, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("data: begin open phase: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	if _, err := tx.ExecContext(ctx,
		`UPDATE phases SET ended_at = ? WHERE account_id = ? AND ended_at IS NULL`,
		startedAt, accountID); err != nil {
		return nil, fmt.Errorf("data: close current phase: %w", err)
	}

	var p Phase
	err = tx.QueryRowContext(ctx, `
		INSERT INTO phases (account_id, rate_pct_per_week, started_at)
		VALUES (?, ?, ?)
		RETURNING `+phaseColumns,
		accountID, rate, startedAt,
	).Scan(&p.ID, &p.AccountID, &p.RatePctPerWeek, &p.StartedAt, &p.EndedAt, &p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("data: insert phase: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("data: commit open phase: %w", err)
	}
	return &p, nil
}

// Current returns the Account's open Phase, or ErrRecordNotFound when none is open —
// a normal state, not a failure: an Account can read its Plan without committing to one.
func (m PhaseModel) Current(ctx context.Context, accountID int64) (*Phase, error) {
	return m.getOne(ctx,
		`SELECT `+phaseColumns+` FROM phases WHERE account_id = ? AND ended_at IS NULL`,
		accountID)
}

// GetByID returns one Phase of the Account, or ErrRecordNotFound.
func (m PhaseModel) GetByID(ctx context.Context, accountID, id int64) (*Phase, error) {
	return m.getOne(ctx,
		`SELECT `+phaseColumns+` FROM phases WHERE id = ? AND account_id = ?`,
		id, accountID)
}

// ListByAccount returns the Account's Phases newest first — the history the Plan page
// shows, and the reason Phases are never overwritten.
func (m PhaseModel) ListByAccount(ctx context.Context, accountID int64) ([]Phase, error) {
	rows, err := m.DB.QueryContext(ctx,
		`SELECT `+phaseColumns+` FROM phases WHERE account_id = ? ORDER BY started_at DESC, id DESC`,
		accountID)
	if err != nil {
		return nil, fmt.Errorf("data: list phases: %w", err)
	}
	defer rows.Close()

	out := []Phase{}
	for rows.Next() {
		var p Phase
		if err := rows.Scan(&p.ID, &p.AccountID, &p.RatePctPerWeek, &p.StartedAt, &p.EndedAt, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("data: scan phase: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate phases: %w", err)
	}
	return out, nil
}

// Close ends an open Phase. Closing an already-closed one yields ErrRecordNotFound
// rather than silently moving its end date, which would rewrite history.
func (m PhaseModel) Close(ctx context.Context, accountID, id int64, endedAt string) error {
	res, err := m.DB.ExecContext(ctx,
		`UPDATE phases SET ended_at = ? WHERE id = ? AND account_id = ? AND ended_at IS NULL`,
		endedAt, id, accountID)
	if err != nil {
		return fmt.Errorf("data: close phase: %w", err)
	}
	return affectedOne(res, "close phase")
}

// Delete removes a Phase outright — for a mis-typed rate, where closing it would leave
// a meaningless stretch in the history rather than correcting it.
func (m PhaseModel) Delete(ctx context.Context, accountID, id int64) error {
	res, err := m.DB.ExecContext(ctx,
		`DELETE FROM phases WHERE id = ? AND account_id = ?`, id, accountID)
	if err != nil {
		return fmt.Errorf("data: delete phase: %w", err)
	}
	return affectedOne(res, "delete phase")
}

func (m PhaseModel) getOne(ctx context.Context, query string, args ...any) (*Phase, error) {
	var p Phase
	err := m.DB.QueryRowContext(ctx, query, args...).
		Scan(&p.ID, &p.AccountID, &p.RatePctPerWeek, &p.StartedAt, &p.EndedAt, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("data: get phase: %w", err)
	}
	return &p, nil
}

// affectedOne maps a zero-row write to ErrRecordNotFound.
func affectedOne(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("data: %s rows affected: %w", op, err)
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}
