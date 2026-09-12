package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/timeaxis"
)

// workout is a compact test Session: one Activity over one span, with the two
// figures the training Metrics read.
type workout struct {
	activity string
	start    string // RFC 3339
	minutes  float64
	km       *float64 // nil for a Session the source reported no distance for
	source   string
}

func km(v float64) *float64 { return &v }

func seedWorkouts(t *testing.T, models data.Models, acc int64, rows []workout) {
	t.Helper()
	for i, r := range rows {
		source := r.source
		if source == "" {
			source = "Watch"
		}
		s := &data.Session{
			AccountID: acc, ActivityType: r.activity,
			StartAt: r.start, EndAt: r.start, // the end is unread: the duration column is the figure
			Duration: r.minutes * 60, TotalDistance: r.km, Source: source,
			ContentKey: fmt.Sprintf("w-%d-%s-%s-%s", i, r.activity, source, r.start),
		}
		if _, err := models.Sessions.InsertWorkout(context.Background(), s, nil, nil); err != nil {
			t.Fatalf("seed workout: %v", err)
		}
	}
}

// trainingSeries runs a training query over [from, to).
func trainingSeries(t *testing.T, e Engine, acc int64, metric, from, to string, bucket timeaxis.Bucket) Series {
	t.Helper()
	s, err := e.Series(context.Background(), Request{
		AccountID: acc, Metric: metric, Bucket: bucket,
		From: mustTime(t, from), To: mustTime(t, to),
	})
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	return s
}

// The basic fold: minutes per day, broken down by Activity, with the Point's value
// the bucket's total across every Activity in it.
func TestTrainingTimeFoldsByDayAndActivity(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-01-01T06:00:00Z", minutes: 45, km: km(9)},
		{activity: "cycling", start: "2024-01-01T18:00:00Z", minutes: 90, km: km(40)},
		{activity: "running", start: "2024-01-02T06:00:00Z", minutes: 30, km: km(6)},
	})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z", timeaxis.Day)

	if s.Unit != "min" || s.Aggregation != "sum_by_state" {
		t.Fatalf("series = %s/%s, want min/sum_by_state", s.Unit, s.Aggregation)
	}
	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want two days", s.Points)
	}
	first := s.Points[0]
	if first.Bucket != "2024-01-01" || first.Value != 135 {
		t.Errorf("day one = %s %v, want 2024-01-01 135 min", first.Bucket, first.Value)
	}
	if first.States["running"] != 45 || first.States["cycling"] != 90 {
		t.Errorf("day one breakdown = %v, want running 45 and cycling 90", first.States)
	}
	if first.Count != 2 {
		t.Errorf("day one count = %d, want the 2 sessions behind it", first.Count)
	}
	if s.Summary == nil || s.Summary.Value != 165 {
		t.Fatalf("summary = %+v, want the window total of 165 min", s.Summary)
	}
	if s.Summary.States["running"] != 75 {
		t.Errorf("summary breakdown = %v, want running 75 over the window", s.Summary.States)
	}
}

// Kilometres are the other column of the same rows, and a Session the source gave
// no distance for contributes nothing rather than a zero.
func TestTrainingDistanceSkipsASessionWithoutOne(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-01-01T06:00:00Z", minutes: 45, km: km(9)},
		{activity: "traditional_strength_training", start: "2024-01-01T18:00:00Z", minutes: 60},
	})

	distance := trainingSeries(t, e, acc, "training_distance", "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", timeaxis.Day)
	if len(distance.Points) != 1 {
		t.Fatalf("points = %+v, want one day", distance.Points)
	}
	p := distance.Points[0]
	if p.Value != 9 {
		t.Errorf("distance = %v, want the 9 km the run recorded", p.Value)
	}
	if _, ok := p.States["traditional_strength_training"]; ok {
		t.Errorf("a session with no distance got a segment: %v", p.States)
	}
	if p.Count != 1 {
		t.Errorf("count = %d, want only the session that recorded a distance", p.Count)
	}

	// The same strength session still counts every one of its minutes.
	elapsed := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", timeaxis.Day)
	if elapsed.Points[0].Value != 105 {
		t.Errorf("minutes = %v, want 105 with the strength session counted", elapsed.Points[0].Value)
	}
}

// A week bucket sums its days, computed from the day label, so a Sunday and the
// Monday after it land in different weeks.
func TestTrainingFoldsToWeeksByDayLabel(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-01-07T09:00:00Z", minutes: 60}, // Sunday
		{activity: "running", start: "2024-01-08T09:00:00Z", minutes: 30}, // Monday
	})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-15T00:00:00Z", timeaxis.Week)
	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want two weeks", s.Points)
	}
	if s.Points[0].Bucket != "2024-01-01" || s.Points[0].Value != 60 {
		t.Errorf("week one = %s %v, want the Sunday alone", s.Points[0].Bucket, s.Points[0].Value)
	}
	if s.Points[1].Bucket != "2024-01-08" || s.Points[1].Value != 30 {
		t.Errorf("week two = %s %v, want the Monday alone", s.Points[1].Bucket, s.Points[1].Value)
	}
}

