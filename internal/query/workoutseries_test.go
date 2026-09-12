package query

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// seedReadings inserts one Metric's readings at the given instants.
func seedReadings(t *testing.T, models data.Models, acc int64, metric, source string, at map[string]float64) {
	t.Helper()
	batch := make([]data.Measurement, 0, len(at))
	i := 0
	for stamp, value := range at {
		batch = append(batch, data.Measurement{
			AccountID: acc, Metric: metric, Value: value, OriginalUnit: "count/min",
			StartAt: stamp, EndAt: stamp, Source: source,
			ContentKey: fmt.Sprintf("r-%s-%s-%d", metric, source, i),
		})
		i++
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), batch); err != nil {
		t.Fatalf("seed readings: %v", err)
	}
}

func workoutCurve(t *testing.T, e Engine, acc int64, metric, from, to string) WorkoutSeries {
	t.Helper()
	s, err := e.WorkoutSeriesFor(context.Background(), WorkoutRequest{
		AccountID: acc, Metric: metric, From: mustTime(t, from), To: mustTime(t, to),
	})
	if err != nil {
		t.Fatalf("WorkoutSeriesFor: %v", err)
	}
	return s
}

// The readings inside the window, bucketed and averaged, with the count behind
// each bucket.
func TestWorkoutSeriesBucketsAndAverages(t *testing.T) {
	e, models, acc := setup(t)
	// A one-hour workout: 300 buckets of 12 seconds each.
	seedReadings(t, models, acc, "heart_rate", "Apple Watch", map[string]float64{
		"2024-01-01T06:00:00Z": 120,
		"2024-01-01T06:00:06Z": 130, // same 12-second bucket as the first
		"2024-01-01T06:30:00Z": 150,
	})

	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T07:00:00Z")

	if s.Unit != "count/min" || s.Source != "Apple Watch" {
		t.Fatalf("series = %+v, want the Metric's unit and the elected Source", s)
	}
	if s.StartAt != "2024-01-01T06:00:00Z" || s.EndAt != "2024-01-01T07:00:00Z" {
		t.Errorf("window = %s..%s, want the workout's own", s.StartAt, s.EndAt)
	}
	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want two buckets holding readings", s.Points)
	}
	if s.Points[0].Value != 125 || s.Points[0].Count != 2 {
		t.Errorf("first bucket = %+v, want the mean of 120 and 130", s.Points[0])
	}
	if s.Points[1].Value != 150 || s.Points[1].Count != 1 {
		t.Errorf("second bucket = %+v, want the lone 150", s.Points[1])
	}
}

// A minute with no reading is an absent Point: a strap that dropped out did not
// record a zero (ADR 0014).
func TestWorkoutSeriesLeavesGapsOut(t *testing.T) {
	e, models, acc := setup(t)
	seedReadings(t, models, acc, "heart_rate", "Apple Watch", map[string]float64{
		"2024-01-01T06:00:00Z": 120,
		"2024-01-01T06:50:00Z": 140,
	})

	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T07:00:00Z")
	if len(s.Points) != 2 {
		t.Fatalf("points = %+v, want only the two buckets with readings", s.Points)
	}
	for _, p := range s.Points {
		if p.Count == 0 {
			t.Errorf("an empty bucket was emitted: %+v", p)
		}
	}
}

// However long the workout, the curve is bounded, and no parameter widens it
// (ADR 0041).
func TestWorkoutSeriesIsBounded(t *testing.T) {
	e, models, acc := setup(t)
	start := mustTime(t, "2024-01-01T06:00:00Z")
	readings := map[string]float64{}
	for i := 0; i < 2000; i++ {
		readings[start.Add(time.Duration(i)*10*time.Second).Format(time.RFC3339)] = float64(100 + i%40)
	}
	seedReadings(t, models, acc, "heart_rate", "Apple Watch", readings)

	// Five and a half hours at one reading per ten seconds.
	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T11:34:00Z")
	if len(s.Points) > workoutBuckets {
		t.Fatalf("points = %d, want at most %d", len(s.Points), workoutBuckets)
	}
	if len(s.Points) < workoutBuckets/2 {
		t.Fatalf("points = %d, want the curve to use its buckets rather than collapse", len(s.Points))
	}
}

// The window is half-open, like every other read.
func TestWorkoutSeriesWindowIsHalfOpen(t *testing.T) {
	e, models, acc := setup(t)
	seedReadings(t, models, acc, "heart_rate", "Apple Watch", map[string]float64{
		"2024-01-01T05:59:59Z": 100,
		"2024-01-01T06:00:00Z": 120,
		"2024-01-01T07:00:00Z": 180,
	})

	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T07:00:00Z")
	if len(s.Points) != 1 || s.Points[0].Value != 120 {
		t.Fatalf("points = %+v, want the reading at From and neither bound outside", s.Points)
	}
}

// One Source wins the whole workout: switching mid-ride would draw a
// discontinuity that is an artefact of the read.
func TestWorkoutSeriesElectsOneSource(t *testing.T) {
	e, models, acc := setup(t)
	seedReadings(t, models, acc, "heart_rate", "Apple Watch", map[string]float64{
		"2024-01-01T06:00:00Z": 120,
	})
	seedReadings(t, models, acc, "heart_rate", "iPhone", map[string]float64{
		"2024-01-01T06:30:00Z": 60,
	})

	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T07:00:00Z")
	if s.Source != "Apple Watch" {
		t.Fatalf("source = %q, want the priority table's winner", s.Source)
	}
	for _, p := range s.Points {
		if p.Value == 60 {
			t.Errorf("the losing Source's reading was averaged in: %+v", p)
		}
	}
}

// A workout with no readings of that Metric is an empty curve, not an error:
// the workout exists and the curve does not.
func TestWorkoutSeriesWithoutReadings(t *testing.T) {
	e, _, acc := setup(t)
	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T07:00:00Z")
	if len(s.Points) != 0 || s.Source != "" {
		t.Fatalf("series = %+v, want an empty curve with no Source", s)
	}
}

func TestWorkoutSeriesRefusesMetricsWithNoCurve(t *testing.T) {
	e, _, acc := setup(t)
	tests := map[string]struct {
		metric string
		want   error
	}{
		"a latest Metric":   {"body_mass", ErrUnsupportedInWorkout},
		"a by-state Metric": {"sleep", ErrUnsupportedInWorkout},
		"a training Metric": {"training_time", ErrUnsupportedInWorkout},
		"a derived Metric":  {"calorie_balance", ErrUnsupportedInWorkout},
		"an unknown Metric": {"not_a_metric", ErrUnknownMetric},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := e.WorkoutSeriesFor(context.Background(), WorkoutRequest{
				AccountID: acc, Metric: tc.metric,
				From: mustTime(t, "2024-01-01T06:00:00Z"), To: mustTime(t, "2024-01-01T07:00:00Z"),
			})
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWorkoutSeriesIsScopedToOneAccount(t *testing.T) {
	e, models, acc := setup(t)
	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(context.Background(), other); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	seedReadings(t, models, other.ID, "heart_rate", "Apple Watch", map[string]float64{
		"2024-01-01T06:00:00Z": 120,
	})

	s := workoutCurve(t, e, acc, "heart_rate", "2024-01-01T06:00:00Z", "2024-01-01T07:00:00Z")
	if len(s.Points) != 0 {
		t.Fatalf("points = %+v, want another Account's readings to be invisible", s.Points)
	}
}
