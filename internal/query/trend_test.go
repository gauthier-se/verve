package query

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// referenceMass is the reference Account's body mass over 15 July – 27 August 2026:
// forty mornings with a real four-day gap in them, and the series every figure in
// ADR 0042's motivation was measured against. Using the actual readings rather than a
// synthetic ramp is the point — a smoother that looks right on a clean ramp and wrong
// on a bathroom scale would pass a synthetic test.
func referenceMass() []meas {
	return []meas{
		{"body_mass", 92.15, "2026-07-15T06:00:00Z", "Scale"},
		{"body_mass", 91.15, "2026-07-16T06:00:00Z", "Scale"},
		{"body_mass", 90.95, "2026-07-17T06:00:00Z", "Scale"},
		{"body_mass", 90.75, "2026-07-18T06:00:00Z", "Scale"},
		{"body_mass", 91.5, "2026-07-19T06:00:00Z", "Scale"},
		{"body_mass", 91.75, "2026-07-20T06:00:00Z", "Scale"},
		{"body_mass", 91.25, "2026-07-21T06:00:00Z", "Scale"},
		{"body_mass", 91.15, "2026-07-22T06:00:00Z", "Scale"},
		{"body_mass", 91.15, "2026-07-23T06:00:00Z", "Scale"},
		{"body_mass", 91.0, "2026-07-24T06:00:00Z", "Scale"},
		{"body_mass", 90.55, "2026-07-25T06:00:00Z", "Scale"},
		{"body_mass", 89.95, "2026-07-26T06:00:00Z", "Scale"},
		{"body_mass", 89.75, "2026-07-27T06:00:00Z", "Scale"},
		{"body_mass", 90.4, "2026-07-28T06:00:00Z", "Scale"},
		{"body_mass", 90.5, "2026-07-29T06:00:00Z", "Scale"},
		{"body_mass", 90.5, "2026-07-30T06:00:00Z", "Scale"},
		{"body_mass", 90.65, "2026-07-31T06:00:00Z", "Scale"},
		{"body_mass", 89.4, "2026-08-03T06:00:00Z", "Scale"},
		{"body_mass", 90.15, "2026-08-04T06:00:00Z", "Scale"},
		{"body_mass", 90.15, "2026-08-05T06:00:00Z", "Scale"},
		{"body_mass", 90.75, "2026-08-06T06:00:00Z", "Scale"},
		{"body_mass", 90.35, "2026-08-07T06:00:00Z", "Scale"},
		{"body_mass", 88.15, "2026-08-10T06:00:00Z", "Scale"},
		{"body_mass", 89.25, "2026-08-11T06:00:00Z", "Scale"},
		{"body_mass", 89.45, "2026-08-12T06:00:00Z", "Scale"},
		{"body_mass", 89.6, "2026-08-13T06:00:00Z", "Scale"},
		{"body_mass", 89.2, "2026-08-14T06:00:00Z", "Scale"},
		{"body_mass", 89.3, "2026-08-15T06:00:00Z", "Scale"},
		{"body_mass", 88.85, "2026-08-16T06:00:00Z", "Scale"},
		{"body_mass", 88.55, "2026-08-17T06:00:00Z", "Scale"},
		{"body_mass", 88.8, "2026-08-18T06:00:00Z", "Scale"},
		{"body_mass", 88.8, "2026-08-19T06:00:00Z", "Scale"},
		{"body_mass", 89.1, "2026-08-20T06:00:00Z", "Scale"},
		{"body_mass", 88.55, "2026-08-21T06:00:00Z", "Scale"},
		{"body_mass", 88.05, "2026-08-22T06:00:00Z", "Scale"},
		{"body_mass", 87.55, "2026-08-23T06:00:00Z", "Scale"},
		{"body_mass", 87.65, "2026-08-24T06:00:00Z", "Scale"},
		{"body_mass", 87.3, "2026-08-25T06:00:00Z", "Scale"},
		{"body_mass", 87.3, "2026-08-26T06:00:00Z", "Scale"},
		{"body_mass", 87.3, "2026-08-27T06:00:00Z", "Scale"},
	}
}

func massSeries(t *testing.T) Series {
	t.Helper()
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, referenceMass())
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "body_mass", Bucket: timeaxis.Day,
		From: mustTime(t, "2026-07-15T00:00:00Z"), To: mustTime(t, "2026-08-28T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	return s
}

// TestTrendFitsTheReferenceWindow is the acceptance case, and the numbers are
// checkable by hand: the endpoints of this series differ by −4.85 kg, the fit says
// −3.96 kg over the same span, and the whole feature exists because those are not the
// same claim.
func TestTrendFitsTheReferenceWindow(t *testing.T) {
	s := massSeries(t)
	if s.Trend == nil {
		t.Fatal("no trend on a latest Metric at day grain")
	}
	if got := s.Trend.PerDay; math.Abs(got-(-0.0921)) > 0.0005 {
		t.Errorf("per day = %.4f kg, want −0.0921 (−92 g)", got)
	}
	if got := s.Trend.PerWeek; math.Abs(got-(-0.645)) > 0.005 {
		t.Errorf("per week = %.3f kg, want −0.645", got)
	}
	if got := s.Trend.R2; math.Abs(got-0.863) > 0.005 {
		t.Errorf("r2 = %.3f, want 0.863", got)
	}
	if s.Trend.Days != 40 {
		t.Errorf("days = %d, want 40 readings", s.Trend.Days)
	}
	// The percentage is against the window mean, not the last reading, so it is a
	// property of the range rather than of one morning.
	if got := s.Trend.PctPerWeek; math.Abs(got-(-0.72)) > 0.02 {
		t.Errorf("pct per week = %.2f%%, want ≈ −0.72", got)
	}
}

