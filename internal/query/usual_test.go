package query

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/timeaxis"
)

// dailyReadings is one reading per day from start, the i-th valued vals[i], at 06:00.
// The days in skip are left out, so a gap is a missing day and never a zero.
func dailyReadings(metric, start string, vals []float64, skip map[int]bool) []meas {
	d, _ := time.Parse("2006-01-02", start)
	out := make([]meas, 0, len(vals))
	for i, v := range vals {
		if skip[i] {
			continue
		}
		out = append(out, meas{metric, v, d.AddDate(0, 0, i).Format("2006-01-02") + "T06:00:00Z", "Watch"})
	}
	return out
}

// oneTo is 1, 2, …, n: a sample whose quartiles are checkable by hand.
func oneTo(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i + 1)
	}
	return out
}

func usualSeries(t *testing.T, e Engine, acc int64, metric string, bucket timeaxis.Bucket, from, to string) Series {
	t.Helper()
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: metric, Bucket: bucket, Usual: true,
		From: mustTime(t, from+"T00:00:00Z"), To: mustTime(t, to+"T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	return s
}

func pointAt(t *testing.T, s Series, bucket string) Point {
	t.Helper()
	for _, p := range s.Points {
		if p.Bucket == bucket {
			return p
		}
	}
	t.Fatalf("no point at %s in %v", bucket, s.Points)
	return Point{}
}

func assertUsual(t *testing.T, p Point, low, high float64, n int) {
	t.Helper()
	if p.Usual == nil {
		t.Fatalf("%s: Usual nil, want %v to %v over %d", p.Bucket, low, high, n)
	}
	got := *p.Usual
	if fmt.Sprintf("%.4f %.4f %d", got.Low, got.High, got.N) != fmt.Sprintf("%.4f %.4f %d", low, high, n) {
		t.Fatalf("%s: Usual = %+v, want {Low:%v High:%v N:%d}", p.Bucket, got, low, high, n)
	}
}

// TestUsualIsTheQuartilesOfTheDaysBefore is the acceptance case: 1 June to 28 June
// read 1, 2, …, 28, and 29 June reads 99. The 29th is read against the 28 days before
// it and not against itself, so its Usual is the p25 to p75 of 1..28: 7.75 to 21.25
// (linear interpolation, positions 6.75 and 20.25).
func TestUsualIsTheQuartilesOfTheDaysBefore(t *testing.T) {
	e, models, acc := setup(t)
	vals := append(oneTo(28), 99)
	seed(t, e.DB, models, acc, dailyReadings("resting_heart_rate", "2026-06-01", vals, nil))

	s := usualSeries(t, e, acc, "resting_heart_rate", timeaxis.Day, "2026-06-29", "2026-06-30")

	assertUsual(t, pointAt(t, s, "2026-06-29"), 7.75, 21.25, 28)
}

// TestUsualCountsOnlyTheDaysThatWereRecorded: a missing day is absent from the
// reference, never a zero, and the minimum counts real values. With every other day of
// June missing, fourteen remain (2, 4, …, 28) and the 29th has a Usual of 8.5 to 21.5
// from 14; with one more missing, thirteen is too few to describe anything.
func TestUsualCountsOnlyTheDaysThatWereRecorded(t *testing.T) {
	everyOther := map[int]bool{}
	for i := 0; i < 28; i += 2 {
		everyOther[i] = true
	}
	vals := append(oneTo(28), 99)

	t.Run("fourteen is enough", func(t *testing.T) {
		e, models, acc := setup(t)
		seed(t, e.DB, models, acc, dailyReadings("resting_heart_rate", "2026-06-01", vals, everyOther))
		s := usualSeries(t, e, acc, "resting_heart_rate", timeaxis.Day, "2026-06-29", "2026-06-30")
		assertUsual(t, pointAt(t, s, "2026-06-29"), 8.5, 21.5, 14)
	})

	t.Run("thirteen is not", func(t *testing.T) {
		e, models, acc := setup(t)
		thirteen := map[int]bool{1: true}
		for k := range everyOther {
			thirteen[k] = true
		}
		seed(t, e.DB, models, acc, dailyReadings("resting_heart_rate", "2026-06-01", vals, thirteen))
		s := usualSeries(t, e, acc, "resting_heart_rate", timeaxis.Day, "2026-06-29", "2026-06-30")
		if u := pointAt(t, s, "2026-06-29").Usual; u != nil {
			t.Fatalf("Usual = %+v from 13 days, want nil", *u)
		}
	})
}

// weeklyReadings is one reading on the Wednesday of each week from the Monday start,
// so a week's sum is exactly its value.
func weeklyReadings(metric, monday string, vals []float64) []meas {
	d, _ := time.Parse("2006-01-02", monday)
	out := make([]meas, len(vals))
	for i, v := range vals {
		out[i] = meas{metric, v, d.AddDate(0, 0, 7*i+2).Format("2006-01-02") + "T12:00:00Z", "Watch"}
	}
	return out
}

// TestUsualAtWeekGrainIsTheTwelveWeeksBefore: the weeks from 6 April read 1, 2, …, 12
// and the week of 29 June reads 99. At week grain the reference is the twelve weekly
// values before it (3.75 to 9.25), and a week with only seven before it has none: the
// week of 1 June, with eight, has 2.75 to 6.25.
func TestUsualAtWeekGrainIsTheTwelveWeeksBefore(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, weeklyReadings("steps", "2026-04-06", append(oneTo(12), 99)))

	s := usualSeries(t, e, acc, "steps", timeaxis.Week, "2026-04-06", "2026-07-06")

	assertUsual(t, pointAt(t, s, "2026-06-29"), 3.75, 9.25, 12)
	assertUsual(t, pointAt(t, s, "2026-06-01"), 2.75, 6.25, 8)
	if u := pointAt(t, s, "2026-05-25").Usual; u != nil {
		t.Fatalf("week of 25 May: Usual = %+v from 7 weeks, want nil", *u)
	}
}

