package day

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
	"github.com/gauthier-se/verve/internal/timeaxis"
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

func read(t *testing.T, e Engine, acc int64, date string) Read {
	t.Helper()
	out, err := e.Read(context.Background(), acc, date)
	if err != nil {
		t.Fatalf("Read(%s): %v", date, err)
	}
	return out
}

// seed writes Measurements, filling in the Account and a unique content key.
func seed(t *testing.T, models data.Models, acc int64, ms ...data.Measurement) {
	t.Helper()
	for i := range ms {
		ms[i].AccountID = acc
		if ms[i].EndAt == "" {
			ms[i].EndAt = ms[i].StartAt
		}
		if ms[i].ContentKey == "" {
			ms[i].ContentKey = fmt.Sprintf("%s-%s-%s-%d", ms[i].Metric, ms[i].Source, ms[i].StartAt, i)
		}
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

// find returns the Metric row for a slug, and whether the Day carries one at all.
func find(rows []Metric, slug string) (Metric, bool) {
	for _, r := range rows {
		if r.Metric == slug {
			return r, true
		}
	}
	return Metric{}, false
}

// The Day's figure is the Series' Point, not a second arithmetic. This is the
// assertion that fails the day somebody folds a day here instead of asking the
// engine for it.
func TestDayFigureEqualsTheSeriesPoint(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "steps", Value: 4000, OriginalUnit: "count", StartAt: "2026-03-03T08:00:00Z", Source: "Apple Watch"},
		data.Measurement{Metric: "steps", Value: 2500, OriginalUnit: "count", StartAt: "2026-03-03T19:00:00Z", Source: "Apple Watch"},
	)

	day, _ := time.Parse(dayLayout, "2026-03-03")
	series, err := e.Query.Series(context.Background(), query.Request{
		AccountID: acc, Metric: "steps", Bucket: timeaxis.Day,
		From: day, To: day.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 1 {
		t.Fatalf("points = %+v, want one bucket", series.Points)
	}

	row, ok := find(read(t, e, acc, "2026-03-03").Metrics, "steps")
	if !ok || row.Value == nil {
		t.Fatalf("steps = %+v, want a value", row)
	}
	if *row.Value != series.Points[0].Value {
		t.Errorf("day = %v, series = %v: the two paths disagree about one date", *row.Value, series.Points[0].Value)
	}
	if row.Source != series.Source {
		t.Errorf("source = %q, series = %q", row.Source, series.Source)
	}
}

// A pinned Metric with no data that day is the most informative row on the page:
// it says the day is missing, not that the Metric is. An unpinned one is absent.
func TestPinnedMetricsAreRowsEvenWhenTheDayIsEmpty(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "steps", Value: 4000, OriginalUnit: "count", StartAt: "2026-03-03T08:00:00Z", Source: "Apple Watch"},
		data.Measurement{Metric: "body_mass", Value: 78.4, OriginalUnit: "kg", StartAt: "2026-03-01T08:00:00Z", Source: "Scale"},
	)
	if _, err := models.Pins.Add(context.Background(), acc, "resting_heart_rate"); err != nil {
		t.Fatalf("pin: %v", err)
	}

	rows := read(t, e, acc, "2026-03-03").Metrics

	pinned, ok := find(rows, "resting_heart_rate")
	if !ok {
		t.Fatalf("metrics = %+v, want the pinned Metric present", rows)
	}
	if pinned.Value != nil || !pinned.Pinned {
		t.Errorf("pinned row = %+v, want a pinned row with no value", pinned)
	}
	if rows[0].Metric != "resting_heart_rate" {
		t.Errorf("first row = %q, want the Pin ordered first", rows[0].Metric)
	}
	if _, ok := find(rows, "body_mass"); ok {
		t.Error("body_mass has no data on this date and is not pinned: it should have no row")
	}
}