// The cap keeps the five largest Activities over the window and folds the rest
// into `other`, without changing any total: the cap is how the bar is drawn, never
// what it says.
func TestTrainingCapsTheBreakdownWithoutChangingTheTotal(t *testing.T) {
	e, models, acc := setup(t)
	rows := []workout{
		{activity: "running", start: "2024-01-01T06:00:00Z", minutes: 100},
		{activity: "cycling", start: "2024-01-01T07:00:00Z", minutes: 90},
		{activity: "swimming", start: "2024-01-01T08:00:00Z", minutes: 80},
		{activity: "rowing", start: "2024-01-01T09:00:00Z", minutes: 70},
		{activity: "hiking", start: "2024-01-01T10:00:00Z", minutes: 60},
		{activity: "yoga", start: "2024-01-01T11:00:00Z", minutes: 50},
		{activity: "pilates", start: "2024-01-02T11:00:00Z", minutes: 40},
	}
	seedWorkouts(t, models, acc, rows)

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z", timeaxis.Day)

	var total float64
	segments := map[string]bool{}
	for _, p := range s.Points {
		total += p.Value
		for key := range p.States {
			segments[key] = true
		}
	}
	if total != 490 {
		t.Errorf("total = %v, want every minute including the capped-away ones", total)
	}
	if len(segments) != 6 {
		t.Fatalf("segments = %v, want five activities plus other", sortedKeys(segments))
	}
	if !segments["other"] {
		t.Errorf("segments = %v, want the folded activities under other", sortedKeys(segments))
	}
	for _, capped := range []string{"yoga", "pilates"} {
		if segments[capped] {
			t.Errorf("%q kept its own segment past the cap: %v", capped, sortedKeys(segments))
		}
	}
	// The two smallest fold together, on the days they happened.
	if s.Points[0].States["other"] != 50 || s.Points[1].States["other"] != 40 {
		t.Errorf("other = %v then %v, want 50 then 40", s.Points[0].States["other"], s.Points[1].States["other"])
	}
	// And the same Activity keeps its segment on every bucket: the cap is decided
	// once over the window, not per bar.
	if s.Points[1].States["pilates"] != 0 {
		t.Errorf("day two named an activity the window capped away: %v", s.Points[1].States)
	}
}

// A workout that runs through midnight counts in the day it began, which is the
// day its owner would name. No noon shift: that rule belongs to a Night.
func TestTrainingCountsAWorkoutInTheDayItStarted(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-01-01T23:30:00Z", minutes: 60},
	})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z", timeaxis.Day)
	if len(s.Points) != 1 || s.Points[0].Bucket != "2024-01-01" {
		t.Fatalf("points = %+v, want the workout on the day it started", s.Points)
	}
}

// The window is half-open, like every other read.
func TestTrainingWindowIsHalfOpen(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-01-01T00:00:00Z", minutes: 30},
		{activity: "running", start: "2024-01-03T00:00:00Z", minutes: 30},
	})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-03T00:00:00Z", timeaxis.Day)
	if len(s.Points) != 1 || s.Points[0].Bucket != "2024-01-01" {
		t.Fatalf("points = %+v, want the workout at From and not the one at To", s.Points)
	}
}

// An empty window is a gap, never a zero (ADR 0014).
func TestTrainingEmptyWindowHasNoSummary(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-02-01T06:00:00Z", minutes: 45},
	})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", timeaxis.Day)
	if len(s.Points) != 0 {
		t.Fatalf("points = %+v, want none", s.Points)
	}
	if s.Summary != nil {
		t.Errorf("summary = %+v, want nil for a window with no workouts", s.Summary)
	}
	if s.Source != "" {
		t.Errorf("source = %q, want none reported for an empty window", s.Source)
	}
}

// Every Source behind the figure is named. Nothing elects between them yet, and
// reporting what was read is what will make a doubled volume visible the day a
// second Connector emits workouts (ADR 0040).
func TestTrainingReportsTheSourcesItRead(t *testing.T) {
	e, models, acc := setup(t)
	seedWorkouts(t, models, acc, []workout{
		{activity: "running", start: "2024-01-01T06:00:00Z", minutes: 45, source: "Apple Watch"},
		{activity: "cycling", start: "2024-01-02T06:00:00Z", minutes: 45, source: "Garmin"},
	})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-08T00:00:00Z", timeaxis.Day)
	if s.Source == "" {
		t.Fatal("the series named no Source")
	}
	if s.Points[0].Value+s.Points[1].Value != 90 {
		t.Errorf("total = %v, want both workouts counted while nothing elects between Sources",
			s.Points[0].Value+s.Points[1].Value)
	}
}

func TestTrainingIsScopedToOneAccount(t *testing.T) {
	e, models, acc := setup(t)
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	seedWorkouts(t, models, acc, []workout{{activity: "running", start: "2024-01-01T06:00:00Z", minutes: 45}})
	seedWorkouts(t, models, other.ID, []workout{{activity: "cycling", start: "2024-01-01T06:00:00Z", minutes: 999}})

	s := trainingSeries(t, e, acc, "training_time", "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", timeaxis.Day)
	if len(s.Points) != 1 || s.Points[0].Value != 45 {
		t.Fatalf("points = %+v, want only this Account's workout", s.Points)
	}
}
