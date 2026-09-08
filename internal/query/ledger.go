package query

import (
	"context"
	"errors"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// Ledger overview windows (ADR 0021): the scoreboard's columns are the last 7 days
// ("~7-day"), the last 30 days ("~30-day"), and a week-over-week delta (this 7 days
// vs the preceding 7). They are constants rather than parameters because ADR 0021
// fixes them: the Ledger is one reading, not a range the caller picks.
const (
	ledgerWeekDays  = 7
	ledgerMonthDays = 30
)

// LedgerValue is a Metric's most recent daily value with the day it fell on.
type LedgerValue struct {
	Value float64 `json:"value"`
	Date  string  `json:"date"`
}

// LedgerRow is one Metric's line in the Ledger overview (ADR 0021): its latest
// value and folded window figures, plus a week-over-week delta. A nil figure is a
// gap ("—") the client renders as no data. For a `sum` Metric the week/month
// figures are daily averages (the window fold ÷ its day count), so steps and
// calories read as a per-day number rather than a window total; sleep is the same
// shape per Night (ADR 0027).
type LedgerRow struct {
	Metric      string              `json:"metric"`
	Unit        string              `json:"unit"`
	Aggregation catalog.Aggregation `json:"aggregation"`
	Latest      *LedgerValue        `json:"latest,omitempty"`
	Week        *float64            `json:"week,omitempty"`
	Month       *float64            `json:"month,omitempty"`
	DeltaAbs    *float64            `json:"delta_abs,omitempty"`
	DeltaPct    *float64            `json:"delta_pct,omitempty"`
}

// Ledger folds every Metric the Account holds data for over the overview's fixed
// windows (ADR 0021), one row each, in Catalog-slug order.
//
// now is a parameter rather than a call to time.Now so the windows are decided by
// the caller and pinned by a test. A Metric whose aggregation the engine cannot
// serve is skipped rather than failing the page, and a slug outside the Catalog is
// skipped too: it has no rule to fold by.
func (e Engine) Ledger(ctx context.Context, accountID int64, now time.Time) ([]LedgerRow, error) {
	slugs, err := e.MetricsWithData(ctx, accountID)
	if err != nil {
		return nil, err
	}

	rows := make([]LedgerRow, 0, len(slugs))
	for _, slug := range slugs {
		metric, ok := catalog.Lookup(slug)
		if !ok {
			continue // a stored slug outside the Catalog has no rule to fold by
		}
		row, ok, err := e.ledgerRow(ctx, accountID, metric, now)
		if err != nil {
			return nil, err
		}
		if ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// ledgerRow folds one Metric over the Ledger's fixed windows. It runs two engine
// queries: a week-over-week Compare (this 7 days vs the preceding 7) for the week
// figure and delta, and a 30-day series for the month figure and the latest value.
// ok is false for a Metric whose aggregation the engine does not serve.
func (e Engine) ledgerRow(ctx context.Context, accountID int64, metric catalog.Metric, now time.Time) (LedgerRow, bool, error) {
	req := Request{AccountID: accountID, Metric: metric.Slug, Bucket: timeaxis.Day}

	weekReq := req
	weekReq.From, weekReq.To = now.AddDate(0, 0, -ledgerWeekDays), now
	baseline := timeaxis.Window{
		From: now.AddDate(0, 0, -2*ledgerWeekDays),
		To:   now.AddDate(0, 0, -ledgerWeekDays),
	}
	cmp, err := e.Compare(ctx, weekReq, baseline)
	if err != nil {
		if errors.Is(err, ErrUnsupportedAggregation) {
			return LedgerRow{}, false, nil
		}
		return LedgerRow{}, false, err
	}

	monthReq := req
	monthReq.From, monthReq.To = now.AddDate(0, 0, -ledgerMonthDays), now
	month, err := e.Series(ctx, monthReq)
	if err != nil {
		return LedgerRow{}, false, err
	}

	row := LedgerRow{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: metric.Aggregation,
		Week:        foldFigure(cmp.Current),
		Month:       foldFigure(month),
		Latest:      latestValue(month.Points),
	}

	// Week-over-week delta, folded on the same basis as the week figure so an
	// absolute delta for a `sum` Metric is a per-day change. Percentage is always the
	// ratio of folds; the client shows percent unless the Metric is signed (ADR 0019).
	base := foldFigure(cmp.Baseline)
	if row.Week != nil && base != nil {
		abs := *row.Week - *base
		row.DeltaAbs = &abs
		if *base != 0 {
			pct := abs / *base * 100
			row.DeltaPct = &pct
		}
	}
	return row, true, nil
}

// foldFigure reduces a Series' window summary to the figure the Ledger prints:
// a rate for a Metric that accumulates, the summary itself for one that does not.
//
// The divisor is the Series' own, not the window's nominal length: a `sum` reads
// per day and divides by the days the window actually spans, while sleep reads per
// night and divides by the Nights that hold data (ADR 0027). Dividing a month of
// sleep by 30 when the Watch was off for nine of them reports a shortfall the
// Account never had, with the confidence of a computed number.
func foldFigure(s Series) *float64 {
	if s.Summary == nil {
		return nil
	}
	v := s.Summary.Value
	switch s.Aggregation {
	case catalog.Sum:
		if s.Days == 0 {
			return nil
		}
		v /= float64(s.Days)
	case catalog.DurationByState:
		if s.Nights == 0 {
			return nil // a summary with no Nights behind it has no per-night figure
		}
		v /= float64(s.Nights)
	}
	return &v
}

// latestValue is the most recent daily value in a series: the last Point (the
// engine returns Points in ascending bucket order and omits empty buckets), or nil
// when the window held no data.
func latestValue(points []Point) *LedgerValue {
	if len(points) == 0 {
		return nil
	}
	last := points[len(points)-1]
	return &LedgerValue{Value: last.Value, Date: last.Bucket}
}
