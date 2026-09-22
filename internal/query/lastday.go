package query

import (
	"context"
	"fmt"
	"sort"

	"github.com/gauthier-se/verve/internal/catalog"
)

// This file answers "when did the data stop", the question Freshness asks (ADR
// 0045). It sits beside metadata.go for the same reason: it reads the two families
// the engine owns and says something about the data rather than aggregating it.
//
// A last day is always the day the engine would bucket the row in, never a
// timestamp: date(start_at) for a Measurement, the Night label for sleep. Anything
// else would let Now say the data stops on the 12th while the Panel under it draws
// a bar on the 13th.

// SourceLastDay is the last day a Source recorded something the engine reads.
type SourceLastDay struct {
	Source  string
	LastDay string // YYYY-MM-DD
}

// sourceLastStartQuery is a loose index scan over measurements_account_source_start
// (migration 0017): the recursive half walks to each next Source with one seek, and
// the outer select takes that Source's last start_at with another. A GROUP BY over
// the same index would read every row the Account holds to answer a dozen values.
const sourceLastStartQuery = `
	WITH RECURSIVE s(source) AS (
		SELECT (SELECT MIN(source) FROM measurements WHERE account_id = ?1)
		UNION ALL
		SELECT (SELECT MIN(source) FROM measurements WHERE account_id = ?1 AND source > s.source)
		FROM s WHERE s.source IS NOT NULL
	)
	SELECT source,
	       date((SELECT MAX(start_at) FROM measurements WHERE account_id = ?1 AND source = s.source))
	FROM s
	WHERE source IS NOT NULL AND source <> ?2`

// sourceLastNightQuery is the same question of sleep, dated by Night. Stand States
// are left out as they are everywhere else: no Metric reads them (ADR 0027).
const sourceLastNightQuery = `
	SELECT source, MAX(` + nightExpr + `)
	FROM states
	WHERE account_id = ? AND kind = ? AND source <> ?
	GROUP BY source`

// SourceLastDays lists, for every Source the Account holds Measurements or sleep
// from, the last day it recorded something, sorted by Source.
//
// The Manual Source is left out: a typed value says nothing about whether a device
// is still sending, and counting it would let one typed weight make a silent Watch
// look fresh. Sessions are left out too, as they are from Sources: a workout's
// provenance is read on the workout.
func (e Engine) SourceLastDays(ctx context.Context, accountID int64) ([]SourceLastDay, error) {
	last := map[string]string{}
	collect := func(q string, args ...any) error {
		rows, err := e.DB.QueryContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("query: source last days: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var source string
			var day *string // date() is NULL for a start_at no Connector could parse
			if err := rows.Scan(&source, &day); err != nil {
				return fmt.Errorf("query: source last days scan: %w", err)
			}
			if day != nil && *day > last[source] {
				last[source] = *day
			}
		}
		return rows.Err()
	}
	if err := collect(sourceLastStartQuery, accountID, catalog.SourceManual); err != nil {
		return nil, err
	}
	if err := collect(sourceLastNightQuery, accountID, sleepKind, catalog.SourceManual); err != nil {
		return nil, err
	}

	out := make([]SourceLastDay, 0, len(last))
	for source, day := range last {
		out = append(out, SourceLastDay{Source: source, LastDay: day})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out, nil
}
