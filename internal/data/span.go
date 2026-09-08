package data

import (
	"context"
	"database/sql"
	"fmt"
)

// Span is the outer bound of everything the Account holds: the first and last
// instant any dated family recorded. Empty strings mean no data at all.
type Span struct {
	First string
	Last  string
}

// Span is the real extent of an Account's history, across all three dated
// families — Measurements, States and Sessions. It is what the History page opens
// on, rather than a range preset guessing at it (CONTEXT.md: History).
//
// It hangs off Models rather than off any one family model because it is the one
// read that is about all of them at once. It is also why it is not in the read
// engine: internal/query aggregates measurements and states, and a Session is an
// entity that it deliberately never names (ADR 0028) — but a workout is still part
// of how far back a history goes.
func (m Models) Span(ctx context.Context, accountID int64) (Span, error) {
	const query = `
		SELECT MIN(start_at), MAX(start_at) FROM (
			SELECT start_at FROM measurements WHERE account_id = ?
			UNION ALL
			SELECT start_at FROM states WHERE account_id = ?
			UNION ALL
			SELECT start_at FROM sessions WHERE account_id = ?
		)`
	var first, last sql.NullString
	err := m.db.QueryRowContext(ctx, query, accountID, accountID, accountID).Scan(&first, &last)
	if err != nil {
		return Span{}, fmt.Errorf("data: span: %w", err)
	}
	return Span{First: first.String, Last: last.String}, nil
}
