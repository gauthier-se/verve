package query

import (
	"context"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
)

// The Manual overlay (ADR 0022): on a day the Account typed a value for a Metric,
// that day's Manual rows replace the winning Source's rows; every other day is
// untouched. These tests pin the behaviour that motivated it — a hand-typed
// correction must fix its own day without erasing the rest of the curve.

func day(t *testing.T, from, to string) Request {
	t.Helper()
	return Request{Bucket: Day, From: mustTime(t, from), To: mustTime(t, to)}
}

// TestOverlayReplacesOnlyItsOwnDay is the test the whole issue exists for. Ranking
// Manual first in Source priority — the original plan — would elect it winner of the
// whole range and collapse this series to a single point.
func TestOverlayReplacesOnlyItsOwnDay(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"body_mass", 91.0, "2024-01-01T08:00:00Z", "Zepp Life"},
		{"body_mass", 91.2, "2024-01-02T08:00:00Z", "Zepp Life"},
		{"body_mass", 99.9, "2024-01-03T08:00:00Z", "Zepp Life"}, // the bad reading
		{"body_mass", 91.4, "2024-01-04T08:00:00Z", "Zepp Life"},
		{"body_mass", 91.5, "2024-01-05T08:00:00Z", "Zepp Life"},
		{"body_mass", 91.3, "2024-01-03T09:00:00Z", catalog.SourceManual}, // the correction
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-06T00:00:00Z")
	req.AccountID, req.Metric = acc, "body_mass"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}

	if len(s.Points) != 5 {
		t.Fatalf("got %d points, want 5 — the overlay swallowed the other days: %+v", len(s.Points), s.Points)
	}
	if s.Points[2].Value != 91.3 {
		t.Errorf("2024-01-03 = %v, want the manual 91.3", s.Points[2].Value)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s.Points[i].Value == 91.3 {
			t.Errorf("point %d took the manual value; the overlay leaked past its day", i)
		}
	}
	// The Series still reports the imported Source: one corrected day does not make
	// the whole curve "Manual".
	if s.Source != "Zepp Life" {
		t.Errorf("source = %q, want %q", s.Source, "Zepp Life")
	}
}

// TestOverlayDoesNotDoubleCountSum is the trap a naive "include both sources" filter
// falls into: the corrected day would total device + manual instead of replacing.
func TestOverlayDoesNotDoubleCountSum(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 4000, "2024-01-01T08:00:00Z", "Watch"},
		{"steps", 3000, "2024-01-02T08:00:00Z", "Watch"},
		{"steps", 2000, "2024-01-02T18:00:00Z", "Watch"}, // same day, second device row
		{"steps", 9000, "2024-01-02T20:00:00Z", catalog.SourceManual},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z")
	req.AccountID, req.Metric = acc, "steps"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}

	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want 2", s.Points)
	}
	if s.Points[0].Value != 4000 {
		t.Errorf("2024-01-01 = %v, want the untouched 4000", s.Points[0].Value)
	}
	// 9000, not 14000 (9000+3000+2000) and not 5000: the device's rows for that day
	// are replaced, not added to and not partially kept.
	if s.Points[1].Value != 9000 {
		t.Errorf("2024-01-02 = %v, want 9000 (device rows replaced, not summed)", s.Points[1].Value)
	}
	// The window summary must agree with the points it summarises (ADR 0019): the
	// overlay is applied once, in the shared predicate, not twice.
	if s.Summary == nil || s.Summary.Value != 13000 {
		t.Errorf("summary = %+v, want 13000 = 4000 + 9000", s.Summary)
	}
}

