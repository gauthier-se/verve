package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBaselineWindow pins the calendar math of the baseline-window resolver: the
// three active rules, isolated from the DB.
func TestBaselineWindow(t *testing.T) {
	req := Request{
		From: mustTime(t, "2024-06-10T00:00:00Z"),
		To:   mustTime(t, "2024-06-20T00:00:00Z"), // a 10-day span
	}

	t.Run("previous shifts back by the range's own length", func(t *testing.T) {
		from, to, err := baselineWindow(req, Baseline{Rule: BaselinePrevious})
		if err != nil {
			t.Fatalf("baselineWindow: %v", err)
		}
		// [From-10d, To-10d) — the 10 days immediately before the current window.
		if !from.Equal(mustTime(t, "2024-05-31T00:00:00Z")) || !to.Equal(mustTime(t, "2024-06-10T00:00:00Z")) {
			t.Errorf("previous = [%s, %s), want [2024-05-31, 2024-06-10)", from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	})

	t.Run("same_period_last_year shifts back one calendar year", func(t *testing.T) {
		from, to, err := baselineWindow(req, Baseline{Rule: BaselineSamePeriodLastYear})
		if err != nil {
			t.Fatalf("baselineWindow: %v", err)
		}
		if !from.Equal(mustTime(t, "2023-06-10T00:00:00Z")) || !to.Equal(mustTime(t, "2023-06-20T00:00:00Z")) {
			t.Errorf("last year = [%s, %s), want [2023-06-10, 2023-06-20)", from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	})

	t.Run("custom returns the absolute bounds as given", func(t *testing.T) {
		cf, ct := mustTime(t, "2020-01-01T00:00:00Z"), mustTime(t, "2020-02-01T00:00:00Z")
		from, to, err := baselineWindow(req, Baseline{Rule: BaselineCustom, From: cf, To: ct})
		if err != nil {
			t.Fatalf("baselineWindow: %v", err)
		}
		if !from.Equal(cf) || !to.Equal(ct) {
			t.Errorf("custom = [%s, %s), want the given bounds", from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	})

	t.Run("unknown rule errors", func(t *testing.T) {
		if _, _, err := baselineWindow(req, Baseline{Rule: "none"}); !errors.Is(err, ErrUnknownBaselineRule) {
			t.Errorf("err = %v, want ErrUnknownBaselineRule", err)
		}
	})
}

// TestSeriesWithBaselinePrevious is the acceptance case: a 3-day range with a
// `previous` baseline returns the prior 3 days at the same bucket granularity,
// equal length and index-aligned, each baseline bucket keeping its own real date.
func TestSeriesWithBaselinePrevious(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Current window: Jan 8, 9, 10.
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"},
		// Previous window (prior 3 days): Jan 5, 6, 7.
		{"steps", 10, "2024-01-05T08:00:00Z", "Watch"},
		{"steps", 20, "2024-01-06T08:00:00Z", "Watch"},
		{"steps", 30, "2024-01-07T08:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, Baseline{Rule: BaselinePrevious})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}

	if len(cmp.Current.Points) != 3 || len(cmp.Baseline.Points) != 3 {
		t.Fatalf("lengths = current %d / baseline %d, want 3/3", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	wantCur := []Point{{Bucket: "2024-01-08", Value: 100}, {Bucket: "2024-01-09", Value: 200}, {Bucket: "2024-01-10", Value: 300}}
	wantBase := []Point{{Bucket: "2024-01-05", Value: 10}, {Bucket: "2024-01-06", Value: 20}, {Bucket: "2024-01-07", Value: 30}}
	for i := range wantCur {
		if cmp.Current.Points[i] != wantCur[i] {
			t.Errorf("current[%d] = %+v, want %+v", i, cmp.Current.Points[i], wantCur[i])
		}
		if cmp.Baseline.Points[i] != wantBase[i] {
			t.Errorf("baseline[%d] = %+v, want %+v", i, cmp.Baseline.Points[i], wantBase[i])
		}
		// Each aligned pair carries distinct real dates — baseline is not relabelled.
		if cmp.Baseline.Points[i].Bucket == cmp.Current.Points[i].Bucket {
			t.Errorf("baseline[%d] date %q equals current date, want its own real date", i, cmp.Baseline.Points[i].Bucket)
		}
	}
	if cmp.Baseline.Bucket != Day {
		t.Errorf("baseline bucket = %q, want the current granularity (day)", cmp.Baseline.Bucket)
	}
}

// TestSeriesWithBaselineSamePeriodLastYear checks the annual rule returns the
// window one calendar year earlier.
func TestSeriesWithBaselineSamePeriodLastYear(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 500, "2024-07-04T08:00:00Z", "Watch"},
		{"steps", 700, "2023-07-04T08:00:00Z", "Watch"}, // same period, last year
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-07-04T00:00:00Z"), To: mustTime(t, "2024-07-05T00:00:00Z"),
	}, Baseline{Rule: BaselineSamePeriodLastYear})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Current.Points) != 1 || len(cmp.Baseline.Points) != 1 {
		t.Fatalf("lengths = current %d / baseline %d, want 1/1", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	if cmp.Current.Points[0] != (Point{Bucket: "2024-07-04", Value: 500}) {
		t.Errorf("current = %+v, want 2024-07-04/500", cmp.Current.Points[0])
	}
	if cmp.Baseline.Points[0] != (Point{Bucket: "2023-07-04", Value: 700}) {
		t.Errorf("baseline = %+v, want 2023-07-04/700 (one year earlier)", cmp.Baseline.Points[0])
	}
}

// TestSeriesWithBaselineTruncatesLongerBaseline proves a longer baseline window
// (a custom span) is truncated to the current window's length, keeping the
// baseline's own leading dates and dropping its orphan tail.
func TestSeriesWithBaselineTruncatesLongerBaseline(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Current window: 3 dense days.
		{"steps", 1, "2024-02-01T08:00:00Z", "Watch"},
		{"steps", 2, "2024-02-02T08:00:00Z", "Watch"},
		{"steps", 3, "2024-02-03T08:00:00Z", "Watch"},
		// Custom baseline window: 5 dense days — two more than the current window.
		{"steps", 10, "2024-03-01T08:00:00Z", "Watch"},
		{"steps", 20, "2024-03-02T08:00:00Z", "Watch"},
		{"steps", 30, "2024-03-03T08:00:00Z", "Watch"},
		{"steps", 40, "2024-03-04T08:00:00Z", "Watch"},
		{"steps", 50, "2024-03-05T08:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-02-01T00:00:00Z"), To: mustTime(t, "2024-02-04T00:00:00Z"),
	}, Baseline{
		Rule: BaselineCustom,
		From: mustTime(t, "2024-03-01T00:00:00Z"), To: mustTime(t, "2024-03-06T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Current.Points) != 3 || len(cmp.Baseline.Points) != 3 {
		t.Fatalf("lengths = current %d / baseline %d, want 3/3 (baseline truncated)", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	// The baseline keeps its first three real dates; Mar 4 and 5 are dropped.
	wantBase := []Point{{Bucket: "2024-03-01", Value: 10}, {Bucket: "2024-03-02", Value: 20}, {Bucket: "2024-03-03", Value: 30}}
	for i := range wantBase {
		if cmp.Baseline.Points[i] != wantBase[i] {
			t.Errorf("baseline[%d] = %+v, want %+v", i, cmp.Baseline.Points[i], wantBase[i])
		}
	}
}

// TestSeriesWithBaselineTruncatesCurrentToShorter proves truncation is symmetric:
// when the baseline window is shorter, the current window is truncated down to it.
func TestSeriesWithBaselineTruncatesCurrentToShorter(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Current: 4 dense days.
		{"steps", 1, "2024-02-01T08:00:00Z", "Watch"},
		{"steps", 2, "2024-02-02T08:00:00Z", "Watch"},
		{"steps", 3, "2024-02-03T08:00:00Z", "Watch"},
		{"steps", 4, "2024-02-04T08:00:00Z", "Watch"},
		// Custom baseline: only 2 days.
		{"steps", 10, "2024-03-01T08:00:00Z", "Watch"},
		{"steps", 20, "2024-03-02T08:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-02-01T00:00:00Z"), To: mustTime(t, "2024-02-05T00:00:00Z"),
	}, Baseline{
		Rule: BaselineCustom,
		From: mustTime(t, "2024-03-01T00:00:00Z"), To: mustTime(t, "2024-03-03T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Current.Points) != 2 || len(cmp.Baseline.Points) != 2 {
		t.Fatalf("lengths = current %d / baseline %d, want 2/2 (current truncated)", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	// The current window keeps its first two days; Feb 3 and 4 are dropped.
	wantCur := []Point{{Bucket: "2024-02-01", Value: 1}, {Bucket: "2024-02-02", Value: 2}}
	for i := range wantCur {
		if cmp.Current.Points[i] != wantCur[i] {
			t.Errorf("current[%d] = %+v, want %+v", i, cmp.Current.Points[i], wantCur[i])
		}
	}
}

// TestSeriesWithBaselineEmptyWindow proves an empty baseline window is
// "effectively absent": it does not shrink the current window, and its series is
// all gaps — each aligned slot a dated Gap, never a zero — so the overlay draws
// no baseline line.
func TestSeriesWithBaselineEmptyWindow(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Current window only; the prior 3 days (Jan 5–7) hold no data.
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, Baseline{Rule: BaselinePrevious})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Current.Points) != 3 {
		t.Errorf("current points = %d, want 3 (untouched by an absent baseline)", len(cmp.Current.Points))
	}
	// Every baseline slot is a dated gap over the prior window (Jan 5, 6, 7).
	wantGap := []Point{
		{Bucket: "2024-01-05", Gap: true},
		{Bucket: "2024-01-06", Gap: true},
		{Bucket: "2024-01-07", Gap: true},
	}
	if len(cmp.Baseline.Points) != 3 {
		t.Fatalf("baseline points = %+v, want 3 all-gap slots", cmp.Baseline.Points)
	}
	for i := range wantGap {
		if cmp.Baseline.Points[i] != wantGap[i] {
			t.Errorf("baseline[%d] = %+v, want dated gap %+v", i, cmp.Baseline.Points[i], wantGap[i])
		}
	}
	if cmp.Baseline.Source != "" {
		t.Errorf("baseline source = %q, want empty (no data)", cmp.Baseline.Source)
	}
}

// TestSeriesWithBaselineGapInCurrentAligns is the interior-gap correctness case:
// a gap in the current window must not shift the pairing. Current has data on
// Jan 8 and Jan 10 (Jan 9 a gap); the baseline is dense over the prior 3 days.
// Jan 10 sits at ordinal 2, so it must pair with the baseline's ordinal-2 bucket
// (Jan 7), never its ordinal-1 bucket (Jan 6) as a naive slice-zip would.
func TestSeriesWithBaselineGapInCurrentAligns(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"}, // ordinal 0
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"}, // ordinal 2 (Jan 9 is a gap)
		// Dense baseline, prior 3 days.
		{"steps", 10, "2024-01-05T08:00:00Z", "Watch"}, // ordinal 0
		{"steps", 20, "2024-01-06T08:00:00Z", "Watch"}, // ordinal 1
		{"steps", 30, "2024-01-07T08:00:00Z", "Watch"}, // ordinal 2
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, Baseline{Rule: BaselinePrevious})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	wantCur := []Point{{Bucket: "2024-01-08", Value: 100}, {Bucket: "2024-01-10", Value: 300}}
	wantBase := []Point{{Bucket: "2024-01-05", Value: 10}, {Bucket: "2024-01-07", Value: 30}} // ordinals 0 and 2
	if len(cmp.Current.Points) != 2 || len(cmp.Baseline.Points) != 2 {
		t.Fatalf("lengths = current %d / baseline %d, want 2/2", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	for i := range wantCur {
		if cmp.Current.Points[i] != wantCur[i] {
			t.Errorf("current[%d] = %+v, want %+v", i, cmp.Current.Points[i], wantCur[i])
		}
		if cmp.Baseline.Points[i] != wantBase[i] {
			t.Errorf("baseline[%d] = %+v, want %+v (aligned by ordinal, not slice index)", i, cmp.Baseline.Points[i], wantBase[i])
		}
	}
}

// TestSeriesWithBaselineGapInBaseline proves a baseline bucket with no data at an
// ordinal that the current window fills becomes a dated Gap slot — keeping the two
// series equal length and index-aligned without inventing a zero.
func TestSeriesWithBaselineGapInBaseline(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Dense current window.
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"},
		// Baseline missing its middle day (Jan 6 a gap).
		{"steps", 10, "2024-01-05T08:00:00Z", "Watch"},
		{"steps", 30, "2024-01-07T08:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, Baseline{Rule: BaselinePrevious})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Baseline.Points) != 3 {
		t.Fatalf("baseline points = %+v, want 3 (gap held open)", cmp.Baseline.Points)
	}
	want := []Point{
		{Bucket: "2024-01-05", Value: 10},
		{Bucket: "2024-01-06", Gap: true}, // held open, dated, no zero
		{Bucket: "2024-01-07", Value: 30},
	}
	for i := range want {
		if cmp.Baseline.Points[i] != want[i] {
			t.Errorf("baseline[%d] = %+v, want %+v", i, cmp.Baseline.Points[i], want[i])
		}
	}
}

// TestSeriesWithBaselineWeekBuckets guards that the Go bucket-stepping used for
// ordinals (snap/next) agrees with the SQL bucketing: a week-bucket comparison
// must key both windows on their Mondays and align them one ISO week apart.
func TestSeriesWithBaselineWeekBuckets(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Current ISO week (Mon 2024-01-08 … Sun 2024-01-14).
		{"steps", 10, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 20, "2024-01-13T08:00:00Z", "Watch"},
		// Previous ISO week (Mon 2024-01-01 … Sun 2024-01-07).
		{"steps", 5, "2024-01-03T08:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Week,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-15T00:00:00Z"),
	}, Baseline{Rule: BaselinePrevious})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Current.Points) != 1 || cmp.Current.Points[0] != (Point{Bucket: "2024-01-08", Value: 30}) {
		t.Errorf("current = %+v, want the week of 2024-01-08 summing to 30", cmp.Current.Points)
	}
	if len(cmp.Baseline.Points) != 1 || cmp.Baseline.Points[0] != (Point{Bucket: "2024-01-01", Value: 5}) {
		t.Errorf("baseline = %+v, want the prior week of 2024-01-01 (Monday key) valued 5", cmp.Baseline.Points)
	}
}

// TestSeriesWithBaselineDerived proves a derived Metric's baseline recomputes per
// bucket from its operands (ADR 0014) over the baseline window, with no single
// Source — exactly the current-window derived path, applied to the prior window.
func TestSeriesWithBaselineDerived(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		// Current day: balance = 1800 − 400 − 1600 = −200.
		{"dietary_energy", 1800, "2024-01-02T12:00:00Z", "MyFitnessPal"},
		{"active_energy", 400, "2024-01-02T18:00:00Z", "Watch"},
		{"basal_energy", 1600, "2024-01-02T23:00:00Z", "Watch"},
		// Previous day: balance = 2200 − 300 − 1500 = 400.
		{"dietary_energy", 2200, "2024-01-01T12:00:00Z", "MyFitnessPal"},
		{"active_energy", 300, "2024-01-01T18:00:00Z", "Watch"},
		{"basal_energy", 1500, "2024-01-01T23:00:00Z", "Watch"},
	})

	cmp, err := e.SeriesWithBaseline(context.Background(), Request{
		AccountID: acc, Metric: "calorie_balance", Bucket: Day,
		From: mustTime(t, "2024-01-02T00:00:00Z"), To: mustTime(t, "2024-01-03T00:00:00Z"),
	}, Baseline{Rule: BaselinePrevious})
	if err != nil {
		t.Fatalf("SeriesWithBaseline: %v", err)
	}
	if len(cmp.Current.Points) != 1 || cmp.Current.Points[0] != (Point{Bucket: "2024-01-02", Value: -200}) {
		t.Errorf("current = %+v, want 2024-01-02/-200", cmp.Current.Points)
	}
	if len(cmp.Baseline.Points) != 1 || cmp.Baseline.Points[0] != (Point{Bucket: "2024-01-01", Value: 400}) {
		t.Errorf("baseline = %+v, want 2024-01-01/400 (recomputed from operands)", cmp.Baseline.Points)
	}
	if cmp.Baseline.Source != "" {
		t.Errorf("derived baseline Source = %q, want empty (per-operand resolution)", cmp.Baseline.Source)
	}
}
