package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The two directions a Goal can take, both inclusive at the bound (ADR 0044). A range
// would be a third direction carrying two values, never two Goals on one Metric.
const (
	GoalAtLeast = "at_least"
	GoalAtMost  = "at_most"
)

// ErrGoalOverlap reports a Goal that would start inside the span an earlier Goal on
// the same Metric already holds. The past is corrected by deleting the entry, never
// by inserting into it, so segments never overlap.
var ErrGoalOverlap = errors.New("data: goal starts inside an earlier goal")

// Goal is a bound the owner declares on a Metric, judged against its day bucket
// (CONTEXT.md). It is in force on [StartedOn, EndedOn), both YYYY-MM-DD; EndedOn nil
// means open, and at most one Goal per Metric is open, enforced by a partial unique
// index. Value is in the Metric's canonical unit and may be negative.
type Goal struct {
	ID        int64
	AccountID int64
	Metric    string
	Direction string
	Value     float64
	StartedOn string
	EndedOn   *string
	CreatedAt string
}

// GoalModel is the DAO for goals.
type GoalModel struct {
	DB Handle
}

const goalColumns = `id, account_id, metric, direction, value, started_on, ended_on, created_at`

// Open sets a Goal on a Metric from startedOn, closing the Metric's open Goal on that
// same date. The check, the close and the insert are one transaction, for the reason
// PhaseModel.Open gives: a history with two open Goals, or a gap where the old one was
// closed and the new one never landed, would make every later count ambiguous.
//
// A Goal must start on or after the first day no earlier Goal holds: after the open
// Goal's start, and not before the last closed Goal's end. Anything earlier returns
// ErrGoalOverlap.
func (m GoalModel) Open(ctx context.Context, accountID int64, metric, direction string, value float64, startedOn string) (*Goal, error) {
	var g Goal
	err := atomically(ctx, m.DB, func(tx Handle) error {
		// The first day free of every earlier Goal: a closed Goal frees its end date,
		// an open one the day after its start (the earliest it could be closed on).
		var free sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT MAX(COALESCE(ended_on, date(started_on, '+1 day')))
			FROM goals WHERE account_id = ? AND metric = ?`,
			accountID, metric,
		).Scan(&free); err != nil {
			return fmt.Errorf("data: goal history bound: %w", err)
		}
		if free.Valid && startedOn < free.String {
			return ErrGoalOverlap
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE goals SET ended_on = ? WHERE account_id = ? AND metric = ? AND ended_on IS NULL`,
			startedOn, accountID, metric); err != nil {
			return fmt.Errorf("data: close current goal: %w", err)
		}

		err := tx.QueryRowContext(ctx, `
			INSERT INTO goals (account_id, metric, direction, value, started_on)
			VALUES (?, ?, ?, ?, ?)
			RETURNING `+goalColumns,
			accountID, metric, direction, value, startedOn,
		).Scan(&g.ID, &g.AccountID, &g.Metric, &g.Direction, &g.Value, &g.StartedOn, &g.EndedOn, &g.CreatedAt)
		if err != nil {
			return fmt.Errorf("data: insert goal: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GetByID returns one Goal of the Account, or ErrRecordNotFound.
func (m GoalModel) GetByID(ctx context.Context, accountID, id int64) (*Goal, error) {
	var g Goal
	err := m.DB.QueryRowContext(ctx,
		`SELECT `+goalColumns+` FROM goals WHERE id = ? AND account_id = ?`, id, accountID,
	).Scan(&g.ID, &g.AccountID, &g.Metric, &g.Direction, &g.Value, &g.StartedOn, &g.EndedOn, &g.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("data: get goal: %w", err)
	}
	return &g, nil
}

// ListByAccount returns the Account's Goals newest first, on one Metric when metric is
// non-empty: the history the Metric page shows, and the reason Goals are never
// overwritten.
func (m GoalModel) ListByAccount(ctx context.Context, accountID int64, metric string) ([]Goal, error) {
	return m.list(ctx, `
		SELECT `+goalColumns+` FROM goals
		WHERE account_id = ? AND (? = '' OR metric = ?)
		ORDER BY started_on DESC, id DESC`,
		accountID, metric, metric)
}

// InForce returns the Goals on a Metric in force at some point of the half-open day
// window [from, to), oldest first. An open Goal runs to the end of any window. The
// predicate is an overlap, as for Annotations: a Goal set in January is in force over
// a window starting in March.
func (m GoalModel) InForce(ctx context.Context, accountID int64, metric, from, to string) ([]Goal, error) {
	return m.list(ctx, `
		SELECT `+goalColumns+` FROM goals
		WHERE account_id = ? AND metric = ?
		  AND started_on < ?
		  AND (ended_on IS NULL OR ended_on > ?)
		ORDER BY started_on, id`,
		accountID, metric, to, from)
}

// Close ends an open Goal on endedOn without setting another. Closing an already
// closed one yields ErrRecordNotFound rather than moving its end date, which would
// rewrite history.
func (m GoalModel) Close(ctx context.Context, accountID, id int64, endedOn string) error {
	res, err := m.DB.ExecContext(ctx,
		`UPDATE goals SET ended_on = ? WHERE id = ? AND account_id = ? AND ended_on IS NULL`,
		endedOn, id, accountID)
	if err != nil {
		return fmt.Errorf("data: close goal: %w", err)
	}
	return affectedOne(res, "close goal")
}

// Delete removes a Goal outright: for a mistyped value, where closing it would leave
// a meaningless stretch in the history rather than correcting it.
func (m GoalModel) Delete(ctx context.Context, accountID, id int64) error {
	res, err := m.DB.ExecContext(ctx,
		`DELETE FROM goals WHERE id = ? AND account_id = ?`, id, accountID)
	if err != nil {
		return fmt.Errorf("data: delete goal: %w", err)
	}
	return affectedOne(res, "delete goal")
}

func (m GoalModel) list(ctx context.Context, query string, args ...any) ([]Goal, error) {
	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("data: list goals: %w", err)
	}
	defer rows.Close()

	out := []Goal{}
	for rows.Next() {
		var g Goal
		if err := rows.Scan(&g.ID, &g.AccountID, &g.Metric, &g.Direction, &g.Value, &g.StartedOn, &g.EndedOn, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("data: scan goal: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: iterate goals: %w", err)
	}
	return out, nil
}