// TestUsualAbsentAtMonthGrain: a month would need most of a year to reach a usable
// count, and would then describe a year rather than a usual.
func TestUsualAbsentAtMonthGrain(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, dailyReadings("steps", "2025-01-01", oneTo(540), nil))

	s := usualSeries(t, e, acc, "steps", timeaxis.Month, "2026-01-01", "2026-07-01")

	for _, p := range s.Points {
		if p.Usual != nil {
			t.Fatalf("%s: Usual = %+v at month grain, want nil", p.Bucket, *p.Usual)
		}
	}
}

// TestUsualIsDrawnFromTheResolvedValues: a second Source claiming a flat 100 over the
// same days loses the election, so it is invisible to the Usual as it is to the points.
func TestUsualIsDrawnFromTheResolvedValues(t *testing.T) {
	e, models, acc := setup(t)
	vals := append(oneTo(28), 99)
	ms := dailyReadings("resting_heart_rate", "2026-06-01", vals, nil)
	for _, m := range dailyReadings("resting_heart_rate", "2026-06-01", vals, nil) {
		ms = append(ms, meas{m.metric, 100, m.at, "Zzz Other"})
	}
	seed(t, e.DB, models, acc, ms)

	s := usualSeries(t, e, acc, "resting_heart_rate", timeaxis.Day, "2026-06-29", "2026-06-30")

	assertUsual(t, pointAt(t, s, "2026-06-29"), 7.75, 21.25, 28)
}