// The Source is elected per day (ADR 0034), so a date reports its own winner and
// not the window's. Here the Watch wins the 3rd and the iPhone owns the 4th alone.
func TestSourceIsTheOneElectedForThatDate(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "steps", Value: 9000, OriginalUnit: "count", StartAt: "2026-03-03T08:00:00Z", Source: "Apple Watch de Gauthier"},
		data.Measurement{Metric: "steps", Value: 6000, OriginalUnit: "count", StartAt: "2026-03-03T09:00:00Z", Source: "iPhone de Gauthier"},
		data.Measurement{Metric: "steps", Value: 5000, OriginalUnit: "count", StartAt: "2026-03-04T09:00:00Z", Source: "iPhone de Gauthier"},
	)

	third, _ := find(read(t, e, acc, "2026-03-03").Metrics, "steps")
	if third.Source != "Apple Watch de Gauthier" || third.Value == nil || *third.Value != 9000 {
		t.Errorf("3 March = %+v, want the Watch's 9000", third)
	}
	fourth, _ := find(read(t, e, acc, "2026-03-04").Metrics, "steps")
	if fourth.Source != "iPhone de Gauthier" || fourth.Value == nil || *fourth.Value != 5000 {
		t.Errorf("4 March = %+v, want the iPhone's 5000, the day it won alone", fourth)
	}
}

// The Night on a Day is the one that woke into it: a night beginning at 23:14 on
// the 2nd is the 3rd's night, which is where the sleep Panel already draws it.
func TestNightIsTheOneThatWokeIntoTheDate(t *testing.T) {
	e, models, acc := setup(t)
	if _, err := models.States.InsertStateBatch(context.Background(), []data.State{{
		AccountID: acc, Kind: "sleep", StateValue: "asleep_core",
		StartAt: "2026-03-02T23:14:00Z", EndAt: "2026-03-03T06:44:00Z",
		Source: "Apple Watch", ContentKey: "n1",
	}}); err != nil {
		t.Fatalf("seed states: %v", err)
	}

	third := read(t, e, acc, "2026-03-03")
	if third.Night == nil {
		t.Fatal("3 March carries no Night, but that is the morning this one woke into")
	}
	if third.Night.Night != "2026-03-03" || third.Night.Asleep != 450 {
		t.Errorf("night = %+v, want the 3rd's 450 minutes", third.Night)
	}
	// The sleep Metric puts the same night on the same date, so the figure row and
	// the Night section cannot disagree.
	if row, ok := find(third.Metrics, "sleep"); !ok || row.Value == nil || *row.Value != third.Night.Asleep {
		t.Errorf("sleep row = %+v, want the Night's own %v minutes", row, third.Night.Asleep)
	}
	if second := read(t, e, acc, "2026-03-02"); second.Night != nil {
		t.Errorf("2 March carries %+v; the night that began that evening belongs to the 3rd", second.Night)
	}
}

// A workout belongs to the day it started, with none of the Night's shift: a ride
// that runs past midnight is one its owner names by the evening it began (ADR 0040).
func TestWorkoutBelongsToTheDayItStarted(t *testing.T) {
	e, models, acc := setup(t)
	s := data.Session{
		AccountID: acc, ActivityType: "running",
		StartAt: "2026-03-03T23:40:00Z", EndAt: "2026-03-04T00:55:00Z", Duration: 4500,
		Source: "Apple Watch", ContentKey: "w1",
	}
	if _, err := models.Sessions.InsertSession(context.Background(), &s); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if got := read(t, e, acc, "2026-03-03").Sessions; len(got) != 1 {
		t.Errorf("3 March has %d workouts, want the one that began that evening", len(got))
	}
	if got := read(t, e, acc, "2026-03-04").Sessions; len(got) != 0 {
		t.Errorf("4 March has %d workouts, want none: it only finished there", len(got))
	}
}

// An Exclusion covering the date makes the absence a refusal, which is the one
// thing no other screen can say (ADR 0033).
func TestAnExcludedMetricIsNotAGap(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "body_fat_percentage", Value: 0.27, OriginalUnit: "%", StartAt: "2026-03-03T08:00:00Z", Source: "Scale"},
		data.Measurement{Metric: "steps", Value: 4000, OriginalUnit: "count", StartAt: "2026-03-03T08:00:00Z", Source: "Apple Watch"},
	)
	if _, err := models.Pins.Add(context.Background(), acc, "body_fat_percentage"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	rule := &data.Exclusion{AccountID: acc, Metric: "body_fat_percentage"}
	if _, err := models.Exclusions.Insert(context.Background(), rule); err != nil {
		t.Fatalf("insert exclusion: %v", err)
	}

	out := read(t, e, acc, "2026-03-03")
	row, ok := find(out.Metrics, "body_fat_percentage")
	if !ok {
		t.Fatalf("metrics = %+v, want the excluded Metric present", out.Metrics)
	}
	if row.Value != nil || !row.Excluded {
		t.Errorf("row = %+v, want an excluded row with no value", row)
	}
	if steps, _ := find(out.Metrics, "steps"); steps.Excluded {
		t.Error("steps is not excluded and must not be marked so")
	}
	if len(out.Exclusions) != 1 || out.Exclusions[0].Metric != "body_fat_percentage" {
		t.Errorf("exclusions = %+v, want the one rule covering this date", out.Exclusions)
	}
}