// TestTrendSmoothsTheNoiseItWasBuiltFor: on the reference series neighbouring
// mornings differ by up to 2.2 kg while the trend between them never moves more than
// a few hundred grams. That is the property a reader is buying.
//
// The bound is asserted over *consecutive* days only, and the two three-day gaps in
// this window are checked apart, because they are not the same thing: after a gap the
// decay deliberately gives the returning reading more weight (three days at a ten-day
// half-life is 19%, against 6.7% for one), so the step there is larger by design. A
// single bound over every pair would either be loose enough to pass a broken smoother
// or would be a test of the gaps rather than of the smoothing.
func TestTrendSmoothsTheNoiseItWasBuiltFor(t *testing.T) {
	s := massSeries(t)

	var maxReadingJump, maxDailyTrendJump, maxGapTrendJump float64
	for i := 1; i < len(s.Points); i++ {
		if s.Points[i].Trend == nil || s.Points[i-1].Trend == nil {
			t.Fatalf("point %d carries no trend", i)
		}
		prev := mustTime(t, s.Points[i-1].Bucket+"T00:00:00Z")
		cur := mustTime(t, s.Points[i].Bucket+"T00:00:00Z")
		jump := math.Abs(*s.Points[i].Trend - *s.Points[i-1].Trend)
		maxReadingJump = math.Max(maxReadingJump, math.Abs(s.Points[i].Value-s.Points[i-1].Value))
		if cur.Sub(prev).Hours() > 24 {
			maxGapTrendJump = math.Max(maxGapTrendJump, jump)
			continue
		}
		maxDailyTrendJump = math.Max(maxDailyTrendJump, jump)
	}

	if maxReadingJump < 1.0 {
		t.Fatalf("fixture is too smooth to test anything: biggest reading jump %.2f kg", maxReadingJump)
	}
	if maxDailyTrendJump > 0.2 {
		t.Errorf("biggest day-to-day trend step %.3f kg (readings step %.2f) — not smoothing",
			maxDailyTrendJump, maxReadingJump)
	}
	// A gap step is allowed to be bigger, but not unbounded: past the raw reading jump
	// the average would be tracking the noise it was built to absorb.
	if maxGapTrendJump > 0.6 {
		t.Errorf("biggest across-gap trend step %.3f kg — the decay forgets too fast", maxGapTrendJump)
	}

	// And it must still *go* somewhere: a smoother that flattens everything would pass
	// every bound above and be useless.
	first, last := *s.Points[0].Trend, *s.Points[len(s.Points)-1].Trend
	if first-last < 3.0 {
		t.Errorf("trend fell only %.2f kg across the window; the readings fell 4.85", first-last)
	}
}

// TestTrendWeightsByElapsedDaysNotPosition: the same readings in the same order, one
// spaced a day apart and one a fortnight, must not produce the same average. A
// positional EWMA would treat both identically, which is the bug this guards.
func TestTrendWeightsByElapsedDaysNotPosition(t *testing.T) {
	dense := []Point{
		{Bucket: "2024-01-01", Value: 90},
		{Bucket: "2024-01-02", Value: 80},
		{Bucket: "2024-01-03", Value: 80},
	}
	sparse := []Point{
		{Bucket: "2024-01-01", Value: 90},
		{Bucket: "2024-01-15", Value: 80},
		{Bucket: "2024-01-29", Value: 80},
	}
	smoothTrend(dense)
	smoothTrend(sparse)

	d, sp := *dense[2].Trend, *sparse[2].Trend
	if math.Abs(d-sp) < 1 {
		t.Errorf("dense %.3f and sparse %.3f agree; the decay is positional, not temporal", d, sp)
	}
	// The sparse one has had a fortnight per step to forget the 90, so it must sit
	// closer to the new level than the dense one does.
	if sp >= d {
		t.Errorf("sparse trend %.3f did not converge faster than dense %.3f", sp, d)
	}
}

// TestTrendRestartsAfterALongSilence: a reading that arrives months later describes
// the body that came back, and must not be averaged against the one that left.
func TestTrendRestartsAfterALongSilence(t *testing.T) {
	points := []Point{
		{Bucket: "2024-01-01", Value: 95},
		{Bucket: "2024-01-02", Value: 95},
		{Bucket: "2024-06-01", Value: 80},
	}
	smoothTrend(points)
	if got := *points[2].Trend; got != 80 {
		t.Errorf("trend after a five-month gap = %.2f, want the reading itself (80)", got)
	}
}

