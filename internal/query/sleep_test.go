package query

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// interval is a compact test State: one Stage over one span, from one Source.
type interval struct {
	stage  string
	start  string // RFC 3339
	end    string // RFC 3339
	source string
}

func seedSleep(t *testing.T, models data.Models, acc int64, rows []interval) {
	t.Helper()
	batch := make([]data.State, len(rows))
	for i, r := range rows {
		batch[i] = data.State{
			AccountID: acc, Kind: "sleep", StateValue: r.stage,
			StartAt: r.start, EndAt: r.end, Source: r.source,
			ContentKey: fmt.Sprintf("s-%d-%s-%s-%s", i, r.stage, r.source, r.start),
		}
	}
	if _, err := models.States.InsertStateBatch(context.Background(), batch); err != nil {
		t.Fatalf("seed states: %v", err)
	}
}

// sleepSeries runs a day-bucket sleep query over [from, to).
func sleepSeries(t *testing.T, e Engine, acc int64, from, to string, bucket Bucket) Series {
	t.Helper()
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "sleep", Bucket: bucket,
		From: mustTime(t, from), To: mustTime(t, to),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	return s
}

// TestSleepNightSpansMidnight pins the Night: an interval crossing midnight is one
// bucket, labelled by the morning it wakes into — not two half-nights.
func TestSleepNightSpansMidnight(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-04T06:00:00Z", "Watch"},
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if len(s.Points) != 1 {
		t.Fatalf("points = %+v, want one night", s.Points)
	}
	if !samePoint(s.Points[0], Point{
		Bucket: "2024-01-04", Value: 420, States: map[string]float64{"asleep_core": 420},
	}) {
		t.Errorf("point = %+v, want 420 min on the waking day 2024-01-04", s.Points[0])
	}
	if s.Nights != 1 {
		t.Errorf("nights = %d, want 1", s.Nights)
	}
	if s.Unit != "min" || s.Aggregation != "duration_by_state" {
		t.Errorf("unit/aggregation = %q/%q, want min/duration_by_state", s.Unit, s.Aggregation)
	}
}

// TestSleepFragmentedNightStaysOneBucket is the regression that kills every
// attribute-by-instant rule: real Apple data is dozens of short rows per night, and
// the ones after midnight must not file themselves under the following day.
func TestSleepFragmentedNightStaysOneBucket(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-03T23:40:00Z", "Watch"}, // 40
		{"asleep_deep", "2024-01-03T23:40:00Z", "2024-01-04T01:10:00Z", "Watch"}, // 90, crosses
		{"asleep_core", "2024-01-04T01:10:00Z", "2024-01-04T03:00:00Z", "Watch"}, // 110
		{"asleep_rem", "2024-01-04T03:00:00Z", "2024-01-04T06:00:00Z", "Watch"},  // 180
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if len(s.Points) != 1 {
		t.Fatalf("points = %+v, want one night, not one per side of midnight", s.Points)
	}
	want := Point{Bucket: "2024-01-04", Value: 420, States: map[string]float64{
		"asleep_core": 150, "asleep_deep": 90, "asleep_rem": 180,
	}}
	if !samePoint(s.Points[0], want) {
		t.Errorf("point = %+v, want %+v", s.Points[0], want)
	}
}

// TestSleepAwakeIsReportedNotCounted: the stack shows the interruptions, the figure
// does not count them as sleep.
func TestSleepAwakeIsReportedNotCounted(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-04T02:00:00Z", "Watch"}, // 180
		{"awake", "2024-01-04T02:00:00Z", "2024-01-04T02:20:00Z", "Watch"},       // 20
		{"asleep_rem", "2024-01-04T02:20:00Z", "2024-01-04T06:00:00Z", "Watch"},  // 220
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	want := Point{Bucket: "2024-01-04", Value: 400, States: map[string]float64{
		"asleep_core": 180, "asleep_rem": 220, "awake": 20,
	}}
	if len(s.Points) != 1 || !samePoint(s.Points[0], want) {
		t.Errorf("points = %+v, want %+v", s.Points, want)
	}
}

