package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
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

// sleepSlug is the Catalog Metric read from the States family, and also the
// states.kind that feeds it (ADR 0027).
const sleepSlug = "sleep"

// ledgerValue is a Metric's most recent daily value with the day it fell on.
type ledgerValue struct {
	Value float64 `json:"value"`
	Date  string  `json:"date"`
}

// ledgerRow is one Metric's line in the Ledger overview (ADR 0021): its latest
// value and folded window figures, plus a week-over-week delta. A nil figure is a
// gap ("—") the client renders as no data. For a `sum` Metric the week/month figures
// are daily averages (the window fold ÷ its day count), so steps/calories read as a
// per-day number, not a window total; sleep is the same shape per Night (ADR 0027).
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
// Account has data for, each folded server-side over fixed recent windows by the
// Metric's own rule (reusing the query engine, so figures match the graphs). A rule
// the engine cannot serve is skipped, never an error. The per-Metric detail table
// uses GET /v1/series, not this endpoint.
func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	slugs, err := s.models.Measurements.DistinctMetrics(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	// Sleep is read from the States family (ADR 0027), which DistinctMetrics cannot
	// see. Without this the one page that promises the numbers behind the curves
	// would be the one page missing sleep.
	slugs, err = s.withSleep(r.Context(), accountID, slugs)
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

// withSleep adds the sleep slug to a Ledger row set when the Account has any sleep
// State, keeping the listing sorted. It is a no-op for an Account with none, and for
// the slug already being present — nothing writes a sleep Measurement (the Manual
// entry path refuses it), but a row set must not gain a duplicate on the day
// something does.
func (s *Server) withSleep(ctx context.Context, accountID int64, slugs []string) ([]string, error) {
	if slices.Contains(slugs, sleepSlug) {
		return slugs, nil
	}
	has, err := s.models.States.HasStates(ctx, accountID, sleepSlug)
	if err != nil {
		return nil, err
	}
	if !has {
		return slugs, nil
	}
	slugs = append(slugs, sleepSlug)
	slices.Sort(slugs)
	return slugs, nil
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
		Week:        foldFigure(cmp.Current, ledgerWeekDays),
		Month:       foldFigure(month, ledgerMonthDays),
		Latest:      latestValue(month.Points),
	}

	// Week-over-week delta, folded on the same basis as the week figure so an
	// absolute delta for a `sum` Metric is a per-day change. Percentage is always the
	// ratio of folds; the client shows percent unless the Metric is signed (ADR 0019).
	base := foldFigure(cmp.Baseline, ledgerWeekDays)
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
// gap; an accumulating rule is divided by the count of the thing it accumulates over,
// every other rule keeps its own fold (ADR 0021).
//
// The divisor is the Metric's own, not the window's length: a `sum` reads per day, so
// it divides by the window's days, while sleep reads per night and divides by the
// Nights that actually hold data (ADR 0027). Dividing a month of sleep by 30 when the
// Watch was off for nine of them reports a shortfall the Account never had.
func foldFigure(s query.Series, windowDays float64) *float64 {
	if s.Summary == nil {
		return nil
	}
	v := s.Summary.Value
	switch s.Aggregation {
	case catalog.Sum:
		v /= windowDays
	case catalog.DurationByState:
		if s.Nights == 0 {
			return nil // a summary with no Nights behind it has no per-night figure
		}
		v /= float64(s.Nights)
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