// TestTrendAbsentForNonLatestAggregation holds the scope at the seam rather than in
// the UI: a sum's bucket is already a fold of everything in it.
func TestTrendAbsentForNonLatestAggregation(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 8000, "2024-01-01T08:00:00Z", "Watch"},
		{"steps", 9000, "2024-01-02T08:00:00Z", "Watch"},
		{"steps", 7000, "2024-01-03T08:00:00Z", "Watch"},
	})
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: timeaxis.Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-04T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Trend != nil {
		t.Errorf("steps carries a trend: %+v", s.Trend)
	}
	for i, p := range s.Points {
		if p.Trend != nil {
			t.Errorf("steps point %d carries a trend", i)
		}
	}
}

// TestTrendAbsentAboveDayGrain: a weekly bucket has already smoothed its readings by
// folding them, and smoothing that again would describe data that is no longer there.
func TestTrendAbsentAboveDayGrain(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, referenceMass())
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "body_mass", Bucket: timeaxis.Week,
		From: mustTime(t, "2026-07-15T00:00:00Z"), To: mustTime(t, "2026-08-28T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Trend != nil {
		t.Errorf("weekly series carries a trend: %+v", s.Trend)
	}
}

// TestTrendNilWhenNothingCouldBeFitted: one reading is a point, not a direction.
func TestTrendNilWhenNothingCouldBeFitted(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{{"body_mass", 90, "2024-01-01T06:00:00Z", "Scale"}})
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "body_mass", Bucket: timeaxis.Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-05T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Trend != nil {
		t.Errorf("a single reading produced a trend: %+v", s.Trend)
	}
	// The point still carries its own smoothed value, which is itself: a seed is a
	// legitimate average of one reading, and the line simply has one dot.
	if s.Points[0].Trend == nil || *s.Points[0].Trend != 90 {
		t.Errorf("seed point trend = %v, want 90", s.Points[0].Trend)
	}
}

// TestTrendFollowsTheResolvedRows is the proof that the fit runs after Source election
// and not beside it: the losing Source's readings must not move the line.
func TestTrendFollowsTheResolvedRows(t *testing.T) {
	e, models, acc := setup(t)
	ms := referenceMass()
	// A second Source claiming a flat 100 kg over the same days. It loses the
	// election, so it must be invisible to the trend as it is to the points.
	for _, m := range referenceMass() {
		ms = append(ms, meas{m.metric, 100, m.at, "Zzz Other"})
	}
	seed(t, e.DB, models, acc, ms)

	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "body_mass", Bucket: timeaxis.Day,
		From: mustTime(t, "2026-07-15T00:00:00Z"), To: mustTime(t, "2026-08-28T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Trend == nil {
		t.Fatal("no trend")
	}
	if math.Abs(s.Trend.PerDay-(-0.0921)) > 0.0005 {
		t.Errorf("per day = %.4f; the losing Source moved the fit", s.Trend.PerDay)
	}
}

// TestTrendR2IsOneForAPerfectLine, and low for noise around a flat level: the two ends
// of the scale the number exists to distinguish.
func TestTrendR2(t *testing.T) {
	perfect := make([]Point, 10)
	for i := range perfect {
		perfect[i] = Point{
			Bucket: time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
			Value:  90 - 0.1*float64(i),
		}
	}
	if got := fitTrend(perfect); got == nil || math.Abs(got.R2-1) > 1e-9 {
		t.Errorf("r2 on a perfect line = %v, want 1", got)
	}

	noisy := make([]Point, 10)
	for i := range noisy {
		v := 90.0
		if i%2 == 1 {
			v = 91.0
		}
		noisy[i] = Point{
			Bucket: time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
			Value:  v,
		}
	}
	got := fitTrend(noisy)
	if got == nil {
		t.Fatal("no fit")
	}
	if got.R2 > 0.1 {
		t.Errorf("r2 on an alternating series = %.3f, want near 0", got.R2)
	}
}

// TestOrdinaryLeastSquaresIsOriginIndependent: the Estimates engine measures x from
// its own window start and query measures from the first reading. One fit serves both
// only if the slope does not depend on that choice.
func TestOrdinaryLeastSquaresIsOriginIndependent(t *testing.T) {
	points := []Point{
		{Bucket: "2024-03-10", Value: 90.4},
		{Bucket: "2024-03-12", Value: 90.1},
		{Bucket: "2024-03-15", Value: 89.6},
		{Bucket: "2024-03-20", Value: 89.0},
	}
	a, ok := OrdinaryLeastSquares(points, mustTime(t, "2024-01-01T00:00:00Z"))
	if !ok {
		t.Fatal("no fit")
	}
	b, _ := OrdinaryLeastSquares(points, mustTime(t, "2024-03-10T00:00:00Z"))
	if math.Abs(a-b) > 1e-12 {
		t.Errorf("slope depends on the origin: %v vs %v", a, b)
	}
	if fmt.Sprintf("%.4f", a) == "0.0000" {
		t.Errorf("slope is zero on a falling series")
	}
}
