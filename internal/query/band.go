package query

import (
	"context"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// Span is a run of buckets on the grid, both ends inclusive and named by their
// bucket keys.
type Span struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Band is one Metric drawn over a whole span as a *dense* series: one Point per
// bucket from the first to the last, gaps included and marked, with the runs of
// empty buckets named separately so a reader can shade a stretch without scanning
// for one.
//
// Dense is the whole point. Everywhere else a bucket without data is simply absent
// from a Series, because a gap is never a zero (ADR 0014) and a chart must not draw
// through one. Here the gap *is* the subject — "you changed phones in April 2022
// and nothing was recorded for five weeks" is a fact about the history — so the
// grid is materialised and each empty bucket says so (ADR 0032).
type Band struct {
	Metric string  `json:"metric"`
	Unit   string  `json:"unit"`
	Bucket string  `json:"bucket"`
	Points []Point `json:"points"`
	Gaps   []Span  `json:"gaps"`
	// Grid is the bucket keys the band drew, in order. It is the axis anything
	// folded onto this band must be placed against, so a caller never computes
	// "which bucket holds this day" for itself.
	Grid []string `json:"-"`
}

// BandRequest is one dense read: a Metric over the inclusive day span [First,
// Last], at a grain derived from that span.
//
// The bounds are inclusive days rather than a half-open window because they are
// the first and last thing an Account recorded, not a range someone chose. They
// are snapped before anything is enumerated: a history whose last reading is at
// 08:00 would otherwise get one bucket too many, drawn as a gap.
type BandRequest struct {
	AccountID   int64
	Metric      string
	First, Last time.Time
}

// Band reads a Metric over its whole span and materialises the grid (ADR 0032).
//
// The grain is the span's own, by the same rule that gives a Dashboard its bucket
// (timeaxis.AutoBucket) applied to the real extent of the history rather than to a
// preset: eight years come out monthly, and an Account three months old comes out
// weekly, which is the right answer for it and not a coarser one borrowed from
// someone else's history.
func (e Engine) Band(ctx context.Context, req BandRequest) (Band, error) {
	first, last := truncateDay(req.First), truncateDay(req.Last)
	bucket := timeaxis.AutoBucket(timeaxis.Window{From: first, To: last})
	to := last.AddDate(0, 0, 1) // the window is half-open; include the last day

	series, err := e.Series(ctx, Request{
		AccountID: req.AccountID, Metric: req.Metric,
		From: first, To: to, Bucket: bucket,
	})
	if err != nil {
		return Band{}, err
	}

	grid := bucket.Starts(first, to)
	band := Band{
		Metric: req.Metric, Unit: series.Unit, Bucket: string(bucket),
		Points: make([]Point, 0, len(grid)), Gaps: []Span{}, Grid: grid,
	}

	byBucket := make(map[string]Point, len(series.Points))
	for _, p := range series.Points {
		byBucket[p.Bucket] = p
	}

	var run *Span
	for _, key := range grid {
		if p, ok := byBucket[key]; ok {
			band.Points = append(band.Points, p)
			if run != nil {
				band.Gaps = append(band.Gaps, *run)
				run = nil
			}
			continue
		}
		band.Points = append(band.Points, Point{Bucket: key, Gap: true})
		if run == nil {
			run = &Span{From: key, To: key}
		} else {
			run.To = key
		}
	}
	if run != nil {
		band.Gaps = append(band.Gaps, *run)
	}

	return band, nil
}

// truncateDay rounds an instant down to UTC midnight.
func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
