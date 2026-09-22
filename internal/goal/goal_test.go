package goal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

func setup(t *testing.T) (Engine, data.Models, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	acc := &data.Account{Email: "owner@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return Engine{Query: query.Engine{DB: db}, Models: models}, models, acc.ID
}

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(dayLayout, s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return d
}

// seedDaily writes one Measurement per date, at noon.
func seedDaily(t *testing.T, models data.Models, acc int64, metric string, values map[string]float64) {
	t.Helper()
	ms := make([]data.Measurement, 0, len(values))
	for date, v := range values {
		at := date + "T12:00:00Z"
		ms = append(ms, data.Measurement{
			AccountID: acc, Metric: metric, Value: v, OriginalUnit: "count",
			StartAt: at, EndAt: at, Source: "Apple Watch",
			ContentKey: fmt.Sprintf("%s-%s", metric, at),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func open(t *testing.T, models data.Models, acc int64, metric, direction string, value float64, start string) {
	t.Helper()
	if _, err := models.Goals.Open(context.Background(), acc, metric, direction, value, start); err != nil {
		t.Fatalf("Open goal: %v", err)
	}
}

func attain(t *testing.T, e Engine, acc int64, metric, from, to, now string) *query.Attainment {
	t.Helper()
	a, err := e.Attain(context.Background(), acc, metric, day(t, from), day(t, to), day(t, now).Add(15*time.Hour))
	if err != nil {
		t.Fatalf("Attain: %v", err)
	}
	return a
}

func wantCounts(t *testing.T, a *query.Attainment, covered, measured, met int) {
	t.Helper()
	if a == nil {
		t.Fatal("no Attainment, want one")
	}
	if a.Covered != covered || a.Measured != measured || a.Met != met {
		t.Errorf("counts = covered %d, measured %d, met %d; want %d, %d, %d",
			a.Covered, a.Measured, a.Met, covered, measured, met)
	}
}

func TestNoGoalIsNoAttainment(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{"2026-03-02": 9000})
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2026-04-01")

	if a := attain(t, e, acc, "steps", "2026-03-01", "2026-03-08", "2026-09-01"); a != nil {
		t.Errorf("Attainment = %+v before any Goal, want nil", a)
	}
}

// A gap is covered but neither measured nor met; a zero is measured. Both directions
// are inclusive at the bound.
func TestGapsAreNeitherMetNorMissed(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{
		"2026-03-01": 7500, // exactly at the bound: met
		"2026-03-02": 9000,
		"2026-03-03": 3000,
		"2026-03-05": 0, // a real zero is a missed day, not a gap
	})
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2026-01-01")

	a := attain(t, e, acc, "steps", "2026-03-01", "2026-03-08", "2026-09-01")
	wantCounts(t, a, 7, 4, 2)

	seedDaily(t, models, acc, "dietary_sodium", map[string]float64{"2026-03-01": 2300, "2026-03-02": 2301})
	open(t, models, acc, "dietary_sodium", data.GoalAtMost, 2300, "2026-01-01")
	wantCounts(t, attain(t, e, acc, "dietary_sodium", "2026-03-01", "2026-03-03", "2026-09-01"), 2, 2, 1)
}

// Each day is judged against the Goal in force on it, and the segments come back
// clipped to the window.
func TestAGoalChangedMidWindowJudgesEachDayByItsOwn(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{
		"2026-03-01": 7200, "2026-03-02": 7200, // met at 7 000
		"2026-03-03": 7200, "2026-03-04": 7200, // missed at 7 500
	})
	open(t, models, acc, "steps", data.GoalAtLeast, 7000, "2026-01-01")
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2026-03-03")

	a := attain(t, e, acc, "steps", "2026-03-01", "2026-03-05", "2026-09-01")
	wantCounts(t, a, 4, 4, 2)

	want := []query.GoalSegment{
		{Direction: data.GoalAtLeast, Value: 7000, From: "2026-03-01", To: "2026-03-03"},
		{Direction: data.GoalAtLeast, Value: 7500, From: "2026-03-03", To: "2026-03-05"},
	}
	if len(a.Segments) != len(want) {
		t.Fatalf("segments = %+v, want %+v", a.Segments, want)
	}
	for i := range want {
		if a.Segments[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, a.Segments[i], want[i])
		}
	}
}

// A Goal closed mid-window leaves the days after it uncovered, and a window starting
// before the first Goal counts only from it.
func TestOnlyDaysUnderAGoalAreCovered(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{
		"2026-03-01": 9000, "2026-03-03": 9000, "2026-03-06": 9000,
	})
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2026-03-03")
	goals, _ := models.Goals.ListByAccount(context.Background(), acc, "steps")
	if err := models.Goals.Close(context.Background(), acc, goals[0].ID, "2026-03-05"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantCounts(t, attain(t, e, acc, "steps", "2026-03-01", "2026-03-08", "2026-09-01"), 2, 1, 1)
}

// Today is never judged, even with a value in it and a window running past it.
func TestTodayIsNeverJudged(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{
		"2026-03-09": 9000, "2026-03-10": 9000, // the 10th is today
	})
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2026-03-01")

	a := attain(t, e, acc, "steps", "2026-03-09", "2026-03-12", "2026-03-10")
	wantCounts(t, a, 1, 1, 1)
	// The line still runs to the window's end: the Goal is in force, just not judged.
	if len(a.Segments) != 1 || a.Segments[0].To != "2026-03-12" {
		t.Errorf("segments = %+v, want one running to 2026-03-12", a.Segments)
	}

	// A Goal set today has nothing to judge yet, and is still reported.
	open(t, models, acc, "dietary_protein", data.GoalAtLeast, 150, "2026-03-10")
	wantCounts(t, attain(t, e, acc, "dietary_protein", "2026-03-09", "2026-03-12", "2026-03-10"), 0, 0, 0)
}