// A date exists whether or not anything happened on it. This is the sharpest
// difference between a Day and every other addressable thing in Verve.
func TestAnEmptyDateIsAnEmptyDayAndNotAnError(t *testing.T) {
	e, _, acc := setup(t)
	out := read(t, e, acc, "1998-06-12")
	if len(out.Metrics) != 0 || len(out.Sessions) != 0 || len(out.Annotations) != 0 ||
		len(out.Manual) != 0 || len(out.Exclusions) != 0 || out.Night != nil || out.Phase != nil {
		t.Errorf("read = %+v, want an empty Day", out)
	}
	if out.Date != "1998-06-12" {
		t.Errorf("date = %q, want the date asked for", out.Date)
	}
}

func TestAMalformedDateIsRefused(t *testing.T) {
	e, _, acc := setup(t)
	for _, date := range []string{"last-tuesday", "2026-13-40", "2026-03", ""} {
		if _, err := e.Read(context.Background(), acc, date); err != ErrInvalidDate {
			t.Errorf("Read(%q) err = %v, want ErrInvalidDate", date, err)
		}
	}
}

// A Manual row comes back addressable, because the Day is where deleting one takes
// no search (ADR 0022).
func TestManualRowsAreAddressable(t *testing.T) {
	e, models, acc := setup(t)
	row := &data.Measurement{
		AccountID: acc, Metric: "body_mass", Value: 78.4, OriginalUnit: "kg",
		StartAt: "2026-03-03T07:00:00Z", EndAt: "2026-03-03T07:00:00Z",
		Source: "Manual", ContentKey: "manual-1",
	}
	if _, err := models.Measurements.InsertOne(context.Background(), row); err != nil {
		t.Fatalf("insert manual: %v", err)
	}

	out := read(t, e, acc, "2026-03-03")
	if len(out.Manual) != 1 || out.Manual[0].ID == 0 {
		t.Fatalf("manual = %+v, want one row carrying its id", out.Manual)
	}
	if _, ok := find(out.Metrics, "body_mass"); !ok {
		t.Error("a Manual entry is data on the day: it belongs in the figures too")
	}
}

// Annotations and the covering Phase are the context that explains a day rather
// than describing it.
func TestTheDayCarriesItsNotesAndItsPhase(t *testing.T) {
	e, models, acc := setup(t)
	note := &data.Annotation{AccountID: acc, Label: "flight to Tokyo", StartsOn: "2026-03-03"}
	if err := models.Annotations.Insert(context.Background(), note); err != nil {
		t.Fatalf("insert annotation: %v", err)
	}
	span := &data.Annotation{AccountID: acc, Label: "flu", StartsOn: "2026-02-28", EndsOn: ptr("2026-03-05")}
	if err := models.Annotations.Insert(context.Background(), span); err != nil {
		t.Fatalf("insert annotation: %v", err)
	}
	if _, err := models.Phases.Open(context.Background(), acc, -0.5, "2026-02-01T00:00:00Z"); err != nil {
		t.Fatalf("open phase: %v", err)
	}

	out := read(t, e, acc, "2026-03-03")
	if len(out.Annotations) != 2 {
		t.Errorf("annotations = %+v, want the dated note and the span still running", out.Annotations)
	}
	if out.Phase == nil || out.Phase.RatePctPerWeek != -0.5 {
		t.Errorf("phase = %+v, want the open Phase this date falls in", out.Phase)
	}
	// A date before the Phase opened falls in none, and says so with an absence.
	if before := read(t, e, acc, "2026-01-15"); before.Phase != nil {
		t.Errorf("phase = %+v on a date before it opened", before.Phase)
	}
}

func ptr(s string) *string { return &s }

