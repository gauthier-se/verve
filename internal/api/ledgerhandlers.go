package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/query"
)

// Ledger overview windows (ADR 0021): the scoreboard's fixed columns are the last
// 7 days ("~7-day"), the last 30 days ("~30-day"), and a week-over-week delta (this
// 7 days vs the preceding 7). Latest is read from the 30-day window's last bucket.
const (
	ledgerWeekDays  = 7
	ledgerMonthDays = 30
)

// ledgerValue is a Metric's most recent daily value with the day it fell on.
type ledgerValue struct {
	Value float64 `json:"value"`
	Date  string  `json:"date"`
}

// ledgerRow is one Metric's line in the Ledger overview (ADR 0021): its latest
// value and folded window figures, plus a week-over-week delta. A nil figure is a
// gap ("—") the client renders as no data. For a `sum` Metric the week/month figures
// are daily averages (the window fold ÷ its day count), so steps/calories read as a
// per-day number, not a window total.
type ledgerRow struct {
	Metric      string       `json:"metric"`
	Unit        string       `json:"unit"`
	Aggregation string       `json:"aggregation"`
	Latest      *ledgerValue `json:"latest,omitempty"`
	Week        *float64     `json:"week,omitempty"`
	Month       *float64     `json:"month,omitempty"`
	DeltaAbs    *float64     `json:"delta_abs,omitempty"`
	DeltaPct    *float64     `json:"delta_pct,omitempty"`
}

// handleLedger answers the Ledger overview (ADR 0021): one row per Metric the
// Account has Measurements for, each folded server-side over fixed recent windows by
// the Metric's own rule (reusing the query engine, so figures match the graphs).
// Metrics the engine cannot serve yet (e.g. duration_by_state) are skipped, never an
// error. The per-Metric detail table uses GET /v1/series, not this endpoint.
func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	slugs, err := s.models.Measurements.DistinctMetrics(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	now := time.Now().UTC()
	rows := make([]ledgerRow, 0, len(slugs))
	for _, slug := range slugs {
		metric, ok := catalog.Lookup(slug)
		if !ok {
			continue // a stored slug outside the Catalog has no rule to fold by
		}
		row, ok, err := s.ledgerRow(r.Context(), accountID, metric, now)
		if err != nil {
			s.serverErrorResponse(w, r, err)
			return
		}
		if ok {
			rows = append(rows, row)
		}
	}

	if err := writeJSON(w, http.StatusOK, envelope{"rows": rows}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// ledgerRow folds one Metric over the Ledger's fixed windows. It runs two engine
// queries: a week-over-week Compare (this 7 days vs the preceding 7) for the week
// figure and delta, and a 30-day series for the month figure and the latest value.
// It returns ok=false for a Metric whose aggregation the engine does not serve.
func (s *Server) ledgerRow(ctx context.Context, accountID int64, metric catalog.Metric, now time.Time) (ledgerRow, bool, error) {
	req := query.Request{AccountID: accountID, Metric: metric.Slug, Bucket: query.Day}

	weekReq := req
	weekReq.From, weekReq.To = now.AddDate(0, 0, -ledgerWeekDays), now
	cmp, err := s.engine.Compare(ctx, weekReq, now.AddDate(0, 0, -2*ledgerWeekDays), now.AddDate(0, 0, -ledgerWeekDays))
	if err != nil {
		if errors.Is(err, query.ErrUnsupportedAggregation) {
			return ledgerRow{}, false, nil
		}
		return ledgerRow{}, false, err
	}

	monthReq := req
	monthReq.From, monthReq.To = now.AddDate(0, 0, -ledgerMonthDays), now
	month, err := s.engine.Series(ctx, monthReq)
	if err != nil {
		return ledgerRow{}, false, err
	}

	row := ledgerRow{
		Metric:      metric.Slug,
		Unit:        metric.Unit,
		Aggregation: string(metric.Aggregation),
		Week:        foldFigure(cmp.Current.Summary, metric.Aggregation, ledgerWeekDays),
		Month:       foldFigure(month.Summary, metric.Aggregation, ledgerMonthDays),
		Latest:      latestValue(month.Points),
	}

	// Week-over-week delta, folded on the same basis as the week figure so an
	// absolute delta for a `sum` Metric is a per-day change. Percentage is always the
	// ratio of folds; the client shows percent unless the Metric is signed (ADR 0019).
	base := foldFigure(cmp.Baseline.Summary, metric.Aggregation, ledgerWeekDays)
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

// foldFigure projects a window's Panel summary to a scoreboard figure: nil stays a
// gap; a `sum` Metric's total is divided by the window's day count to read as a daily
// average, every other rule keeps its own fold (ADR 0021).
func foldFigure(summary *query.Point, agg catalog.Aggregation, windowDays float64) *float64 {
	if summary == nil {
		return nil
	}
	v := summary.Value
	if agg == catalog.Sum {
		v /= windowDays
	}
	return &v
}

// latestValue is the most recent daily value in a series: the last Point (the engine
// returns Points in ascending bucket order and omits empty buckets), or nil when the
// window held no data.
func latestValue(points []query.Point) *ledgerValue {
	if len(points) == 0 {
		return nil
	}
	last := points[len(points)-1]
	return &ledgerValue{Value: last.Value, Date: last.Bucket}
}