// Sleep is judged per Night, keyed on the morning it wakes into (ADR 0027), including
// today's, which is complete by morning and still not judged.
func TestSleepIsJudgedPerNight(t *testing.T) {
	e, models, acc := setup(t)
	nights := []data.State{
		// Woke on the 2nd: 7 h 30 asleep, met.
		{StateValue: "asleep_core", StartAt: "2026-03-01T23:00:00Z", EndAt: "2026-03-02T06:30:00Z"},
		// Woke on the 3rd: 6 h, missed.
		{StateValue: "asleep_core", StartAt: "2026-03-03T00:30:00Z", EndAt: "2026-03-03T06:30:00Z"},
		// Woke on the 4th, which is today.
		{StateValue: "asleep_core", StartAt: "2026-03-03T23:00:00Z", EndAt: "2026-03-04T07:00:00Z"},
	}
	for i := range nights {
		nights[i].AccountID, nights[i].Kind, nights[i].Source = acc, "sleep", "Apple Watch"
		nights[i].ContentKey = fmt.Sprintf("n-%d", i)
	}
	if _, err := models.States.InsertStateBatch(context.Background(), nights); err != nil {
		t.Fatalf("seed nights: %v", err)
	}
	open(t, models, acc, "sleep", data.GoalAtLeast, 420, "2026-01-01")

	wantCounts(t, attain(t, e, acc, "sleep", "2026-03-01", "2026-03-05", "2026-03-04"), 3, 2, 1)
}

// A derived Metric missing an operand on a day is a gap there (ADR 0014), and a
// negative bound is judged like any other.
func TestADerivedGapIsUnmeasured(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "dietary_energy", map[string]float64{"2026-03-01": 2000, "2026-03-02": 2600})
	seedDaily(t, models, acc, "active_energy", map[string]float64{"2026-03-01": 500, "2026-03-02": 500, "2026-03-03": 500})
	seedDaily(t, models, acc, "basal_energy", map[string]float64{"2026-03-01": 1800, "2026-03-02": 1800, "2026-03-03": 1800})
	open(t, models, acc, "calorie_balance", data.GoalAtMost, -300, "2026-01-01")

	// −300 on the 1st (met at the bound), +300 on the 2nd, no intake on the 3rd.
	wantCounts(t, attain(t, e, acc, "calorie_balance", "2026-03-01", "2026-03-04", "2026-09-01"), 3, 2, 1)
}

// An `all` range spans more days than one day-grain read may, and is counted whole.
func TestALongWindowIsCountedWhole(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{
		"2020-01-15": 9000, "2023-06-01": 9000, "2026-02-27": 3000,
	})
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2020-01-01")

	from, to := day(t, "2020-01-01"), day(t, "2026-03-01")
	covered := int(to.Sub(from).Hours() / 24)
	wantCounts(t, attain(t, e, acc, "steps", "2020-01-01", "2026-03-01", "2026-09-01"), covered, 3, 2)
}

// An Excluded day holds nothing to judge: the refusal purged it, so it is a gap.
func TestAnExcludedDayIsUnmeasured(t *testing.T) {
	e, models, acc := setup(t)
	seedDaily(t, models, acc, "steps", map[string]float64{"2026-03-01": 9000, "2026-03-02": 9000})
	ex := data.Exclusion{AccountID: acc, Metric: "steps", StartsOn: "2026-03-02", EndsOn: "2026-03-02"}
	if _, err := models.Exclusions.Insert(context.Background(), &ex); err != nil {
		t.Fatalf("exclude: %v", err)
	}
	open(t, models, acc, "steps", data.GoalAtLeast, 7500, "2026-01-01")

	wantCounts(t, attain(t, e, acc, "steps", "2026-03-01", "2026-03-03", "2026-09-01"), 2, 1, 1)
}

// A latest Metric is judged on the day's last reading, and a day nobody weighed in
// is a gap: the last weigh-in is never carried onto the days after it (ADR 0048).
func TestALatestMetricIsJudgedOnTheDaysLastReading(t *testing.T) {
	e, models, acc := setup(t)
	ms := []data.Measurement{
		// 1 March: 76.0 in the morning, 74.8 in the evening; the evening is the day.
		{Metric: "body_mass", Value: 76.0, StartAt: "2026-03-01T07:00:00Z"},
		{Metric: "body_mass", Value: 74.8, StartAt: "2026-03-01T21:00:00Z"},
		// 2 March: no reading. 3 March: over the bound.
		{Metric: "body_mass", Value: 75.2, StartAt: "2026-03-03T07:00:00Z"},
	}
	for i := range ms {
		ms[i].AccountID, ms[i].OriginalUnit, ms[i].Source = acc, "kg", "Withings"
		ms[i].EndAt, ms[i].ContentKey = ms[i].StartAt, "body_mass-"+ms[i].StartAt
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed: %v", err)
	}
	open(t, models, acc, "body_mass", data.GoalAtMost, 75, "2026-01-01")

	// Four days covered, two weighed, one of them under 75 kg.
	wantCounts(t, attain(t, e, acc, "body_mass", "2026-03-01", "2026-03-05", "2026-09-01"), 4, 2, 1)
}