// TestSleepInBedResolvedPerNight is the case whole-window Source ranking gets wrong:
// one night has Watch stages beside an iPhone in-bed row, the next has the iPhone
// alone. Ranking over the range would either double the first night or delete the
// second; per Night, both are right.
func TestSleepInBedResolvedPerNight(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		// Night of 2024-01-04: the Watch staged it, the iPhone also called it in-bed.
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-04T06:00:00Z", "Apple Watch"},
		{"in_bed", "2024-01-03T22:30:00Z", "2024-01-04T06:30:00Z", "iPhone"},
		// Night of 2024-01-05: the Watch was on its charger.
		{"in_bed", "2024-01-04T23:00:00Z", "2024-01-05T07:00:00Z", "iPhone"},
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want both nights", s.Points)
	}
	staged := Point{Bucket: "2024-01-04", Value: 420, States: map[string]float64{"asleep_core": 420}}
	if !samePoint(s.Points[0], staged) {
		t.Errorf("staged night = %+v, want %+v (in-bed dropped beside stages)", s.Points[0], staged)
	}
	inBed := Point{Bucket: "2024-01-05", Value: 480, States: map[string]float64{"in_bed": 480}}
	if !samePoint(s.Points[1], inBed) {
		t.Errorf("in-bed night = %+v, want %+v (the only evidence, so it counts)", s.Points[1], inBed)
	}
	if s.Nights != 2 {
		t.Errorf("nights = %d, want 2", s.Nights)
	}
}

// TestSleepStagedSourceWinsTheTie: when two Sources both staged the same night, the
// standing priority breaks it (ADR 0003) rather than the row order.
func TestSleepStagedSourceWinsTheTie(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-04T06:00:00Z", "Apple Watch"}, // 420
		{"asleep_core", "2024-01-03T23:30:00Z", "2024-01-04T05:00:00Z", "iPhone"},      // 330
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if len(s.Points) != 1 || s.Points[0].Value != 420 {
		t.Fatalf("points = %+v, want the Watch's 420 min alone", s.Points)
	}
	if s.Source != "Apple Watch" {
		t.Errorf("source = %q, want Apple Watch", s.Source)
	}
}

// TestSleepWeekBucketSumsItsNights also pins that a Night lands in the week its own
// label falls in — the Sunday-into-Monday night belongs to the week it wakes into.
func TestSleepWeekBucketSumsItsNights(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		// Nights of Tue 2 and Wed 3 January (ISO week starting Mon 1 January).
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T05:00:00Z", "Watch"}, // 360
		{"asleep_core", "2024-01-02T23:00:00Z", "2024-01-03T04:00:00Z", "Watch"}, // 300
		// The night of Sun 7 into Mon 8: it wakes on the 8th, so it is the next week's.
		{"asleep_core", "2024-01-07T23:00:00Z", "2024-01-08T06:00:00Z", "Watch"}, // 420
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-15T00:00:00Z", Week)
	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want two weeks", s.Points)
	}
	if !samePoint(s.Points[0], Point{Bucket: "2024-01-01", Value: 660,
		States: map[string]float64{"asleep_core": 660}}) {
		t.Errorf("first week = %+v, want 660 min", s.Points[0])
	}
	if !samePoint(s.Points[1], Point{Bucket: "2024-01-08", Value: 420,
		States: map[string]float64{"asleep_core": 420}}) {
		t.Errorf("second week = %+v, want the Sunday-into-Monday night", s.Points[1])
	}
}

// TestSleepNightBelongsToAWindowWhole: the window predicate is on the Night, so the
// evening half of a night at the edge never appears as a truncated night.
func TestSleepNightBelongsToAWindowWhole(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		// Wakes on 2024-01-08, the first day outside a [01-01, 01-08) window.
		{"asleep_core", "2024-01-07T23:00:00Z", "2024-01-07T23:59:00Z", "Watch"},
		{"asleep_core", "2024-01-08T00:00:00Z", "2024-01-08T06:00:00Z", "Watch"},
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if len(s.Points) != 0 {
		t.Errorf("points = %+v, want none: the night wakes outside the window", s.Points)
	}

	// The same night, in the window it wakes into, is whole — both halves.
	next := sleepSeries(t, e, acc, "2024-01-08T00:00:00Z", "2024-01-15T00:00:00Z", Day)
	if len(next.Points) != 1 || next.Points[0].Value != 419 {
		t.Errorf("points = %+v, want one whole 419 min night", next.Points)
	}
}

// TestSleepEmptyWindow: no data is an empty series, not an error and not a zero.
func TestSleepEmptyWindow(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-04T06:00:00Z", "Watch"},
	})

	s := sleepSeries(t, e, acc, "2024-02-01T00:00:00Z", "2024-02-08T00:00:00Z", Day)
	if s.Points == nil || len(s.Points) != 0 {
		t.Errorf("points = %+v, want empty and non-nil", s.Points)
	}
	if s.Source != "" || s.Summary != nil || s.Nights != 0 {
		t.Errorf("source/summary/nights = %q/%+v/%d, want empty, nil, 0", s.Source, s.Summary, s.Nights)
	}
}

