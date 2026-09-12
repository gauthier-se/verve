package query

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// The Ledger's windows are relative to the now it is given, so these seed against
// a fixed now rather than against the wall clock: the fold is what is under test,
// not the clock.
var ledgerNow = time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

// daysBefore renders an RFC 3339 UTC timestamp n days before ledgerNow.
func daysBefore(n int) string {
	return ledgerNow.AddDate(0, 0, -n).Format(time.RFC3339)
}

// findRow returns the Ledger row for a metric slug, failing if it is absent.
func findRow(t *testing.T, rows []LedgerRow, metric string) LedgerRow {
	t.Helper()
	for _, r := range rows {
		if r.Metric == metric {
			return r
		}
	}
	t.Fatalf("ledger rows %+v carry no %q row", rows, metric)
	return LedgerRow{}
}

// assertFloat fails unless got is non-nil and within a small epsilon of want.
func assertFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func TestLedgerFoldsWindowsAndWeekDelta(t *testing.T) {
	e, models, acc := setup(t)
	// A `sum` Metric: 700 steps this week (2 days ago) and 350 the week before
	// (9 days ago). Week figure is the 7-day total ÷ 7 = 100/day; the previous
	// week is 350 ÷ 7 = 50/day, so the delta is +50/day (+100%). The 30-day window
	// holds both: 1050 ÷ 30 = 35/day.
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "steps", Value: 700, OriginalUnit: "count", StartAt: daysBefore(2), EndAt: daysBefore(2), Source: "Watch", ContentKey: "s1"},
		{Metric: "steps", Value: 350, OriginalUnit: "count", StartAt: daysBefore(9), EndAt: daysBefore(9), Source: "Watch", ContentKey: "s2"},
	})

	rows, err := e.Ledger(context.Background(), acc, ledgerNow)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}

	steps := findRow(t, rows, "steps")
	if steps.Unit != "count" || steps.Aggregation != "sum" {
		t.Errorf("steps row = %+v, want unit count / sum", steps)
	}
	assertFloat(t, "week", steps.Week, 100)
	assertFloat(t, "month", steps.Month, 35)
	assertFloat(t, "delta_abs", steps.DeltaAbs, 50)
	assertFloat(t, "delta_pct", steps.DeltaPct, 100)
	// Latest is the most recent daily bucket: 700 steps two days ago.
	if steps.Latest == nil || steps.Latest.Value != 700 {
		t.Fatalf("latest = %+v, want value 700", steps.Latest)
	}
	if want := ledgerNow.AddDate(0, 0, -2).Format("2006-01-02"); steps.Latest.Date != want {
		t.Errorf("latest date = %q, want %q", steps.Latest.Date, want)
	}
}

func TestLedgerOnlyListsMetricsWithData(t *testing.T) {
	e, models, acc := setup(t)
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "body_mass", Value: 78.4, OriginalUnit: "kg", StartAt: daysBefore(1), EndAt: daysBefore(1), Source: "Health", ContentKey: "m1"},
	})

	rows, err := e.Ledger(context.Background(), acc, ledgerNow)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	// Only the one seeded Metric appears — no empty rows for the rest of the Catalog.
	if len(rows) != 1 || rows[0].Metric != "body_mass" {
		t.Fatalf("rows = %+v, want only body_mass", rows)
	}
	// A `latest` Metric is not divided by the window; its figure is the value itself.
	assertFloat(t, "week", rows[0].Week, 78.4)
	if rows[0].Latest == nil || rows[0].Latest.Value != 78.4 {
		t.Errorf("latest = %+v, want 78.4", rows[0].Latest)
	}
}

// seedNights seeds one whole-night sleep State per entry, each starting an hour
// before midnight of the Night it belongs to, so the Night label is n days before
// ledgerNow.
func seedNights(t *testing.T, models data.Models, acc int64, minutesByNightsAgo map[int]float64) {
	t.Helper()
	var batch []data.State
	for nightsAgo, minutes := range minutesByNightsAgo {
		night := ledgerNow.AddDate(0, 0, -nightsAgo)
		start := time.Date(night.Year(), night.Month(), night.Day(), 0, 0, 0, 0, time.UTC).Add(-time.Hour)
		batch = append(batch, data.State{
			AccountID: acc, Kind: "sleep", StateValue: "asleep_core",
			StartAt:    start.Format(time.RFC3339),
			EndAt:      start.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339),
			Source:     "Apple Watch",
			ContentKey: fmt.Sprintf("night-%d", nightsAgo),
		})
	}
	if _, err := models.States.InsertStateBatch(context.Background(), batch); err != nil {
		t.Fatalf("seed states: %v", err)
	}
}

