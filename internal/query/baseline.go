package query

import (
	"context"
	"time"
)

// Comparison is a current series overlaid with its Baseline, aligned by ordinal
// bucket position and truncated to equal length (ADR 0015). Current and Baseline
// share the same bucket granularity; each Baseline Point keeps its own real date
// so a tooltip can show which earlier bucket it came from.
type Comparison struct {
	Current  Series `json:"current"`
	Baseline Series `json:"baseline"`
}

// Compare overlays the current window with a Baseline window the caller has
// already resolved (internal/timeaxis owns the rule→window date math). It runs the
// bucket query for both windows at the same granularity — the derived path
// included, each operand resolving its own Source (ADR 0003) — then aligns them by
// ordinal position, truncating both to the shorter (ADR 0015).
func (e Engine) Compare(ctx context.Context, req Request, baseFrom, baseTo time.Time) (Comparison, error) {
	current, err := e.Series(ctx, req)
	if err != nil {
		return Comparison{}, err
	}

	breq := req
	breq.From, breq.To = baseFrom, baseTo
	baseline, err := e.Series(ctx, breq)
	if err != nil {
		return Comparison{}, err
	}

	alignOrdinal(req.Bucket, &current, &baseline, req.From, req.To, baseFrom, baseTo)
	return Comparison{Current: current, Baseline: baseline}, nil
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