// A refused Metric has no data by construction, so without its rule it would have no
// row at all and the one screen that can name a refusal would say nothing. The rule
// puts the row there, pinned or not.
func TestARefusedMetricGetsARowWithoutBeingPinned(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "walking_asymmetry_percentage", Value: 0.02, OriginalUnit: "%", StartAt: "2026-03-03T08:00:00Z", Source: "iPhone"},
	)
	rule := &data.Exclusion{AccountID: acc, Metric: "walking_asymmetry_percentage"}
	if _, err := models.Exclusions.Insert(context.Background(), rule); err != nil {
		t.Fatalf("insert exclusion: %v", err)
	}

	row, ok := find(read(t, e, acc, "2026-03-03").Metrics, "walking_asymmetry_percentage")
	if !ok {
		t.Fatal("the refused Metric has no row, so the refusal is invisible")
	}
	if row.Value != nil || !row.Excluded || row.Pinned {
		t.Errorf("row = %+v, want an unpinned excluded row with no value", row)
	}
}

// Over a window the Series reports the imported Source even when a Manual entry
// displaces a day, deliberately: the overlay is one day of many. On a Day the window
// *is* that day, so the same answer would name a device for a figure it did not
// produce.
func TestAManualValueNamesItselfAsTheSource(t *testing.T) {
	e, models, acc := setup(t)
	seed(t, models, acc,
		data.Measurement{Metric: "body_mass", Value: 91.6, OriginalUnit: "kg", StartAt: "2026-03-03T06:00:00Z", Source: "Zepp Life"},
		data.Measurement{Metric: "body_mass", Value: 91.9, OriginalUnit: "kg", StartAt: "2026-03-04T06:00:00Z", Source: "Zepp Life"},
	)
	typed := &data.Measurement{
		AccountID: acc, Metric: "body_mass", Value: 91.2, OriginalUnit: "kg",
		StartAt: "2026-03-03T07:30:00Z", EndAt: "2026-03-03T07:30:00Z",
		Source: "Manual", ContentKey: "manual-1",
	}
	if _, err := models.Measurements.InsertOne(context.Background(), typed); err != nil {
		t.Fatalf("insert manual: %v", err)
	}

	row, _ := find(read(t, e, acc, "2026-03-03").Metrics, "body_mass")
	if row.Source != "Manual" || row.Value == nil || *row.Value != 91.2 {
		t.Errorf("row = %+v, want the typed 91.2 named as Manual", row)
	}
	// The day next to it is untouched: the overlay displaces the day it falls on and
	// nothing else (ADR 0022).
	next, _ := find(read(t, e, acc, "2026-03-04").Metrics, "body_mass")
	if next.Source != "Zepp Life" || next.Value == nil || *next.Value != 91.9 {
		t.Errorf("row = %+v, want the scale's own reading the day after", next)
	}
}

// A Day carries the Goal in force on its own date, the same half-open rule the
// Panel's segments follow: none before the first, the new one on the day it changed.
func TestTheDayCarriesTheGoalInForceThatDate(t *testing.T) {
	e, models, acc := setup(t)
	for _, d := range []string{"2026-03-01", "2026-03-02", "2026-03-03"} {
		seed(t, models, acc, data.Measurement{Metric: "steps", Value: 8000, OriginalUnit: "count", StartAt: d + "T12:00:00Z", Source: "Apple Watch"})
	}
	ctx := context.Background()
	if _, err := models.Goals.Open(ctx, acc, "steps", data.GoalAtLeast, 7000, "2026-03-02"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := models.Goals.Open(ctx, acc, "steps", data.GoalAtLeast, 7500, "2026-03-03"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := map[string]*Goal{
		"2026-03-01": nil,
		"2026-03-02": {Direction: data.GoalAtLeast, Value: 7000},
		"2026-03-03": {Direction: data.GoalAtLeast, Value: 7500},
	}
	for date, goal := range want {
		row, ok := find(read(t, e, acc, date).Metrics, "steps")
		if !ok {
			t.Fatalf("%s: no steps row", date)
		}
		switch {
		case goal == nil && row.Goal != nil:
			t.Errorf("%s: goal = %+v, want none before the first Goal", date, row.Goal)
		case goal != nil && (row.Goal == nil || *row.Goal != *goal):
			t.Errorf("%s: goal = %+v, want %+v", date, row.Goal, goal)
		}
	}
}
