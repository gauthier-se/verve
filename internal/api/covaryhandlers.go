package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// maxCoVaryMetrics caps how many Metrics are paired at once. The read is
// quadratic in this number — 8 Metrics is 56 ordered pairs over one window — and
// a matrix wider than that stops being readable long before it stops being
// affordable. Pins beyond the cap are dropped from the tail, in Pin order, so the
// page shrinks predictably rather than failing.
const maxCoVaryMetrics = 8

// coVaryLag is a lag preset: the grain a pair is read at and how far the second
// Metric is shifted on it. Grain and shift travel together because that is how the
// question is actually asked — "the next morning" is a day-grain question, "the
// week after" a week-grain one — and letting them be chosen separately would offer
// combinations ("+1 day on monthly buckets") that mean nothing.
type coVaryLag struct {
	// Bucket overrides the window's own bucket; empty keeps it.
	Bucket string
	// Shift is how many buckets the second Metric moves forward.
	Shift int
}

var coVaryLags = map[string]coVaryLag{
	"same":      {Bucket: "", Shift: 0},
	"next_day":  {Bucket: "day", Shift: 1},
	"next_week": {Bucket: "week", Shift: 1},
}

// coVaryView is the cross-metric answer as the client reads it: the resolved axis
// it was computed on, the pairs, and the strongest pair drawn.
type coVaryView struct {
	Range     windowView          `json:"range"`
	Bucket    string              `json:"bucket"`
	Lag       string              `json:"lag"`
	LagShift  int                 `json:"lag_shift"`
	Metrics   []string            `json:"metrics"`
	Units     map[string]string   `json:"units"`
	Pairs     []query.Pair        `json:"pairs"`
	MinShared int                 `json:"min_shared"`
	Strongest *query.Scatter      `json:"strongest,omitempty"`
	Skipped   []coVarySkippedView `json:"skipped,omitempty"`
}

// coVarySkippedView names a pinned Metric that could not join the matrix, and why.
// It is reported rather than silently dropped: a Metric the Account pinned and does
// not find on this page is a bug until the page says otherwise.
type coVarySkippedView struct {
	Metric string `json:"metric"`
	Reason string `json:"reason"`
}

// handleCoVary answers the cross-metric read: the Account's pinned Metrics paired
// over one window at one lag (ADR 0025 — the Pins are the page's input, so the way
// to put a Metric on it is to pin it, and there is no second list to curate).
//
// Everything the page prints is computed here: the coefficients, the ranking, the
// threshold under which a pair is not ranked, the fitted line. The client draws.
func (s *Server) handleCoVary(w http.ResponseWriter, r *http.Request) {
	accountID, _ := s.accountID(r)

	qs := r.URL.Query()
	v := NewValidator()

	lagName := qs.Get("lag")
	if lagName == "" {
		lagName = "same"
	}
	lag, ok := coVaryLags[lagName]
	v.Check(ok, "lag", "must be one of same, next_day, next_week")

	tokens := timeaxis.Tokens{
		RangePreset: qs.Get("range_preset"),
		RangeFrom:   optionalParam(qs, "range_from"),
		RangeTo:     optionalParam(qs, "range_to"),
	}
	// The lag's grain overrides the window's own, exactly as a Panel's bucket
	// override does, so the whole axis is still resolved by the one module that owns
	// boundaries rather than half-derived here.
	if lag.Bucket != "" {
		bucket := lag.Bucket
		tokens.Bucket = &bucket
	}
	resolved, err := timeaxis.Resolve(tokens, time.Now())
	if inv, ok := err.(timeaxis.Invalid); ok {
		for field, msg := range inv {
			v.AddError(field, msg)
		}
	} else if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}
	if !v.Valid() {
		s.failedValidationResponse(w, r, v.Errors)
		return
	}

	pins, err := s.models.Pins.ListByAccount(r.Context(), accountID)
	if err != nil {
		s.serverErrorResponse(w, r, err)
		return
	}

	metrics, skipped := coVaryInputs(pins)

	covary, err := s.engine.CoVary(r.Context(), query.CoVaryRequest{
		AccountID: accountID, Metrics: metrics,
		From: resolved.Current.From, To: resolved.Current.To,
		Bucket: resolved.Bucket, Lag: lag.Shift,
	})
	if err != nil {
		s.respondSeriesError(w, r, err)
		return
	}

	// A Metric that survived the Catalog check but held no bucket in the window is
	// named too: "you pinned it, it has nothing here" is the answer, not an absence.
	held := make(map[string]bool, len(covary.Metrics))
	for _, slug := range covary.Metrics {
		held[slug] = true
	}
	for _, slug := range metrics {
		if !held[slug] {
			skipped = append(skipped, coVarySkippedView{Metric: slug, Reason: "no data in this window"})
		}
	}

	view := coVaryView{
		Range: toWindowView(resolved.Current), Bucket: string(covary.Bucket),
		Lag: lagName, LagShift: lag.Shift,
		Metrics: covary.Metrics, Units: covary.Units, Pairs: covary.Pairs,
		MinShared: covary.MinShared, Strongest: covary.Strongest, Skipped: skipped,
	}
	if err := writeJSON(w, http.StatusOK, envelope{"covary": view}, nil); err != nil {
		s.serverErrorResponse(w, r, err)
	}
}

// coVaryInputs turns the Account's Pins into the Metric list to pair, dropping
// what cannot be paired and saying why. A Pin whose slug has left the Catalog is
// kept in the store (it comes back if the Metric does, ADR 0025) but has no rule to
// read it by here.
func coVaryInputs(pins []data.Pin) ([]string, []coVarySkippedView) {
	metrics := make([]string, 0, len(pins))
	skipped := []coVarySkippedView{}
	for _, pin := range pins {
		if _, ok := catalog.Lookup(pin.Metric); !ok {
			skipped = append(skipped, coVarySkippedView{Metric: pin.Metric, Reason: "not in the catalog"})
			continue
		}
		if len(metrics) >= maxCoVaryMetrics {
			skipped = append(skipped, coVarySkippedView{
				Metric: pin.Metric,
				Reason: fmt.Sprintf("beyond the %d metrics this page pairs", maxCoVaryMetrics),
			})
			continue
		}
		metrics = append(metrics, pin.Metric)
	}
	return metrics, skipped
}