// TestSleepSummaryAndNights: the summary keeps ADR 0019's rule (one bucket over the
// range), and Nights is the honest denominator beside it — nights with data, never
// the window's days.
func TestSleepSummaryAndNights(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T05:00:00Z", "Watch"}, // 360
		{"asleep_deep", "2024-01-03T23:00:00Z", "2024-01-04T06:00:00Z", "Watch"}, // 420
		// 2024-01-05 and 01-06: the watch was off. Three days, two nights.
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if s.Summary == nil || s.Summary.Value != 780 {
		t.Fatalf("summary = %+v, want 780 min over the window", s.Summary)
	}
	if got := s.Summary.States["asleep_deep"]; got != 420 {
		t.Errorf("summary states = %+v, want the per-Stage totals", s.Summary.States)
	}
	if s.Days != 7 {
		t.Errorf("days = %d, want 7", s.Days)
	}
	if s.Nights != 2 {
		t.Errorf("nights = %d, want 2 — the divisor is nights with data, not days", s.Nights)
	}
	// The figure the client shows: 390 min a night, not 780 ÷ 7 = 111.
	if perNight := s.Summary.Value / float64(s.Nights); perNight != 390 {
		t.Errorf("per night = %v, want 390", perNight)
	}
}

// TestSleepAccountIsolation: another Account's nights are invisible (ADR 0007).
func TestSleepAccountIsolation(t *testing.T) {
	e, models, acc := setup(t)
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	seedSleep(t, models, other.ID, []interval{
		{"asleep_core", "2024-01-03T23:00:00Z", "2024-01-04T06:00:00Z", "Watch"},
	})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", Day)
	if len(s.Points) != 0 {
		t.Errorf("points = %+v, want none of the other Account's sleep", s.Points)
	}
}

// TestSleepComparesLikeAnyMetric: period comparison needs no sleep-specific code,
// because Compare goes through Series. A test, not a feature.
func TestSleepComparesLikeAnyMetric(t *testing.T) {
	e, models, acc := setup(t)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", "2024-01-08T23:00:00Z", "2024-01-09T06:00:00Z", "Watch"}, // 420, current
		{"asleep_core", "2024-01-01T23:00:00Z", "2024-01-02T05:00:00Z", "Watch"}, // 360, baseline
	})

	cmp, err := e.Compare(context.Background(), Request{
		AccountID: acc, Metric: "sleep", Bucket: Day,
		From: mustTime(t, "2024-01-08T00:00:00Z"), To: mustTime(t, "2024-01-15T00:00:00Z"),
	}, mustTime(t, "2024-01-01T00:00:00Z"), mustTime(t, "2024-01-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if cmp.Current.Summary == nil || cmp.Current.Summary.Value != 420 {
		t.Errorf("current summary = %+v, want 420", cmp.Current.Summary)
	}
	if cmp.Baseline.Summary == nil || cmp.Baseline.Summary.Value != 360 {
		t.Errorf("baseline summary = %+v, want 360", cmp.Baseline.Summary)
	}
}

// TestNonSleepPointsCarryNoStates: every other Metric's payload is untouched by the
// breakdown — the field is omitted, not empty.
func TestNonSleepPointsCarryNoStates(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, e.DB, models, acc, []meas{{"steps", 100, "2024-01-01T08:00:00Z", "Watch"}})

	s := sleepSeries(t, e, acc, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", Day)
	_ = s
	steps, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "steps", Bucket: Day,
		From: mustTime(t, "2024-01-01T00:00:00Z"), To: mustTime(t, "2024-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(steps.Points) != 1 || steps.Points[0].States != nil {
		t.Errorf("steps point = %+v, want no States", steps.Points)
	}
	if steps.Nights != 0 {
		t.Errorf("nights = %d, want 0 for a measurement Metric", steps.Nights)
	}
}

// TestSleepNowRelativeWindowKeepsLastNight: the Ledger asks for the last seven days
// from *now*, not from midnight. Truncating those bounds would drop the night that
// woke this morning — the one a person most wants to see — until tomorrow.
func TestSleepNowRelativeWindowKeepsLastNight(t *testing.T) {
	e, models, acc := setup(t)
	now := time.Now().UTC()
	// Last night: fell asleep before midnight, woke this morning.
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-2 * time.Hour)
	seedSleep(t, models, acc, []interval{
		{"asleep_core", start.Format(time.RFC3339), start.Add(7 * time.Hour).Format(time.RFC3339), "Watch"},
	})

	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: "sleep", Bucket: Day,
		From: now.AddDate(0, 0, -7), To: now,
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if s.Nights != 1 || len(s.Points) != 1 {
		t.Fatalf("points = %+v (nights %d), want last night", s.Points, s.Nights)
	}
	if want := now.Format(dayLayout); s.Points[0].Bucket != want {
		t.Errorf("bucket = %q, want today %q — the morning it woke into", s.Points[0].Bucket, want)
	}
	if s.Points[0].Value != 420 {
		t.Errorf("value = %v, want the whole 420 min", s.Points[0].Value)
	}
}