// TestUsualOfADerivedMetric is drawn from its recomputed buckets: a balance of 1, 2,
// …, 28 over June reads as 7.75 to 21.25, whatever its operands' own spreads.
func TestUsualOfADerivedMetric(t *testing.T) {
	e, models, acc := setup(t)
	var ms []meas
	for i, v := range oneTo(29) {
		day := time.Date(2026, 6, 1+i, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		ms = append(ms,
			meas{"dietary_energy", 2000 + v, day + "T12:00:00Z", "MyFitnessPal"},
			meas{"active_energy", 400, day + "T18:00:00Z", "Watch"},
			meas{"basal_energy", 1600, day + "T23:00:00Z", "Watch"},
		)
	}
	seed(t, e.DB, models, acc, ms)

	s := usualSeries(t, e, acc, "calorie_balance", timeaxis.Day, "2026-06-29", "2026-06-30")

	assertUsual(t, pointAt(t, s, "2026-06-29"), 7.75, 21.25, 28)
}

// TestUsualOfSleepIsOverNights: time asleep of 401, 402, …, 428 minutes over the
// nights waking into 1 to 28 June gives the night into the 29th 407.75 to 421.25.
func TestUsualOfSleepIsOverNights(t *testing.T) {
	e, models, acc := setup(t)
	var rows []interval
	for i := 0; i < 29; i++ {
		start := time.Date(2026, 5, 31+i, 23, 0, 0, 0, time.UTC)
		end := start.Add(time.Duration(401+i) * time.Minute)
		rows = append(rows, interval{"asleep_core", start.Format(time.RFC3339), end.Format(time.RFC3339), "Watch"})
	}
	seedSleep(t, models, acc, rows)

	s := usualSeries(t, e, acc, "sleep", timeaxis.Day, "2026-06-29", "2026-06-30")

	assertUsual(t, pointAt(t, s, "2026-06-29"), 407.75, 421.25, 28)
}

// TestCompareReadsNoUsualForTheBaseline: the Baseline line is already the reference
// in comparison, so its Usual would be a second read nobody draws.
func TestCompareReadsNoUsualForTheBaseline(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, dailyReadings("resting_heart_rate", "2026-05-01", oneTo(60), nil))

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "resting_heart_rate", Bucket: timeaxis.Day, Usual: true,
		From: mustTime(t, "2026-06-29T00:00:00Z"), To: mustTime(t, "2026-06-30T00:00:00Z"),
	}, timeaxis.Window{From: mustTime(t, "2026-06-28T00:00:00Z"), To: mustTime(t, "2026-06-29T00:00:00Z")})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if pointAt(t, cmp.Current, "2026-06-29").Usual == nil {
		t.Error("current: no Usual")
	}
	if u := pointAt(t, cmp.Baseline, "2026-06-28").Usual; u != nil {
		t.Errorf("baseline: Usual = %+v, want nil", *u)
	}
}

// TestQuantileIsType7 pins the interpolation on cases checkable by hand, so a second
// implementation (a client, a spreadsheet) cannot disagree with the band.
func TestQuantileIsType7(t *testing.T) {
	cases := []struct {
		name     string
		sorted   []float64
		p25, p75 float64
	}{
		{"even", []float64{1, 2, 3, 4}, 1.75, 3.25},
		{"odd lands on ranks", []float64{1, 2, 3, 4, 5}, 2, 4},
		{"ties", []float64{5, 5, 5, 9}, 5, 6},
		{"one value", []float64{7}, 7, 7},
	}
	for _, c := range cases {
		if got := quantile(c.sorted, 0.25); got != c.p25 {
			t.Errorf("%s: p25 = %v, want %v", c.name, got, c.p25)
		}
		if got := quantile(c.sorted, 0.75); got != c.p75 {
			t.Errorf("%s: p75 = %v, want %v", c.name, got, c.p75)
		}
	}
}

// TestUsualAtWeekGrainReadsWholeWeeks: one step a day, so a whole week sums to 7,
// except the weeks of 19 and 26 January at half a step a day (3.5). A window from
// Wednesday 8 April to Thursday 23 April cuts its first and last weeks, and neither
// carries a Usual: a week the window only partly covers is not comparable to whole
// ones. Nor does the cut leak into the reference: the week of 13 April reads its
// twelve whole weeks before it, 6 April at 7 among them, so p25 falls between two
// sevens. Were the cut 6 April read at the 5 the window drew, p25 would be 6.5.
func TestUsualAtWeekGrainReadsWholeWeeks(t *testing.T) {
	e, models, acc := setup(t)
	days := make([]float64, 112) // 5 January to 26 April
	for i := range days {
		days[i] = 1
		if i >= 14 && i < 28 {
			days[i] = 0.5
		}
	}
	seed(t, e.DB, models, acc, dailyReadings("steps", "2026-01-05", days, nil))

	s := usualSeries(t, e, acc, "steps", timeaxis.Week, "2026-04-08", "2026-04-23")

	for _, cut := range []string{"2026-04-06", "2026-04-20"} {
		if u := pointAt(t, s, cut).Usual; u != nil {
			t.Errorf("week of %s is cut by the window: Usual = %+v, want nil", cut, *u)
		}
	}
	assertUsual(t, pointAt(t, s, "2026-04-13"), 7, 7, 12)
}
