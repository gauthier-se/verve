package query

import (
	"context"
	"fmt"
	"sort"

	"github.com/gauthier-se/verve/internal/catalog"
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
	return e.MetricsWithDataIn(ctx, accountID, "", "")
}

// MetricsWithDataIn is the same question asked of one window: the slugs the engine
// can serve a Series for between the day labels from (inclusive) and to
// (exclusive), either side empty for unbounded. It is what a Day asks (ADR 0043),
// and MetricsWithData is it with no bounds.
//
// The bounds are day labels rather than timestamps because every dated column here
// is a zero-padded RFC 3339 string, so a lexical comparison against YYYY-MM-DD
// orders exactly as a date does. It is the same comparison ExclusionSet.Excludes
// makes, for the same reason.
//
// The window is applied to all three families, each at the grain that family is
// read at: a Measurement by its own timestamp, sleep by the Night expression so a
// night lands where its Metric puts it (ADR 0027), and a workout by the day it
// started (ADR 0040). Asked of one day, the three answers are the three the Day
// page shows.
func (e Engine) MetricsWithDataIn(ctx context.Context, accountID int64, from, to string) ([]string, error) {
	seen := map[string]bool{}
	out, err := e.measuredMetrics(ctx, accountID, from, to)
	if err != nil {
		return nil, err
	}
	for _, slug := range out {
		seen[slug] = true
	}

	// Nothing writes a sleep Measurement today (the Manual entry path refuses one),
	// but a row set must not gain a duplicate on the day something does.
	if !seen[sleepKind] {
		has, err := e.hasStates(ctx, accountID, sleepKind, from, to)
		if err != nil {
			return nil, err
		}
		if has {
			out = append(out, sleepKind)
		}
	}

	// Training volume is folded from the Sessions family (ADR 0040), which the
	// DISTINCT above cannot see either. The two Metrics are probed separately
	// rather than together: an Account that only lifts has workouts and no
	// kilometres, and a row of dashes is the empty row this function exists to
	// keep off the scoreboard.
	for _, slug := range []string{metricTrainingTime, metricTrainingDistance} {
		if seen[slug] {
			continue
		}
		has, err := e.hasTrainingData(ctx, accountID, slug, from, to)
		if err != nil {
			return nil, err
		}
		if has {
			out = append(out, slug)
		}
	}

	sort.Strings(out)
	return out, nil
}

// measuredMetrics is the measurements half of the question, and it asks it in one of
// two shapes because the two windows have opposite best plans.
//
// Unbounded, a DISTINCT over the Account's rows is a single covering-index scan, and
// it is what the Ledger wants: it has to look at everything anyway.
//
// Windowed, that same scan is the wrong query. measurements_account_metric_start
// leads on (account_id, metric), so a filter on start_at with no Metric named cannot
// seek and reads every row the Account holds: 150 ms on a real history, for a page
// that is navigated a day at a time with the arrow keys. Probing the Catalog instead
// turns it into about a hundred index seeks on the leading columns, which is two
// orders of magnitude cheaper and needs no second index. The Catalog is compiled in
// and small by construction (ADR 0002), which is what makes the loop bounded.
func (e Engine) measuredMetrics(ctx context.Context, accountID int64, from, to string) ([]string, error) {
	if from == "" && to == "" {
		return e.distinctMetrics(ctx, accountID)
	}

	clause, bounds := windowClause("start_at", from, to)
	q := `SELECT EXISTS(SELECT 1 FROM measurements WHERE account_id = ? AND metric = ?` + clause + `)`
	stmt, err := e.DB.PrepareContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query: metrics with data: %w", err)
	}
	defer stmt.Close()

	out := []string{}
	for slug, metric := range catalog.All() {
		// A derived Metric has no rows of its own; it is computed from operands that
		// have them, and probing for it would always miss (ADR 0014).
		if metric.Nature == catalog.Derived {
			continue
		}
		args := append([]any{accountID, slug}, bounds...)
		var found bool
		if err := stmt.QueryRowContext(ctx, args...).Scan(&found); err != nil {
			return nil, fmt.Errorf("query: metrics with data: %w", err)
		}
		if found {
			out = append(out, slug)
		}
	}
	return out, nil
}

// distinctMetrics is the unbounded row set: one covering-index scan.
func (e Engine) distinctMetrics(ctx context.Context, accountID int64) ([]string, error) {
	const q = `SELECT DISTINCT metric FROM measurements WHERE account_id = ?`
	rows, err := e.DB.QueryContext(ctx, q, accountID)
	if err != nil {
		return nil, fmt.Errorf("query: metrics with data: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var metric string
		if err := rows.Scan(&metric); err != nil {
			return nil, fmt.Errorf("query: metrics with data scan: %w", err)
		}
		out = append(out, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: metrics with data: %w", err)
	}
	return out, nil
}

// hasStates reports whether the Account has any State of a kind in the window.
// Deliberately not a DistinctKinds: there are two kinds and only one of them is
// read (ADR 0027).
//
// The window is applied to the Night expression and not to start_at, because a
// night is the bucket a sleep interval falls in and an interval that started at
// 23:14 belongs to the morning after. Filtering on the raw timestamp would put a
// night in the day it began, which is the split ADR 0027 exists to undo.
func (e Engine) hasStates(ctx context.Context, accountID int64, kind, from, to string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM states WHERE account_id = ? AND kind = ?`
	args := []any{accountID, kind}
	clause, bounds := windowClause(nightExpr, from, to)
	query += clause + `)`
	args = append(args, bounds...)

	var found bool
	if err := e.DB.QueryRowContext(ctx, query, args...).Scan(&found); err != nil {
		return false, fmt.Errorf("query: has states: %w", err)
	}
	return found, nil
}

// windowClause is the half-open day-label filter shared by the three family
// probes: [from, to) on expr, either side empty for unbounded. It returns the SQL
// and the bound values, in order, or an empty clause when both sides are open.
func windowClause(expr, from, to string) (string, []any) {
	clause := ""
	args := []any{}
	if from != "" {
		clause += " AND " + expr + " >= ?"
		args = append(args, from)
	}
	if to != "" {
		clause += " AND " + expr + " < ?"
		args = append(args, to)
	}
	return clause, args
}
