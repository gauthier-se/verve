package query

import (
	"context"
	"fmt"
	"sort"
)

// This file answers the questions that are *about* an Account's data rather than
// aggregations of it: which Metrics it holds, and which Sources produced them.
//
// They live in the read engine because they read the two families it owns —
// measurements and states — and because their answers are only ever used to decide
// what to read next. Kept in the storage layer they had to be split across two
// models and patched back together by the caller, which is exactly what the
// Ledger's old withSleep was.

// SourceSpan is one Source's footprint in the Account's history: when it first and
// last recorded something, and how many rows it left. It answers "when did this
// device arrive" — the question a step change in a curve usually turns out to be.
type SourceSpan struct {
	Source    string
	FirstSeen string
	LastSeen  string
	Rows      int
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
func (e Engine) Sources(ctx context.Context, accountID int64) ([]SourceSpan, error) {
	rows, err := e.DB.QueryContext(ctx, sourcesQuery, accountID, accountID)
	if err != nil {
		return nil, fmt.Errorf("query: sources: %w", err)
	}
	defer rows.Close()

	out := []SourceSpan{}
	for rows.Next() {
		var s SourceSpan
		if err := rows.Scan(&s.Source, &s.FirstSeen, &s.LastSeen, &s.Rows); err != nil {
			return nil, fmt.Errorf("query: sources scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: sources: %w", err)
	}
	return out, nil
}

// MetricsWithData lists the Catalog slugs this engine can actually serve a Series
// for, sorted — the row set of the Ledger overview (ADR 0021), so the scoreboard
// shows no empty rows.
//
// It answers the whole question rather than the measurements half of it. Sleep is
// a Metric whose values are States (ADR 0027), so a DISTINCT over measurements
// cannot see it, and the caller used to patch the slug back in afterwards while
// knowing which table it came from. Any future State-backed Metric is a change
// here rather than a second patch at a second call site.
func (e Engine) MetricsWithData(ctx context.Context, accountID int64) ([]string, error) {
	const query = `SELECT DISTINCT metric FROM measurements WHERE account_id = ?`
	rows, err := e.DB.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("query: metrics with data: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var metric string
		if err := rows.Scan(&metric); err != nil {
			return nil, fmt.Errorf("query: metrics with data scan: %w", err)
		}
		seen[metric] = true
		out = append(out, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: metrics with data: %w", err)
	}

	// Nothing writes a sleep Measurement today (the Manual entry path refuses one),
	// but a row set must not gain a duplicate on the day something does.
	if !seen[sleepKind] {
		has, err := e.hasStates(ctx, accountID, sleepKind)
		if err != nil {
			return nil, err
		}
		if has {
			out = append(out, sleepKind)
		}
	}

	// Training volume is folded from the Sessions family (ADR 0040), which the
	// DISTINCT above cannot see either. One probe serves both Metrics: they read
	// the same rows and an Account with workouts has data for both, even if a
	// history of strength sessions alone gives training_distance nothing to show.
	hasSessions, err := e.hasSessions(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if hasSessions {
		for _, slug := range []string{metricTrainingTime, metricTrainingDistance} {
			if !seen[slug] {
				out = append(out, slug)
			}
		}
	}

	sort.Strings(out)
	return out, nil
}

// hasStates reports whether the Account has any State of a kind. Deliberately not
// a DistinctKinds: there are two kinds and only one of them is read (ADR 0027).
func (e Engine) hasStates(ctx context.Context, accountID int64, kind string) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM states WHERE account_id = ? AND kind = ?)`
	var found bool
	if err := e.DB.QueryRowContext(ctx, query, accountID, kind).Scan(&found); err != nil {
		return false, fmt.Errorf("query: has states: %w", err)
	}
	return found, nil
}
