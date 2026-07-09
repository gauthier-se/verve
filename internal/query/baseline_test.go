package query

import (
	"context"
	"testing"
)

// TestComparePrevious is the acceptance case: a 3-day current window overlaid with
// the prior 3 days at the same bucket granularity, equal length and index-aligned,
// each baseline bucket keeping its own real date. The baseline window is resolved
// by the caller (timeaxis); Compare only aligns.
func TestComparePrevious(t *testing.T) {
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

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, mustTime(t, "2024-01-05T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
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
		if cmp.Baseline.Points[i].Bucket == cmp.Current.Points[i].Bucket {
			t.Errorf("baseline[%d] date %q equals current date, want its own real date", i, cmp.Baseline.Points[i].Bucket)
		}
	}
	if cmp.Baseline.Bucket != Day {
		t.Errorf("baseline bucket = %q, want the current granularity (day)", cmp.Baseline.Bucket)
	}
}

// TestCompareTruncatesLongerBaseline proves a longer baseline window is truncated
// to the current window's length, keeping the baseline's own leading dates and
// dropping its orphan tail.
func TestCompareTruncatesLongerBaseline(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 1, "2024-02-01T08:00:00Z", "Watch"},
		{"steps", 2, "2024-02-02T08:00:00Z", "Watch"},
		{"steps", 3, "2024-02-03T08:00:00Z", "Watch"},
		// Baseline window: 5 dense days — two more than the current window.
		{"steps", 10, "2024-03-01T08:00:00Z", "Watch"},
		{"steps", 20, "2024-03-02T08:00:00Z", "Watch"},
		{"steps", 30, "2024-03-03T08:00:00Z", "Watch"},
		{"steps", 40, "2024-03-04T08:00:00Z", "Watch"},
		{"steps", 50, "2024-03-05T08:00:00Z", "Watch"},
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-02-01T00:00:00Z"), To: mustTime(t, "2024-02-04T00:00:00Z"),
	}, mustTime(t, "2024-03-01T00:00:00Z"), mustTime(t, "2024-03-06T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(cmp.Current.Points) != 3 || len(cmp.Baseline.Points) != 3 {
		t.Fatalf("lengths = current %d / baseline %d, want 3/3 (baseline truncated)", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	wantBase := []Point{{Bucket: "2024-03-01", Value: 10}, {Bucket: "2024-03-02", Value: 20}, {Bucket: "2024-03-03", Value: 30}}
	for i := range wantBase {
		if cmp.Baseline.Points[i] != wantBase[i] {
			t.Errorf("baseline[%d] = %+v, want %+v", i, cmp.Baseline.Points[i], wantBase[i])
		}
	}
}

// TestCompareTruncatesCurrentToShorter proves truncation is symmetric: a shorter
// baseline window truncates the current window down to it.
func TestCompareTruncatesCurrentToShorter(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 1, "2024-02-01T08:00:00Z", "Watch"},
		{"steps", 2, "2024-02-02T08:00:00Z", "Watch"},
		{"steps", 3, "2024-02-03T08:00:00Z", "Watch"},
		{"steps", 4, "2024-02-04T08:00:00Z", "Watch"},
		{"steps", 10, "2024-03-01T08:00:00Z", "Watch"},
		{"steps", 20, "2024-03-02T08:00:00Z", "Watch"},
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-02-01T00:00:00Z"), To: mustTime(t, "2024-02-05T00:00:00Z"),
	}, mustTime(t, "2024-03-01T00:00:00Z"), mustTime(t, "2024-03-03T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(cmp.Current.Points) != 2 || len(cmp.Baseline.Points) != 2 {
		t.Fatalf("lengths = current %d / baseline %d, want 2/2 (current truncated)", len(cmp.Current.Points), len(cmp.Baseline.Points))
	}
	wantCur := []Point{{Bucket: "2024-02-01", Value: 1}, {Bucket: "2024-02-02", Value: 2}}
	for i := range wantCur {
		if cmp.Current.Points[i] != wantCur[i] {
			t.Errorf("current[%d] = %+v, want %+v", i, cmp.Current.Points[i], wantCur[i])
		}
	}
}