// TestOverlayAverageUsesManualBand checks the min/max band follows the resolved row
// set too, rather than being computed over the union.
func TestOverlayAverageUsesManualBand(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"heart_rate", 60, "2024-01-01T08:00:00Z", "Watch"},
		{"heart_rate", 200, "2024-01-02T08:00:00Z", "Watch"}, // artefact
		{"heart_rate", 40, "2024-01-02T09:00:00Z", "Watch"},  // artefact
		{"heart_rate", 70, "2024-01-02T10:00:00Z", catalog.SourceManual},
		{"heart_rate", 80, "2024-01-02T11:00:00Z", catalog.SourceManual},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z")
	req.AccountID, req.Metric = acc, "heart_rate"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}

	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want 2", s.Points)
	}
	p := s.Points[1]
	if p.Value != 75 {
		t.Errorf("2024-01-02 mean = %v, want 75 (manual rows only)", p.Value)
	}
	if p.Min == nil || p.Max == nil || *p.Min != 70 || *p.Max != 80 {
		t.Errorf("band = %v–%v, want 70–80; the artefacts leaked into the band", p.Min, p.Max)
	}
}

// TestOverlayLatestPrefersManualWithinDay covers the `latest` rule, whose per-bucket
// pick runs through a window function rather than a plain GROUP BY.
func TestOverlayLatestPrefersManualWithinDay(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"body_fat_percentage", 0.27, "2024-01-01T08:00:00Z", "Zepp Life"},
		// The manual row is *earlier* in the day than the device's, so a rule that
		// only ordered by time would still pick the device value.
		{"body_fat_percentage", 0.22, "2024-01-01T06:00:00Z", catalog.SourceManual},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z")
	req.AccountID, req.Metric = acc, "body_fat_percentage"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 1 || s.Points[0].Value != 0.22 {
		t.Fatalf("points = %+v, want the manual 0.22 even though it is earlier in the day", s.Points)
	}
}

// TestManualOnlyMetricSeriesWorks covers an Account with no device for a Metric —
// the case that made ok=false before Manual was split out of the election.
func TestManualOnlyMetricSeriesWorks(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"height", 184, "2024-01-01T08:00:00Z", catalog.SourceManual},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z")
	req.AccountID, req.Metric = acc, "height"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(s.Points) != 1 || s.Points[0].Value != 184 {
		t.Fatalf("points = %+v, want one 184 point", s.Points)
	}
	if s.Source != catalog.SourceManual {
		t.Errorf("source = %q, want %q", s.Source, catalog.SourceManual)
	}
}

// TestOverlayFlowsThroughDerivedOperand checks a Formula operand picks the overlay up
// with no extra wiring — the payoff of resolving the row set in one shared predicate.
func TestOverlayFlowsThroughDerivedOperand(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"dietary_protein", 100, "2024-01-01T08:00:00Z", "Yazio"},
		{"body_mass", 200, "2024-01-01T08:00:00Z", "Zepp Life"}, // absurd, to be corrected
		{"body_mass", 100, "2024-01-01T09:00:00Z", catalog.SourceManual},
	})

	req := day(t, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z")
	req.AccountID, req.Metric = acc, "protein_per_kg"
	s, err := e.Series(context.Background(), req)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	// 100 g / 100 kg = 1, not 100/200 = 0.5.
	if len(s.Points) != 1 || s.Points[0].Value != 1 {
		t.Fatalf("points = %+v, want 1 g/kg from the corrected body mass", s.Points)
	}
}

// TestNoManualRowsLeavesFilterUnchanged pins the regression guard directly: with no
// Manual rows the predicate is the original `source = ?` filter, so no existing
// behaviour can shift.
func TestNoManualRowsLeavesFilterUnchanged(t *testing.T) {
	f := sourceFilter{source: "Watch"}
	req := day(t, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z")
	req.AccountID, req.Metric = 7, "steps"

	pred, args := f.where(req)
	want := `account_id = ? AND metric = ? AND source = ? AND start_at >= ? AND start_at < ?`
	if pred != want {
		t.Errorf("predicate = %q, want the original %q", pred, want)
	}
	if len(args) != 5 || args[2] != "Watch" {
		t.Errorf("args = %v, want 5 args with the winning Source third", args)
	}
	if !f.any() || f.reported() != "Watch" {
		t.Errorf("any/reported = %v/%q, want true/Watch", f.any(), f.reported())
	}
}

func TestEmptyFilterSelectsNothing(t *testing.T) {
	var f sourceFilter
	if f.any() {
		t.Error("zero filter reports data")
	}
}