// TestLedgerSleepFoldsPerNight: sleep joins the scoreboard from the States family,
// and its window figures divide by the Nights that hold data — never by the window's
// days, which would report a shortfall the Account never had (ADR 0027).
func TestLedgerSleepFoldsPerNight(t *testing.T) {
	e, models, acc := setup(t)
	// Three nights in the last week, of 420, 480 and 300 minutes: 1200 over 3 nights
	// is 400 a night. Over 7 days it would be 171 — the wrong answer this pins.
	seedNights(t, models, acc, map[int]float64{1: 420, 2: 480, 4: 300})

	rows, err := e.Ledger(context.Background(), acc, ledgerNow)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}

	sleep := findRow(t, rows, "sleep")
	if sleep.Unit != "min" || sleep.Aggregation != "duration_by_state" {
		t.Errorf("sleep row = %+v, want unit min / duration_by_state", sleep)
	}
	assertFloat(t, "week", sleep.Week, 400)
	assertFloat(t, "month", sleep.Month, 400)
	if sleep.Latest == nil || sleep.Latest.Value != 420 {
		t.Errorf("latest = %+v, want last night's 420 min", sleep.Latest)
	}
	if want := ledgerNow.AddDate(0, 0, -1).Format("2006-01-02"); sleep.Latest.Date != want {
		t.Errorf("latest date = %q, want the night that woke on %s", sleep.Latest.Date, want)
	}
}

// TestLedgerOmitsSleepWithoutStates: an Account with no sleep gets no sleep row, and
// no error for the family it has never imported.
func TestLedgerOmitsSleepWithoutStates(t *testing.T) {
	e, models, acc := setup(t)
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "steps", Value: 700, OriginalUnit: "count", StartAt: daysBefore(2), EndAt: daysBefore(2), Source: "Watch", ContentKey: "s1"},
	})

	rows, err := e.Ledger(context.Background(), acc, ledgerNow)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	for _, r := range rows {
		if r.Metric == "sleep" {
			t.Fatalf("rows = %+v, want no sleep row for an Account with no States", rows)
		}
	}
}

// TestMetricsWithDataSeesBothFamilies pins the reason withSleep no longer exists:
// the engine answers the whole question, so a caller never has to know that sleep
// is stored as States rather than as Measurements.
func TestMetricsWithDataSeesBothFamilies(t *testing.T) {
	e, models, acc := setup(t)
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "steps", Value: 700, OriginalUnit: "count", StartAt: daysBefore(2), EndAt: daysBefore(2), Source: "Watch", ContentKey: "s1"},
	})
	seedNights(t, models, acc, map[int]float64{1: 420})

	got, err := e.MetricsWithData(context.Background(), acc)
	if err != nil {
		t.Fatalf("MetricsWithData: %v", err)
	}
	want := []string{"sleep", "steps"} // sorted
	if len(got) != len(want) {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("metrics = %v, want %v", got, want)
		}
	}
}

// seedRows inserts measurements verbatim, so a Ledger test can name the unit and
// the content key it needs rather than the shared meas shape's placeholders.
func seedRows(t *testing.T, models data.Models, acc int64, ms []data.Measurement) {
	t.Helper()
	for i := range ms {
		ms[i].AccountID = acc
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

// Training volume is folded from the Sessions family (ADR 0040), which the
// DISTINCT over measurements cannot see any more than it sees sleep. An Account
// whose only data is workouts still gets its rows.
func TestMetricsWithDataSeesTheSessionsFamily(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: daysBefore(2), minutes: 45, km: km(9)},
	})

	got, err := e.MetricsWithData(context.Background(), acc)
	if err != nil {
		t.Fatalf("MetricsWithData: %v", err)
	}
	want := []string{"training_distance", "training_time"} // sorted
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
}

// An Account with no workouts gets neither row, so the scoreboard shows no empty
// lines (ADR 0021).
func TestMetricsWithDataOmitsTrainingWithoutSessions(t *testing.T) {
	e, models, acc := setup(t)
	seedRows(t, models, acc, []data.Measurement{
		{Metric: "steps", Value: 700, OriginalUnit: "count", StartAt: daysBefore(2), EndAt: daysBefore(2), Source: "Watch", ContentKey: "s1"},
	})

	got, err := e.MetricsWithData(context.Background(), acc)
	if err != nil {
		t.Fatalf("MetricsWithData: %v", err)
	}
	for _, slug := range got {
		if slug == "training_time" || slug == "training_distance" {
			t.Fatalf("metrics = %v, want no training row for an Account with no workouts", got)
		}
	}
}

// The Ledger's per-day figure divides a volume by every calendar day, rest days
// included. Dividing by active days instead would be a per-session average
// wearing a per-day label, and the columns are fixed so one Metric cannot mean
// something else than its neighbours (ADR 0021).
func TestLedgerTrainingDividesByCalendarDays(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: daysBefore(2), minutes: 70},
	})

	rows, err := e.Ledger(context.Background(), acc, ledgerNow)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	var row *LedgerRow
	for i := range rows {
		if rows[i].Metric == "training_time" {
			row = &rows[i]
		}
	}
	if row == nil {
		t.Fatalf("rows = %+v, want a training_time row", rows)
	}
	if row.Week == nil {
		t.Fatal("the training row has no week figure")
	}
	if got := *row.Week; got != 10 {
		t.Errorf("week figure = %v, want 70 minutes over 7 days", got)
	}
}

// An Account that only lifts has workouts and no kilometres. It gets the time row
// and not the distance one, because a row of dashes is the empty row the Ledger
// exists not to show (ADR 0021).
func TestMetricsWithDataOmitsDistanceWithoutOne(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "traditional_strength_training", start: daysBefore(2), minutes: 60},
	})

	got, err := e.MetricsWithData(context.Background(), acc)
	if err != nil {
		t.Fatalf("MetricsWithData: %v", err)
	}
	if len(got) != 1 || got[0] != "training_time" {
		t.Fatalf("metrics = %v, want training_time alone", got)
	}
}
