package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/timeaxis"
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

// LatestValue is a Metric's value on the last day that holds one (CONTEXT.md:
// Latest value). It always travels with its date, because a latest value without
// its date is exactly the stale figure Freshness exists to expose.
type LatestValue struct {
	Value float64 `json:"value"`
	Date  string  `json:"date"`
}

// latestLookbackDays is how far back from a Metric's last datum Latest reads when
// that day alone holds no value. For an imported Metric it always does, so the
// window only matters for a derived one, whose operands can stop on different days
// and whose last complete day can sit before the last datum of any single operand.
// Past it, a derived Metric has no Latest value rather than a walk back through its
// whole history.
const latestLookbackDays = 31

// Latest answers a Metric's Latest value, however far back it lies, or nil for a
// Metric the Account never measured.
//
// It finds the Metric's last datum with one index seek, then reads the day series
// that ends on it through Series, so the per-day Source election (ADR 0034), the
// Manual overlay (ADR 0022), sleep's Night (ADR 0027) and a derived Metric's
// Formula (ADR 0014) apply exactly as they do to the bar a Panel draws on that day.
// A value computed here any other way would be one a Panel could disagree with.
//
// The last datum's own day is read first and alone, because it is nearly always the
// answer and a Series costs what its window costs: on a real history, 5 ms for one
// day against 150 ms for thirty-one, paid per Pin on the screen that opens first.
func (e Engine) Latest(ctx context.Context, accountID int64, slug string) (*LatestValue, error) {
	metric, ok := catalog.Lookup(slug)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownMetric, slug)
	}
	last, err := e.lastDay(ctx, accountID, metric)
	if err != nil || last == "" {
		return nil, err
	}
	day, err := time.Parse(dayLayout, last)
	if err != nil {
		return nil, fmt.Errorf("query: latest: %w", err)
	}

	for _, days := range []int{1, latestLookbackDays} {
		got, err := e.lastPoint(ctx, accountID, slug, day.AddDate(0, 0, -days+1), day.AddDate(0, 0, 1))
		if err != nil || got != nil {
			return got, err
		}
	}
	return nil, nil
}

// lastPoint is the last day on [from, to) that holds a value, or nil.
func (e Engine) lastPoint(ctx context.Context, accountID int64, slug string, from, to time.Time) (*LatestValue, error) {
	series, err := e.Series(ctx, Request{AccountID: accountID, Metric: slug, Bucket: timeaxis.Day, From: from, To: to})
	if err != nil {
		return nil, err
	}
	for i := len(series.Points) - 1; i >= 0; i-- {
		if p := series.Points[i]; !p.Gap {
			return &LatestValue{Value: p.Value, Date: p.Bucket}, nil
		}
	}
	return nil, nil
}

// lastDay is the last day label a Metric holds data on, in the family it is read
// from, or "" when it holds none. A derived Metric's is the earliest of its
// operands' last days: no day after it can hold every operand.
func (e Engine) lastDay(ctx context.Context, accountID int64, metric catalog.Metric) (string, error) {
	if metric.Nature == catalog.Derived {
		if metric.Formula == nil {
			return "", fmt.Errorf("%w: derived %q has no Formula", ErrUnsupportedAggregation, metric.Slug)
		}
		out := ""
		for i, operand := range metric.Formula.Operands() {
			m, ok := catalog.Lookup(operand)
			if !ok {
				return "", fmt.Errorf("%w: operand %q", ErrUnknownMetric, operand)
			}
			d, err := e.lastDay(ctx, accountID, m)
			if err != nil || d == "" {
				return "", err
			}
			if i == 0 || d < out {
				out = d
			}
		}
		return out, nil
	}

	var q string
	args := []any{accountID}
	switch {
	case metric.Aggregation == catalog.DurationByState:
		// The Night label is monotonic in start_at, so the label of the last start is
		// the last label, and the MAX stays one seek on states_account_kind_start.
		q = `SELECT date(MAX(start_at), '+12 hours') FROM states WHERE account_id = ? AND kind = ?`
		args = append(args, sleepKind)
	case metric.Aggregation == catalog.SumByState && metric.Slug == metricTrainingDistance:
		q = `SELECT date(MAX(start_at)) FROM sessions WHERE account_id = ? AND total_distance IS NOT NULL`
	case metric.Aggregation == catalog.SumByState:
		q = `SELECT date(MAX(start_at)) FROM sessions WHERE account_id = ?`
	default:
		// MAX over measurements_account_metric_start is one seek to the end of the
		// Metric's range, which is why the date() goes outside it.
		q = `SELECT date(MAX(start_at)) FROM measurements WHERE account_id = ? AND metric = ?`
		args = append(args, metric.Slug)
	}

	var day *string
	if err := e.DB.QueryRowContext(ctx, q, args...).Scan(&day); err != nil {
		return "", fmt.Errorf("query: last day: %w", err)
	}
	if day == nil {
		return "", nil
	}
	return *day, nil
}
