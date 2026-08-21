package data

import (
	"context"
	"database/sql"
	"fmt"
)

// SourceSpan is one Source's footprint in the Account's history: when it first and
// last recorded something, and how many rows it left. It answers "when did this
// device arrive" — the question a step change in a curve usually turns out to be.
type SourceSpan struct {
	Source    string
	FirstSeen string
	LastSeen  string
	Rows      int
}

// Span is the outer bound of everything the Account holds: the first and last
// instant any family recorded. Empty strings mean no data at all.
type Span struct {
	First string
	Last  string
}

// sourcesQuery unions the two dated families a Source writes into. Sessions carry
// a Source too, but a workout's provenance is already read on the workout itself,
// and a device that only ever recorded workouts is not the arrival this list is
// about. Routes are artifacts of a Session and carry no Source of their own.
const sourcesQuery = `
	SELECT source, MIN(start_at) AS first_seen, MAX(start_at) AS last_seen, COUNT(*) AS rows_count
	FROM (
		SELECT source, start_at FROM measurements WHERE account_id = ?
		UNION ALL
		SELECT source, start_at FROM states WHERE account_id = ?
	)
	GROUP BY source
	ORDER BY first_seen, source`

// Sources lists every Source the Account holds data from, oldest arrival first.
func (m MeasurementModel) Sources(ctx context.Context, accountID int64) ([]SourceSpan, error) {
	rows, err := m.DB.QueryContext(ctx, sourcesQuery, accountID, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: measurement Sources: %w", err)
	}
	defer rows.Close()

	out := []SourceSpan{}
	for rows.Next() {
		var s SourceSpan
		if err := rows.Scan(&s.Source, &s.FirstSeen, &s.LastSeen, &s.Rows); err != nil {
			return nil, fmt.Errorf("data: measurement Sources scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: measurement Sources: %w", err)
	}
	return out, nil
}

// Span is the first and last instant the Account holds anything at, across the
// dated families. It is what the History page opens on: the real extent of the
// history, rather than a range preset guessing at it.
func (m MeasurementModel) Span(ctx context.Context, accountID int64) (Span, error) {
	const query = `
		SELECT MIN(start_at), MAX(start_at) FROM (
			SELECT start_at FROM measurements WHERE account_id = ?
			UNION ALL
			SELECT start_at FROM states WHERE account_id = ?
			UNION ALL
			SELECT start_at FROM sessions WHERE account_id = ?
		)`
	var first, last sql.NullString
	err := m.DB.QueryRowContext(ctx, query, accountID, accountID, accountID).Scan(&first, &last)
	if err != nil {
		return Span{}, fmt.Errorf("data: measurement Span: %w", err)
	}
	return Span{First: first.String, Last: last.String}, nil
}

// ListImports returns the Account's recorded Import runs, most recent first. Every
// run is kept, including the ones that added nothing: "I re-dropped the export and
// it added 412 rows" and "I re-dropped it and it added none" are the same promise
// of idempotence, and only the second one proves it (ADR 0006).
func (m MeasurementModel) ListImports(ctx context.Context, accountID int64) ([]Import, error) {
	const query = `
		SELECT id, account_id, source_file, added_count, skipped_count, unmapped_count, imported_at
		FROM imports WHERE account_id = ?
		ORDER BY imported_at DESC, id DESC`
	rows, err := m.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("data: measurement ListImports: %w", err)
	}
	defer rows.Close()

	out := []Import{}
	for rows.Next() {
		var imp Import
		if err := rows.Scan(&imp.ID, &imp.AccountID, &imp.SourceFile,
			&imp.AddedCount, &imp.SkippedCount, &imp.UnmappedCount, &imp.ImportedAt); err != nil {
			return nil, fmt.Errorf("data: measurement ListImports scan: %w", err)
		}
		out = append(out, imp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: measurement ListImports: %w", err)
	}
	return out, nil
}

// CountUnmapped is how many records the Catalog could not map and the bin kept
// (ADR 0002). It is shown next to the imports because it is the other half of the
// same promise: nothing incoming is discarded, even when nothing can read it yet.
func (m MeasurementModel) CountUnmapped(ctx context.Context, accountID int64) (int, error) {
	const query = `SELECT COUNT(*) FROM unmapped_records WHERE account_id = ?`
	var n int
	if err := m.DB.QueryRowContext(ctx, query, accountID).Scan(&n); err != nil {
		return 0, fmt.Errorf("data: measurement CountUnmapped: %w", err)
	}
	return n, nil
}
