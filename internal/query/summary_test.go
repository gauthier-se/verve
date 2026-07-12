package query

import (
	"context"
	"testing"
)

// summaryReq is a day-bucketed Request over [from, to) for the summary tests.
func summaryReq(t *testing.T, acc int64, metric, from, to string) Request {
	t.Helper()
	return Request{
		AccountID: acc, Metric: metric, Bucket: Day,
		From: mustTime(t, from), To: mustTime(t, to),
	}
}

func mustSeries(t *testing.T, e Engine, r Request) Series {
	t.Helper()
	s, err := e.Series(context.Background(), r)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	return s
}

func meanOfPoints(pts []Point) float64 {
	if len(pts) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range pts {
		sum += p.Value
	}
	return sum / float64(len(pts))
}

func TestSummarySumTotalsWholeWindow(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-01T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-01T18:00:00Z", "Watch"},
		{"steps", 50, "2024-01-02T09:00:00Z", "Watch"},
	})
	s := mustSeries(t, e, summaryReq(t, acc, "steps", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z"))
	if s.Summary == nil || s.Summary.Value != 350 {
		t.Fatalf("summary = %+v, want value 350", s.Summary)
	}
}

// The summary of an average Metric is a true count-weighted mean over the raw rows,
// not a mean of per-bucket means (ADR 0019). Unequal per-day counts make the two
// diverge: three 60s on day 1 and one 180 on day 2 give a weighted mean of 90, while
// a mean of the daily means (60, 180) would be 120 — the wrong answer.
func TestSummaryAverageIsCountWeightedNotMeanOfMeans(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"heart_rate", 60, "2024-01-01T08:00:00Z", "Watch"},
		{"heart_rate", 60, "2024-01-01T12:00:00Z", "Watch"},
		{"heart_rate", 60, "2024-01-01T18:00:00Z", "Watch"},
		{"heart_rate", 180, "2024-01-02T09:00:00Z", "Watch"},
	})
	s := mustSeries(t, e, summaryReq(t, acc, "heart_rate", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z"))
	if got := meanOfPoints(s.Points); got != 120 {
		t.Fatalf("mean of per-bucket means = %v, want 120 (the biased figure the summary must avoid)", got)
	}
	if s.Summary == nil || s.Summary.Value != 90 {
		t.Fatalf("summary = %+v, want count-weighted mean 90 (not 120)", s.Summary)
	}
	if s.Summary.Min == nil || *s.Summary.Min != 60 || s.Summary.Max == nil || *s.Summary.Max != 180 {
		t.Fatalf("summary band = %v/%v, want 60/180", s.Summary.Min, s.Summary.Max)
	}
}

func TestSummaryLatestIsWindowLastValue(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"body_mass", 75.0, "2024-01-01T08:00:00Z", "Scale"},
		{"body_mass", 74.6, "2024-01-03T08:00:00Z", "Scale"},
		{"body_mass", 74.2, "2024-01-05T08:00:00Z", "Scale"},
	})
	s := mustSeries(t, e, summaryReq(t, acc, "body_mass", "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z"))
	if s.Summary == nil || s.Summary.Value != 74.2 {
		t.Fatalf("summary = %+v, want last value 74.2", s.Summary)
	}
}

func TestSummaryEmptyRangeIsNil(t *testing.T) {
	e, _, acc := setup(t)
	s := mustSeries(t, e, summaryReq(t, acc, "steps", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z"))
	if s.Summary != nil {
		t.Fatalf("summary = %+v, want nil for an empty range", s.Summary)
	}
	if len(s.Points) != 0 {
		t.Fatalf("points = %+v, want empty (non-nil)", s.Points)
	}
}

// A derived Metric's summary aggregates each operand over the window then applies the
// Formula once (ADR 0019): calorie_balance = Σenergy − Σactive − Σbasal.
func TestSummaryDerivedFoldsOperandsThenFormula(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"dietary_energy", 2000, "2024-01-01T12:00:00Z", "App"},
		{"dietary_energy", 2200, "2024-01-02T12:00:00Z", "App"},
		{"active_energy", 500, "2024-01-01T18:00:00Z", "Watch"},
		{"active_energy", 600, "2024-01-02T18:00:00Z", "Watch"},
		{"basal_energy", 1500, "2024-01-01T00:30:00Z", "Watch"},
		{"basal_energy", 1500, "2024-01-02T00:30:00Z", "Watch"},
	})
	// Window sums: energy 4200, active 1100, basal 3000 → balance 100.
	s := mustSeries(t, e, summaryReq(t, acc, "calorie_balance", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z"))
	if s.Summary == nil || s.Summary.Value != 100 {
		t.Fatalf("summary = %+v, want 4200 − 1100 − 3000 = 100", s.Summary)
	}
}

// A derived ratio's summary is the period's real ratio — numerator and denominator
// each folded over the window, divided once — not a mean of per-bucket ratios.
func TestSummaryDerivedRatioIsWindowRatioNotMeanOfRatios(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"dietary_protein", 100, "2024-01-01T12:00:00Z", "App"},
		{"dietary_protein", 200, "2024-01-02T12:00:00Z", "App"},
		{"body_mass", 100, "2024-01-01T08:00:00Z", "Scale"},
		{"body_mass", 50, "2024-01-02T08:00:00Z", "Scale"},
	})
	// Per-day ratios are 100/100 = 1 and 200/50 = 4 (the Points); their mean 2.5 is
	// the wrong answer. Window ratio = Σprotein / latest(body_mass) = 300 / 50 = 6.
	s := mustSeries(t, e, summaryReq(t, acc, "protein_per_kg", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z"))
	if got := meanOfPoints(s.Points); got != 2.5 {
		t.Fatalf("mean of per-bucket ratios = %v, want 2.5 (the figure the summary must avoid)", got)
	}
	if s.Summary == nil || s.Summary.Value != 6 {
		t.Fatalf("summary = %+v, want window ratio 300/50 = 6 (not mean-of-ratios 2.5)", s.Summary)
	}
}

// A required operand absent over the whole window makes the derived summary a gap
// (nil), never a zero — the ADR 0014 gap rule at window scope.
func TestSummaryDerivedMissingOperandIsNil(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"dietary_energy", 2000, "2024-01-01T12:00:00Z", "App"},
		{"active_energy", 500, "2024-01-01T18:00:00Z", "Watch"},
		// basal_energy absent → calorie_balance undefined over the window.
	})
	s := mustSeries(t, e, summaryReq(t, acc, "calorie_balance", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z"))
	if s.Summary != nil {
		t.Fatalf("summary = %+v, want nil (a required operand absent over the window)", s.Summary)
	}
}

// Comparison carries a summary on both windows so the client can render the delta.
func TestCompareCarriesBothSummaries(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 300, "2024-01-08T09:00:00Z", "Watch"}, // current week
		{"steps", 200, "2024-01-01T09:00:00Z", "Watch"}, // baseline week
	})
	cmp, err := e.Compare(context.Background(),
		summaryReq(t, acc, "steps", "2024-01-08T00:00:00Z", "2024-01-15T00:00:00Z"),
		mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if cmp.Current.Summary == nil || cmp.Current.Summary.Value != 300 {
		t.Fatalf("current summary = %+v, want 300", cmp.Current.Summary)
	}
	if cmp.Baseline.Summary == nil || cmp.Baseline.Summary.Value != 200 {
		t.Fatalf("baseline summary = %+v, want 200", cmp.Baseline.Summary)
	}
}