// TestCompareEmptyWindow proves an empty baseline window is "effectively absent":
// it does not shrink the current window, and its series is all dated gaps.
func TestCompareEmptyWindow(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"},
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, mustTime(t, "2024-01-05T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(cmp.Current.Points) != 3 {
		t.Errorf("current points = %d, want 3 (untouched by an absent baseline)", len(cmp.Current.Points))
	}
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

// TestCompareGapInCurrentAligns is the interior-gap correctness case: a gap in the
// current window must not shift the pairing. Jan 10 sits at ordinal 2, so it pairs
// with the baseline's ordinal-2 bucket (Jan 7), never its ordinal-1 (Jan 6).
func TestCompareGapInCurrentAligns(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"}, // ordinal 0
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"}, // ordinal 2 (Jan 9 is a gap)
		{"steps", 10, "2024-01-05T08:00:00Z", "Watch"},  // ordinal 0
		{"steps", 20, "2024-01-06T08:00:00Z", "Watch"},  // ordinal 1
		{"steps", 30, "2024-01-07T08:00:00Z", "Watch"},  // ordinal 2
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, mustTime(t, "2024-01-05T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
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

// TestCompareGapInBaseline proves a baseline bucket with no data at an ordinal the
// current window fills becomes a dated Gap slot — equal length, no invented zero.
func TestCompareGapInBaseline(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 100, "2024-01-08T08:00:00Z", "Watch"},
		{"steps", 200, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 300, "2024-01-10T08:00:00Z", "Watch"},
		{"steps", 10, "2024-01-05T08:00:00Z", "Watch"},
		{"steps", 30, "2024-01-07T08:00:00Z", "Watch"}, // Jan 6 a gap
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-11T00:00:00Z"),
	}, mustTime(t, "2024-01-05T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(cmp.Baseline.Points) != 3 {
		t.Fatalf("baseline points = %+v, want 3 (gap held open)", cmp.Baseline.Points)
	}
	want := []Point{
		{Bucket: "2024-01-05", Value: 10},
		{Bucket: "2024-01-06", Gap: true},
		{Bucket: "2024-01-07", Value: 30},
	}
	for i := range want {
		if cmp.Baseline.Points[i] != want[i] {
			t.Errorf("baseline[%d] = %+v, want %+v", i, cmp.Baseline.Points[i], want[i])
		}
	}
}

// TestCompareWeekBuckets guards that the Go bucket-stepping used for ordinals
// agrees with the SQL bucketing: a week-bucket comparison keys both windows on
// their Mondays and aligns them one ISO week apart.
func TestCompareWeekBuckets(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"steps", 10, "2024-01-09T08:00:00Z", "Watch"},
		{"steps", 20, "2024-01-13T08:00:00Z", "Watch"},
		{"steps", 5, "2024-01-03T08:00:00Z", "Watch"},
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Week,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-15T00:00:00Z"),
	}, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(cmp.Current.Points) != 1 || cmp.Current.Points[0] != (Point{Bucket: "2024-01-08", Value: 30}) {
		t.Errorf("current = %+v, want the week of 2024-01-08 summing to 30", cmp.Current.Points)
	}
	if len(cmp.Baseline.Points) != 1 || cmp.Baseline.Points[0] != (Point{Bucket: "2024-01-01", Value: 5}) {
		t.Errorf("baseline = %+v, want the prior week of 2024-01-01 (Monday key) valued 5", cmp.Baseline.Points)
	}
}

// TestCompareDerived proves a derived Metric's baseline recomputes per bucket from
// its operands (ADR 0014) over the baseline window, with no single Source.
func TestCompareDerived(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{
		{"dietary_energy", 1800, "2024-01-02T12:00:00Z", "MyFitnessPal"},
		{"active_energy", 400, "2024-01-02T18:00:00Z", "Watch"},
		{"basal_energy", 1600, "2024-01-02T23:00:00Z", "Watch"},
		{"dietary_energy", 2200, "2024-01-01T12:00:00Z", "MyFitnessPal"},
		{"active_energy", 300, "2024-01-01T18:00:00Z", "Watch"},
		{"basal_energy", 1500, "2024-01-01T23:00:00Z", "Watch"},
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "calorie_balance", Bucket: Day,
		From: mustTime(t, "2024-01-02T00:00:00Z"), To: mustTime(t, "2024-01-03T00:00:00Z"),
	}, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-02T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
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
