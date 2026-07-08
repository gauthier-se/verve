package query

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnknownBaselineRule is returned when a Baseline carries a rule the engine
// does not resolve. Comparison-off (`none`) and the `all` range are guarded
// upstream (ADR 0015), so a rule reaching the engine must be one of the three
// active rules; anything else is a caller error, not a silent no-comparison.
var ErrUnknownBaselineRule = errors.New("query: unknown baseline rule")

// BaselineRule selects how the Baseline window is derived from the current range
// (ADR 0015, CONTEXT.md: Baseline rule). The relative rules are recomputed from
// the current range; only custom is absolute. `none` never reaches the engine —
// it means comparison is off and is handled at the edge.
type BaselineRule string

const (
	// BaselinePrevious shifts the current range back by its own length.
	BaselinePrevious BaselineRule = "previous"
	// BaselineSamePeriodLastYear shifts the current range back one calendar year.
	BaselineSamePeriodLastYear BaselineRule = "same_period_last_year"
	// BaselineCustom uses absolute frozen from/to bounds, ignoring the range.
	BaselineCustom BaselineRule = "custom"
)

// Baseline describes the second, earlier window a comparison reads against
// (CONTEXT.md: Baseline). From and To are the absolute bounds and are used only
// when Rule is BaselineCustom; the relative rules derive their window from the
// request's own range and ignore them.
type Baseline struct {
	Rule BaselineRule
	From time.Time
	To   time.Time
}

// Comparison is a current series overlaid with its Baseline, aligned by ordinal
// bucket position and truncated to equal length (ADR 0015). Current and Baseline
// share the same bucket granularity; each Baseline Point keeps its own real date
// so a tooltip can show which earlier bucket it came from.
type Comparison struct {
	Current  Series `json:"current"`
	Baseline Series `json:"baseline"`
}

// SeriesWithBaseline answers a comparison query: the current window plus a
// Baseline window overlaid on it. It runs the existing bucket query for both
// windows at the same granularity — including the derived path unchanged, where
// each operand resolves its own winning Source (ADR 0003) — then aligns them by
// ordinal position, truncating both to the shorter window (ADR 0015). The `all`
// range and the `none` rule are rejected upstream, so a Baseline reaching here
// always names one of the three active rules.
func (e Engine) SeriesWithBaseline(ctx context.Context, req Request, b Baseline) (Comparison, error) {
	current, err := e.Series(ctx, req)
	if err != nil {
		return Comparison{}, err
	}

	from, to, err := baselineWindow(req, b)
	if err != nil {
		return Comparison{}, err
	}
	breq := req
	breq.From, breq.To = from, to
	baseline, err := e.Series(ctx, breq)
	if err != nil {
		return Comparison{}, err
	}

	alignOrdinal(req.Bucket, &current, &baseline, req.From, req.To, from, to)
	return Comparison{Current: current, Baseline: baseline}, nil
}

// baselineWindow resolves the concrete [from, to) of the Baseline window from the
// current range and the rule, calendar-aware:
//   - previous: shift both bounds back by the range's own length, giving the
//     window immediately before the current one, same length.
//   - same_period_last_year: shift both bounds back one calendar year (AddDate
//     normalizes a Feb 29 start to Mar 1 in a non-leap year).
//   - custom: the absolute bounds as given.
func baselineWindow(req Request, b Baseline) (from, to time.Time, err error) {
	switch b.Rule {
	case BaselinePrevious:
		span := req.To.Sub(req.From)
		return req.From.Add(-span), req.To.Add(-span), nil
	case BaselineSamePeriodLastYear:
		return req.From.AddDate(-1, 0, 0), req.To.AddDate(-1, 0, 0), nil
	case BaselineCustom:
		return b.From, b.To, nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %q", ErrUnknownBaselineRule, b.Rule)
	}
}

// alignOrdinal overlays the Baseline on the current series by ordinal bucket
// position within each window — "day 1 vs day 1" (ADR 0015), not by date, since
// the dates differ by construction. It maps each series' points onto the ordinal
// of its bucket (its index in the window's start sequence), not the point's
// position in the compacted slice: because a normal query omits empty buckets, a
// window with an interior gap would otherwise misalign every bucket after it.
//
// Both windows are truncated to the shorter's bucket count: current points past
// that ordinal (a leap-day boundary or a longer custom span) are dropped, and the
// Baseline is rebuilt so index i names the same ordinal as current point i. A
// Baseline bucket with no data at that ordinal becomes a Gap slot that keeps its
// own real date — so the overlay stays index-aligned and an empty Baseline window
// yields an all-gap (effectively absent) series rather than shrinking the current
// window to nothing.
func alignOrdinal(bucket Bucket, current, baseline *Series, curFrom, curTo, baseFrom, baseTo time.Time) {
	curStarts := bucket.starts(curFrom, curTo)
	baseStarts := bucket.starts(baseFrom, baseTo)
	n := min(len(curStarts), len(baseStarts)) // the shorter window's bucket count

	ordinal := make(map[string]int, len(curStarts))
	for i, s := range curStarts {
		ordinal[s] = i
	}
	baseByDate := make(map[string]Point, len(baseline.Points))
	for _, p := range baseline.Points {
		baseByDate[p.Bucket] = p
	}

	keptCur := make([]Point, 0, len(current.Points))
	aligned := make([]Point, 0, len(current.Points))
	for _, cp := range current.Points {
		k, ok := ordinal[cp.Bucket]
		if !ok || k >= n {
			continue // an orphan bucket beyond the shorter window has no counterpart
		}
		keptCur = append(keptCur, cp)
		bs := baseStarts[k]
		if bp, ok := baseByDate[bs]; ok {
			aligned = append(aligned, bp) // real Baseline bucket at this ordinal, own date
		} else {
			aligned = append(aligned, Point{Bucket: bs, Gap: true}) // no data: a dated gap
		}
	}
	current.Points = keptCur
	baseline.Points = aligned
}
