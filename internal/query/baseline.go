package query

import (
	"context"
	"time"
)

// Comparison is a current series overlaid with its Baseline, ordinal-aligned and
// truncated to equal length (ADR 0015). Each Baseline Point keeps its own date.
type Comparison struct {
	Current  Series `json:"current"`
	Baseline Series `json:"baseline"`
}

// Compare overlays the current window with a caller-resolved Baseline window
// (timeaxis owns the rule→window math), running the bucket query for both at the
// same granularity, then aligning by ordinal position, truncated to the shorter.
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

// alignOrdinal overlays the Baseline on the current series by ordinal position
// within each window — "day 1 vs day 1" (ADR 0015), keyed on each bucket's index
// in the window's start sequence (not its slice position, since a query omits
// empty buckets). Both windows truncate to the shorter's count; a Baseline bucket
// with no data at an ordinal becomes a dated Gap, never a zero.
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
