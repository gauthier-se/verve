package query

import (
	"context"
	"math"
	"testing"
)

func TestSpearmanIsMonotoneNotLinear(t *testing.T) {
	// A perfectly monotone but sharply curved relationship: Pearson would report
	// well under 1, Spearman reports exactly 1, which is the reason it is the measure
	// this page uses.
	xs := []float64{1, 2, 3, 4, 5}
	ys := []float64{1, 4, 9, 16, 25}
	if got := spearman(xs, ys); math.Abs(got-1) > 1e-9 {
		t.Errorf("rho = %v, want 1 for a monotone increasing pair", got)
	}
	rev := []float64{25, 16, 9, 4, 1}
	if got := spearman(xs, rev); math.Abs(got+1) > 1e-9 {
		t.Errorf("rho = %v, want -1 for a monotone decreasing pair", got)
	}
}

func TestSpearmanFlatSideIsZero(t *testing.T) {
	// No variance on one side: no relationship is expressible. Zero, not NaN — a NaN
	// would travel into the JSON and land in the interface as a blank cell nobody
	// could explain.
	if got := spearman([]float64{1, 2, 3}, []float64{5, 5, 5}); got != 0 {
		t.Errorf("rho = %v, want 0 when one side never moves", got)
	}
}

func TestRanksShareTiesEvenly(t *testing.T) {
	got := ranks([]float64{10, 20, 20, 30})
	want := []float64{1, 2.5, 2.5, 4}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("ranks = %v, want %v", got, want)
		}
	}
}

// TestCoVaryLagShiftsTheSecondMetric locks the direction of a lag: with shift 1, a
// pair reads A now against B one bucket later, so a series that leads its partner by
// a day correlates perfectly in one direction and not in the other.
func TestCoVaryLagShiftsTheSecondMetric(t *testing.T) {
	e, models, acc := setup(t)
	// steps rise; resting heart rate repeats the same shape one day later.
	seed(t, e.DB, models, acc, []meas{
		{"steps", 1000, "2024-01-01T08:00:00Z", "Watch"},
		{"steps", 2000, "2024-01-02T08:00:00Z", "Watch"},
		{"steps", 3000, "2024-01-03T08:00:00Z", "Watch"},
		{"steps", 4000, "2024-01-04T08:00:00Z", "Watch"},
		{"resting_heart_rate", 50, "2024-01-02T08:00:00Z", "Watch"},
		{"resting_heart_rate", 52, "2024-01-03T08:00:00Z", "Watch"},
		{"resting_heart_rate", 54, "2024-01-04T08:00:00Z", "Watch"},
		{"resting_heart_rate", 56, "2024-01-05T08:00:00Z", "Watch"},
	})

	req := CoVaryRequest{
		AccountID: acc, Metrics: []string{"steps", "resting_heart_rate"},
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-06T00:00:00Z"),
		Bucket: Day, Lag: 1,
	}
	cv, err := e.CoVary(context.Background(), req)
	if err != nil {
		t.Fatalf("CoVary: %v", err)
	}

	lead := findPair(t, cv, "steps", "resting_heart_rate")
	if lead.Shared != 4 || math.Abs(lead.Rho-1) > 1e-9 {
		t.Errorf("steps → rhr at lag 1 = %+v, want 4 shared buckets and rho 1", lead)
	}
	// The other direction pairs each rhr bucket with the steps bucket after it, which
	// the data supports on two days only: the lag is directional, and the overlap it
	// finds is not the same overlap read the other way round.
	back := findPair(t, cv, "resting_heart_rate", "steps")
	if back.Shared != 2 {
		t.Errorf("rhr → steps at lag 1 shared = %d, want 2 (the lag is directional)", back.Shared)
	}
}

// TestCoVaryUnrankedPairsSinkButStay locks that a pair short of the shared-bucket
// threshold is still reported, flagged, and sorted below every ranked one: "not
// enough overlap" is an answer, and dropping it would read as "no relationship".
func TestCoVaryUnrankedPairsSinkButStay(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 1000, "2024-01-01T08:00:00Z", "Watch"},
		{"steps", 2000, "2024-01-02T08:00:00Z", "Watch"},
		{"steps", 3000, "2024-01-03T08:00:00Z", "Watch"},
		{"body_mass", 80, "2024-01-01T08:00:00Z", "Health"},
		{"body_mass", 81, "2024-01-02T08:00:00Z", "Health"},
		{"body_mass", 82, "2024-01-03T08:00:00Z", "Health"},
	})

	cv, err := e.CoVary(context.Background(), CoVaryRequest{
		AccountID: acc, Metrics: []string{"steps", "body_mass"},
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-31T00:00:00Z"),
		Bucket: Day,
	})
	if err != nil {
		t.Fatalf("CoVary: %v", err)
	}
	if cv.MinShared < minSharedBuckets {
		t.Errorf("min shared = %d, want at least the floor %d", cv.MinShared, minSharedBuckets)
	}
	if len(cv.Pairs) != 2 {
		t.Fatalf("pairs = %d, want both directions", len(cv.Pairs))
	}
	for _, p := range cv.Pairs {
		if p.Ranked {
			t.Errorf("pair %s × %s ranked on %d shared buckets, threshold is %d", p.A, p.B, p.Shared, cv.MinShared)
		}
		if p.Shared != 3 {
			t.Errorf("pair %s × %s shared = %d, want 3", p.A, p.B, p.Shared)
		}
	}
	// Nothing was rankable, so there is no strongest pair to draw.
	if cv.Strongest != nil {
		t.Errorf("strongest = %+v, want none when no pair is ranked", cv.Strongest)
	}
}

// TestCoVaryDrawsTheStrongestPair locks that the scatter carries the shared buckets
// and a fitted line, both computed server-side (ADR 0012).
func TestCoVaryDrawsTheStrongestPair(t *testing.T) {
	e, models, acc := setup(t)
	ms := []meas{}
	for day := 1; day <= 20; day++ {
		at := "2024-01-" + twoDigits(day) + "T08:00:00Z"
		ms = append(ms,
			meas{"steps", float64(1000 * day), at, "Watch"},
			meas{"resting_heart_rate", float64(70 - day), at, "Watch"},
		)
	}
	seed(t, e.DB, models, acc, ms)

	cv, err := e.CoVary(context.Background(), CoVaryRequest{
		AccountID: acc, Metrics: []string{"steps", "resting_heart_rate"},
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-21T00:00:00Z"),
		Bucket: Day,
	})
	if err != nil {
		t.Fatalf("CoVary: %v", err)
	}
	if cv.Strongest == nil {
		t.Fatal("strongest = nil, want the ranked pair drawn")
	}
	if len(cv.Strongest.Points) != 20 {
		t.Errorf("scatter points = %d, want 20", len(cv.Strongest.Points))
	}
	if math.Abs(cv.Strongest.Rho+1) > 1e-9 {
		t.Errorf("scatter rho = %v, want -1", cv.Strongest.Rho)
	}
	if cv.Strongest.Fit == nil || cv.Strongest.Fit.Y2 >= cv.Strongest.Fit.Y1 {
		t.Errorf("fit = %+v, want a line sloping down", cv.Strongest.Fit)
	}
	if cv.Units["steps"] != "count" {
		t.Errorf("units = %v, want each metric's own unit", cv.Units)
	}
}

func findPair(t *testing.T, cv CoVariation, a, b string) Pair {
	t.Helper()
	for _, p := range cv.Pairs {
		if p.A == a && p.B == b {
			return p
		}
	}
	t.Fatalf("pair %s × %s not found in %+v", a, b, cv.Pairs)
	return Pair{}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
