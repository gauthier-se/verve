package ingest_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// importedAt is when the import of the records of the tests was created.
var importedAt = time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC)

// watch is the source of the records of the tests.
var watch = store.SourceInfo{
	Name:            "Apple Watch",
	DeviceModel:     "Watch",
	BundleID:        "com.apple.health.81C9A0F2-6E43-4A6B-9D8E-2B7C5F1A0E93",
	DeviceName:      "Apple Watch",
	Manufacturer:    "Apple Inc.",
	HardwareVersion: "Watch7,1",
	SoftwareVersion: "12.1",
}

// heartRate is the curated heart rate type.
var heartRate = store.MetricType{
	Key:        "heart_rate",
	Identifier: "HKQuantityTypeIdentifierHeartRate",
	Unit:       "count/min",
	Curated:    true,
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T {
	return &v
}

// utc returns the time of an RFC 3339 string, in UTC.
func utc(t *testing.T, s string) time.Time {
	t.Helper()

	at, err := time.Parse(time.RFC3339Nano, s)
	require.NoError(t, err)

	return at.UTC()
}

// quantitySample returns a heart rate sample of the watch, as an adapter produces it.
func quantitySample() ingest.QuantitySample {
	return ingest.QuantitySample{
		UUID:     "6F1C2B9E-3D4A-4E0B-9C59-1A2B3C4D5E6F",
		Type:     "HKQuantityTypeIdentifierHeartRate",
		Start:    "2026-11-10T08:00:00+01:00",
		End:      "2026-11-10T08:00:00+01:00",
		Value:    62,
		Unit:     "count/min",
		Source:   watch,
		Metadata: json.RawMessage(`{"HKMetadataKeyHeartRateMotionContext":1}`),
	}
}

// categorySample returns a sleep sample of the watch, as an adapter produces it.
func categorySample() ingest.CategorySample {
	return ingest.CategorySample{
		UUID:   "C3A9F5D7-1E2B-4C8A-B6D4-5F7E9A1C3B2D",
		Type:   "HKCategoryTypeIdentifierSleepAnalysis",
		Start:  "2026-11-10T01:12:00+01:00",
		End:    "2026-11-10T02:03:30.250+01:00",
		Value:  "HKCategoryValueSleepAnalysisAsleepCore",
		Source: watch,
	}
}

// workout returns a run of the watch, as an adapter produces it, with a pause of one minute.
func workout() ingest.Workout {
	return ingest.Workout{
		UUID:         "9E7D5C3B-1A2F-4E6D-8C0B-2F4A6E8C0D1B",
		ActivityType: "HKWorkoutActivityTypeRunning",
		Start:        "2026-11-10T18:00:00+01:00",
		End:          "2026-11-10T18:31:00+01:00",
		Duration:     &ingest.Quantity{Value: 30, Unit: "min"},
		Totals: ingest.WorkoutTotals{
			Distance:           &ingest.Quantity{Value: 5.21, Unit: "km"},
			ActiveEnergy:       &ingest.Quantity{Value: 1514.608, Unit: "kJ"},
			TotalEnergy:        &ingest.Quantity{Value: 401, Unit: "kcal"},
			ElevationAscended:  &ingest.Quantity{Value: 100, Unit: "ft"},
			ElevationDescended: &ingest.Quantity{Value: 30, Unit: "m"},
			AvgHeartRate:       &ingest.Quantity{Value: 2.5, Unit: "count/s"},
			MaxHeartRate:       &ingest.Quantity{Value: 178, Unit: "count/min"},
		},
		Indoor:   ptr(false),
		Source:   watch,
		Metadata: json.RawMessage(`{"HKAverageMETs":"9.8 kcal/hr·kg"}`),
		Events: []ingest.WorkoutEvent{
			{Type: "HKWorkoutEventTypePause", Start: "2026-11-10T18:12:00+01:00", End: "2026-11-10T18:12:00+01:00"},
			{Type: "HKWorkoutEventTypeResume", Start: "2026-11-10T18:13:00+01:00", End: "2026-11-10T18:13:00+01:00"},
		},
		Route: []ingest.RoutePoint{
			{
				Time: "2026-11-10T18:00:01+01:00", Latitude: 48.8566, Longitude: 2.3522,
				Altitude: ptr(35.2), Speed: ptr(2.9), Course: ptr(87.5),
				HorizontalAccuracy: ptr(4.1), VerticalAccuracy: ptr(3.2),
			},
			{
				Time: "2026-11-10T18:00:02+01:00", Latitude: 48.85662, Longitude: 2.35224,
				Altitude: ptr(35.3), Speed: ptr(3.1), Course: ptr(88.0),
				HorizontalAccuracy: ptr(4.0), VerticalAccuracy: ptr(3.1),
			},
		},
	}
}

// Given a quantity sample as an adapter produces it, with its identifier, unit and times as
// received, when NormalizeQuantity runs, then its times are in UTC with their offset, and its
// value, min and max in the unit of its type.
func TestNormalizeQuantity_SampleAsReceived_ReturnsItsTimesAndValueInTheUnitOfItsType(t *testing.T) {
	t.Parallel()

	s := quantitySample()
	s.Start, s.End = "2026-11-10T08:00:00+01:00", "2026-11-10T08:10:00+01:00"
	s.Value, s.Min, s.Max = 1.25, ptr(1.0), ptr(2.0)
	s.Unit = "count/s"

	got, err := ingest.NormalizeQuantity(s, heartRate, importedAt)
	require.NoError(t, err)
	require.Equal(t, ingest.NormalizedSample{
		Times: ingest.Times{
			Start:     time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC),
			End:       time.Date(2026, 11, 10, 7, 10, 0, 0, time.UTC),
			UTCOffset: 3600,
		},
		Value: 75,
		Min:   ptr(60.0),
		Max:   ptr(120.0),
	}, got)
}

// Given a quantity sample whose unit the table does not list, or of another dimension, when
// NormalizeQuantity runs, then the record is invalid, with a reason naming the unit.
func TestNormalizeQuantity_UnitNotOfItsType_IsInvalid(t *testing.T) {
	t.Parallel()

	s := quantitySample()
	s.Unit = "bpm"

	_, err := ingest.NormalizeQuantity(s, heartRate, importedAt)
	require.EqualError(t, err, `unit "bpm" is not a unit of count/min`)
}

// Given a start and an end in RFC 3339 with an offset, when a record is normalized, then they are
// parsed to UTC with millisecond precision and the original offset in seconds.
func TestNormalize_TimesWithOffset_ParsedToUTCWithTheirOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start string
		end   string
		want  ingest.Times
	}{
		{
			name:  "positive offset",
			start: "2026-11-10T08:00:00+01:00",
			end:   "2026-11-10T08:10:00+01:00",
			want: ingest.Times{
				Start:     time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC),
				End:       time.Date(2026, 11, 10, 7, 10, 0, 0, time.UTC),
				UTCOffset: 3600,
			},
		},
		{
			name:  "Z",
			start: "2026-11-10T07:00:00Z",
			end:   "2026-11-10T07:00:00Z",
			want: ingest.Times{
				Start: time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC),
				End:   time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC),
			},
		},
		{
			name:  "negative offset of half an hour",
			start: "2026-11-10T03:30:00-03:30",
			end:   "2026-11-10T03:45:00-03:30",
			want: ingest.Times{
				Start:     time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC),
				End:       time.Date(2026, 11, 10, 7, 15, 0, 0, time.UTC),
				UTCOffset: -12600,
			},
		},
		{
			name:  "fractional seconds truncated to milliseconds",
			start: "2026-11-10T08:00:00.123456789+01:00",
			end:   "2026-11-10T08:00:01.5+01:00",
			want: ingest.Times{
				Start:     time.Date(2026, 11, 10, 7, 0, 0, 123_000_000, time.UTC),
				End:       time.Date(2026, 11, 10, 7, 0, 1, 500_000_000, time.UTC),
				UTCOffset: 3600,
			},
		},
		{
			name:  "offset of the start",
			start: "2026-10-25T02:30:00+02:00",
			end:   "2026-10-25T02:10:00+01:00",
			want: ingest.Times{
				Start:     time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC),
				End:       time.Date(2026, 10, 25, 1, 10, 0, 0, time.UTC),
				UTCOffset: 7200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := quantitySample()
			s.Start, s.End = tt.start, tt.end

			got, err := ingest.NormalizeQuantity(s, heartRate, importedAt)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Times)

			c := categorySample()
			c.Start, c.End = tt.start, tt.end

			times, err := ingest.NormalizeCategory(c, importedAt)
			require.NoError(t, err)
			require.Equal(t, tt.want, times)
		})
	}
}

// Given a time without offset, an unparsable time, or an end before its start, when a record is
// normalized, then the record is invalid.
func TestNormalize_BrokenTimes_IsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start string
		end   string
		want  string
	}{
		{name: "start without offset", start: "2026-11-10T08:00:00", end: "2026-11-10T08:00:00+01:00", want: "start is not an RFC 3339 time with an offset"},
		{name: "end without offset", start: "2026-11-10T08:00:00+01:00", end: "2026-11-10T08:00:00", want: "end is not an RFC 3339 time with an offset"},
		{name: "offset without colon", start: "2026-11-10T08:00:00+0100", end: "2026-11-10T08:00:00+01:00", want: "start is not an RFC 3339 time with an offset"},
		{name: "unparsable start", start: "yesterday", end: "2026-11-10T08:00:00+01:00", want: "start is not an RFC 3339 time with an offset"},
		{name: "missing end", start: "2026-11-10T08:00:00+01:00", end: "", want: "end is not an RFC 3339 time with an offset"},
		{name: "end before start", start: "2026-11-10T08:00:00+01:00", end: "2026-11-10T07:59:59.999+01:00", want: "end is before start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := quantitySample()
			s.Start, s.End = tt.start, tt.end

			_, err := ingest.NormalizeQuantity(s, heartRate, importedAt)
			require.EqualError(t, err, tt.want)

			c := categorySample()
			c.Start, c.End = tt.start, tt.end

			_, err = ingest.NormalizeCategory(c, importedAt)
			require.EqualError(t, err, tt.want)

			w := workout()
			w.Start, w.End = tt.start, tt.end
			w.Events, w.Route = nil, nil

			_, err = ingest.NormalizeWorkout(w, importedAt)
			require.EqualError(t, err, tt.want)
		})
	}
}

// Given a record that starts before 2000 or more than one day after the import time, when it is
// normalized, then it is valid and flagged suspicious.
func TestNormalize_StartBefore2000OrAfterTheNextDay_IsSuspicious(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start string
		want  bool
	}{
		{name: "before 2000", start: "1999-12-31T23:59:59.999Z", want: true},
		{name: "start of 2000 in UTC", start: "2000-01-01T00:00:00Z", want: false},
		{name: "start of 2000 in local time only", start: "2000-01-01T00:30:00+01:00", want: true},
		{name: "during the import", start: "2026-11-10T20:00:00Z", want: false},
		{name: "one day after the import", start: "2026-11-11T20:00:00Z", want: false},
		{name: "more than one day after the import", start: "2026-11-11T20:00:00.001Z", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := quantitySample()
			s.Start, s.End = tt.start, tt.start

			got, err := ingest.NormalizeQuantity(s, heartRate, importedAt)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Suspicious)

			c := categorySample()
			c.Start, c.End = tt.start, tt.start

			times, err := ingest.NormalizeCategory(c, importedAt)
			require.NoError(t, err)
			require.Equal(t, tt.want, times.Suspicious)

			w := workout()
			w.Start, w.End = tt.start, tt.start
			w.Duration = &ingest.Quantity{Value: 0, Unit: "s"}
			w.Events, w.Route = nil, nil

			normalized, err := ingest.NormalizeWorkout(w, importedAt)
			require.NoError(t, err)
			require.Equal(t, tt.want, normalized.Suspicious)
		})
	}
}

// Given a workout, when NormalizeWorkout runs, then its totals are converted, distance and
// elevation to m, energy to kcal, heart rate to count/min, and its times, events and route
// points parsed.
func TestNormalizeWorkout_Workout_ConvertsItsTotalsAndParsesItsTimes(t *testing.T) {
	t.Parallel()

	got, err := ingest.NormalizeWorkout(workout(), importedAt)
	require.NoError(t, err)

	require.Equal(t, ingest.Times{
		Start:     utc(t, "2026-11-10T17:00:00Z"),
		End:       utc(t, "2026-11-10T17:31:00Z"),
		UTCOffset: 3600,
	}, got.Times)
	require.InEpsilon(t, 1800, got.Duration, 1e-15)
	require.InEpsilon(t, 5210, *got.Distance, 1e-15)
	require.InEpsilon(t, 362, *got.ActiveEnergy, 1e-15)
	require.InEpsilon(t, 401, *got.TotalEnergy, 1e-15)
	require.InEpsilon(t, 30.48, *got.ElevationGain, 1e-15)
	require.InEpsilon(t, 30, *got.ElevationLoss, 1e-15)
	require.InEpsilon(t, 150, *got.AvgHeartRate, 1e-15)
	require.InEpsilon(t, 178, *got.MaxHeartRate, 1e-15)
	require.Equal(t, []store.WorkoutEvent{
		{Type: store.WorkoutEventPause, Start: utc(t, "2026-11-10T17:12:00Z"), End: utc(t, "2026-11-10T17:12:00Z")},
		{Type: store.WorkoutEventResume, Start: utc(t, "2026-11-10T17:13:00Z"), End: utc(t, "2026-11-10T17:13:00Z")},
	}, got.Events)
	require.Equal(t, []route.Point{
		{
			Time: utc(t, "2026-11-10T17:00:01Z"), Lat: 48.8566, Lon: 2.3522,
			Altitude: ptr(35.2), Speed: ptr(2.9), Course: ptr(87.5),
			HorizontalAccuracy: ptr(4.1), VerticalAccuracy: ptr(3.2),
		},
		{
			Time: utc(t, "2026-11-10T17:00:02Z"), Lat: 48.85662, Lon: 2.35224,
			Altitude: ptr(35.3), Speed: ptr(3.1), Course: ptr(88.0),
			HorizontalAccuracy: ptr(4.0), VerticalAccuracy: ptr(3.1),
		},
	}, got.Route)
	require.False(t, got.Suspicious)
	require.Empty(t, got.Warnings)
}

// Given a workout that lacks its totals, when NormalizeWorkout runs, then they stay unknown.
func TestNormalizeWorkout_MissingTotals_StayUnknown(t *testing.T) {
	t.Parallel()

	w := workout()
	w.Totals = ingest.WorkoutTotals{}
	w.Events, w.Route = nil, nil

	got, err := ingest.NormalizeWorkout(w, importedAt)
	require.NoError(t, err)
	require.Nil(t, got.Distance)
	require.Nil(t, got.ActiveEnergy)
	require.Nil(t, got.TotalEnergy)
	require.Nil(t, got.ElevationGain)
	require.Nil(t, got.ElevationLoss)
	require.Nil(t, got.AvgHeartRate)
	require.Nil(t, got.MaxHeartRate)
	require.Nil(t, got.Events)
	require.Nil(t, got.Route)
}

// Given a workout whose total or duration has a unit of another dimension, when NormalizeWorkout
// runs, then the record is invalid, with a reason naming the total and the unit.
func TestNormalizeWorkout_TotalOfAnotherUnit_IsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(w *ingest.Workout)
		want   string
	}{
		{name: "duration", change: func(w *ingest.Workout) { w.Duration.Unit = "m" }, want: `duration: unit "m" is not a unit of s`},
		{name: "negative duration", change: func(w *ingest.Workout) { w.Duration.Value = -1 }, want: "duration is negative"},
		{name: "distance", change: func(w *ingest.Workout) { w.Totals.Distance.Unit = "kcal" }, want: `distance: unit "kcal" is not a unit of m`},
		{name: "active energy", change: func(w *ingest.Workout) { w.Totals.ActiveEnergy.Unit = "km" }, want: `active energy: unit "km" is not a unit of kcal`},
		{name: "total energy", change: func(w *ingest.Workout) { w.Totals.TotalEnergy.Unit = "kg" }, want: `total energy: unit "kg" is not a unit of kcal`},
		{name: "elevation ascended", change: func(w *ingest.Workout) { w.Totals.ElevationAscended.Unit = "s" }, want: `elevation ascended: unit "s" is not a unit of m`},
		{name: "elevation descended", change: func(w *ingest.Workout) { w.Totals.ElevationDescended.Unit = "" }, want: `elevation descended: unit "" is not a unit of m`},
		{name: "average heart rate", change: func(w *ingest.Workout) { w.Totals.AvgHeartRate.Unit = "bpm" }, want: `average heart rate: unit "bpm" is not a unit of count/min`},
		{name: "max heart rate", change: func(w *ingest.Workout) { w.Totals.MaxHeartRate.Unit = "count" }, want: `max heart rate: unit "count" is not a unit of count/min`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workout()
			tt.change(&w)

			_, err := ingest.NormalizeWorkout(w, importedAt)
			require.EqualError(t, err, tt.want)
		})
	}
}

// Given a workout without duration, when NormalizeWorkout runs, then its duration is its time
// range without its pauses, from each pause, or motion pause, to the next resume, or to its end.
func TestNormalizeWorkout_WithoutDuration_ExcludesItsPauses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		events []ingest.WorkoutEvent
		want   float64
	}{
		{name: "no events", want: 31 * 60},
		{
			name: "pause and resume",
			events: []ingest.WorkoutEvent{
				{Type: "HKWorkoutEventTypePause", Start: "2026-11-10T18:12:00+01:00", End: "2026-11-10T18:12:00+01:00"},
				{Type: "HKWorkoutEventTypeResume", Start: "2026-11-10T18:13:00+01:00", End: "2026-11-10T18:13:00+01:00"},
			},
			want: 30 * 60,
		},
		{
			name: "motion pause and resume, laps aside",
			events: []ingest.WorkoutEvent{
				{Type: "HKWorkoutEventTypeLap", Start: "2026-11-10T18:05:00+01:00", End: "2026-11-10T18:10:00+01:00"},
				{Type: "HKWorkoutEventTypeMotionPaused", Start: "2026-11-10T18:20:00+01:00", End: "2026-11-10T18:20:00+01:00"},
				{Type: "HKWorkoutEventTypeMotionResumed", Start: "2026-11-10T18:20:30+01:00", End: "2026-11-10T18:20:30+01:00"},
			},
			want: 30*60 + 30,
		},
		{
			name: "pause until the end",
			events: []ingest.WorkoutEvent{
				{Type: "HKWorkoutEventTypePause", Start: "2026-11-10T18:30:00+01:00", End: "2026-11-10T18:30:00+01:00"},
			},
			want: 30 * 60,
		},
		{
			name: "repeated pause counted from the first",
			events: []ingest.WorkoutEvent{
				{Type: "HKWorkoutEventTypePause", Start: "2026-11-10T18:10:00+01:00", End: "2026-11-10T18:10:00+01:00"},
				{Type: "HKWorkoutEventTypeMotionPaused", Start: "2026-11-10T18:10:30+01:00", End: "2026-11-10T18:10:30+01:00"},
				{Type: "HKWorkoutEventTypeResume", Start: "2026-11-10T18:11:00+01:00", End: "2026-11-10T18:11:00+01:00"},
			},
			want: 30 * 60,
		},
		{
			name: "events out of order",
			events: []ingest.WorkoutEvent{
				{Type: "HKWorkoutEventTypeResume", Start: "2026-11-10T18:13:00+01:00", End: "2026-11-10T18:13:00+01:00"},
				{Type: "HKWorkoutEventTypePause", Start: "2026-11-10T18:12:00+01:00", End: "2026-11-10T18:12:00+01:00"},
			},
			want: 30 * 60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workout()
			w.Duration = nil
			w.Events = tt.events

			got, err := ingest.NormalizeWorkout(w, importedAt)
			require.NoError(t, err)
			require.InEpsilon(t, tt.want, got.Duration, 1e-15)
		})
	}
}

// Given a workout with a received duration, when NormalizeWorkout runs, then its duration is the
// one received, which HealthKit computes without the pauses.
func TestNormalizeWorkout_WithDuration_KeepsTheReceivedOne(t *testing.T) {
	t.Parallel()

	w := workout()
	w.Duration = &ingest.Quantity{Value: 1795.5, Unit: "s"}

	got, err := ingest.NormalizeWorkout(w, importedAt)
	require.NoError(t, err)
	require.InEpsilon(t, 1795.5, got.Duration, 1e-15)
}

// Given a workout whose event has a broken time, when NormalizeWorkout runs, then the record is
// invalid, with a reason naming the event.
func TestNormalizeWorkout_BrokenEvent_IsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event ingest.WorkoutEvent
		want  string
	}{
		{name: "start without offset", event: ingest.WorkoutEvent{Type: "HKWorkoutEventTypeLap", Start: "2026-11-10T18:05:00", End: "2026-11-10T18:10:00+01:00"}, want: "event 1: start is not an RFC 3339 time with an offset"},
		{name: "unparsable end", event: ingest.WorkoutEvent{Type: "HKWorkoutEventTypeLap", Start: "2026-11-10T18:05:00+01:00", End: "later"}, want: "event 1: end is not an RFC 3339 time with an offset"},
		{name: "end before start", event: ingest.WorkoutEvent{Type: "HKWorkoutEventTypeLap", Start: "2026-11-10T18:05:00+01:00", End: "2026-11-10T18:04:00+01:00"}, want: "event 1: end is before start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workout()
			w.Events = []ingest.WorkoutEvent{w.Events[0], tt.event}

			_, err := ingest.NormalizeWorkout(w, importedAt)
			require.EqualError(t, err, tt.want)
		})
	}
}

// Given a route point out of the latitudes [-90, 90] or longitudes [-180, 180], or whose time is
// broken or outside the workout, when NormalizeWorkout runs, then the record is invalid, with a
// reason naming the point.
func TestNormalizeWorkout_BrokenRoutePoint_IsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(p *ingest.RoutePoint)
		want   string
	}{
		{name: "latitude above 90", change: func(p *ingest.RoutePoint) { p.Latitude = 90.0001 }, want: "route point 1: latitude is out of [-90, 90]"},
		{name: "latitude below -90", change: func(p *ingest.RoutePoint) { p.Latitude = -90.0001 }, want: "route point 1: latitude is out of [-90, 90]"},
		{name: "longitude above 180", change: func(p *ingest.RoutePoint) { p.Longitude = 180.0001 }, want: "route point 1: longitude is out of [-180, 180]"},
		{name: "longitude below -180", change: func(p *ingest.RoutePoint) { p.Longitude = -180.0001 }, want: "route point 1: longitude is out of [-180, 180]"},
		{name: "time without offset", change: func(p *ingest.RoutePoint) { p.Time = "2026-11-10T18:00:02" }, want: "route point 1: time is not an RFC 3339 time with an offset"},
		{name: "time before the workout", change: func(p *ingest.RoutePoint) { p.Time = "2026-11-10T17:59:59.999+01:00" }, want: "route point 1: time is outside the workout"},
		{name: "time after the workout", change: func(p *ingest.RoutePoint) { p.Time = "2026-11-10T18:31:00.001+01:00" }, want: "route point 1: time is outside the workout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workout()
			tt.change(&w.Route[1])

			_, err := ingest.NormalizeWorkout(w, importedAt)
			require.EqualError(t, err, tt.want)
		})
	}
}

// Given route points at the bounds of the latitudes, longitudes and time of the workout, when
// NormalizeWorkout runs, then they are valid.
func TestNormalizeWorkout_RoutePointsAtTheBounds_AreValid(t *testing.T) {
	t.Parallel()

	w := workout()
	w.Route = []ingest.RoutePoint{
		{Time: "2026-11-10T18:00:00+01:00", Latitude: -90, Longitude: -180},
		{Time: "2026-11-10T17:31:00Z", Latitude: 90, Longitude: 180},
	}

	got, err := ingest.NormalizeWorkout(w, importedAt)
	require.NoError(t, err)
	require.Equal(t, []route.Point{
		{Time: utc(t, "2026-11-10T17:00:00Z"), Lat: -90, Lon: -180},
		{Time: utc(t, "2026-11-10T17:31:00Z"), Lat: 90, Lon: 180},
	}, got.Route)
}

// Given a route point with a negative speed, course or accuracy, which CoreLocation writes when the
// value is unknown, when NormalizeWorkout runs, then the value counts as left out.
func TestNormalizeWorkout_NegativeRouteValue_CountsAsLeftOut(t *testing.T) {
	t.Parallel()

	w := workout()
	w.Route = []ingest.RoutePoint{{
		Time: "2026-11-10T18:00:01+01:00", Latitude: 48.8566, Longitude: 2.3522,
		Altitude: ptr(-12.5), Speed: ptr(-1.0), Course: ptr(-1.0),
		HorizontalAccuracy: ptr(-1.0), VerticalAccuracy: ptr(-1.0),
	}}

	got, err := ingest.NormalizeWorkout(w, importedAt)
	require.NoError(t, err)
	require.Equal(t, []route.Point{{
		Time: utc(t, "2026-11-10T17:00:01Z"), Lat: 48.8566, Lon: 2.3522, Altitude: ptr(-12.5),
	}}, got.Route)
}

// Given workout events, when NormalizeWorkout runs, then their types are stored as FR-7 of
// Ingestion pipeline maps them, a pause or resume request is ignored, and another type is ignored
// with a warning naming the event.
func TestNormalizeWorkout_EventTypes_MappedAsFR7(t *testing.T) {
	t.Parallel()

	at := "2026-11-10T18:10:00+01:00"
	w := workout()
	w.Duration = &ingest.Quantity{Value: 30, Unit: "min"}
	w.Events = []ingest.WorkoutEvent{
		{Type: "HKWorkoutEventTypePause", Start: at, End: at},
		{Type: "HKWorkoutEventTypeResume", Start: at, End: at},
		{Type: "HKWorkoutEventTypeLap", Start: at, End: at},
		{Type: "HKWorkoutEventTypeSegment", Start: at, End: at},
		{Type: "HKWorkoutEventTypeMarker", Start: at, End: at},
		{Type: "HKWorkoutEventTypeMotionPaused", Start: at, End: at},
		{Type: "HKWorkoutEventTypeMotionResumed", Start: at, End: at},
		{Type: "HKWorkoutEventTypePauseOrResumeRequest", Start: "broken", End: "broken"},
		{Type: "HKWorkoutEventTypeTeleport", Start: "broken", End: "broken"},
	}

	got, err := ingest.NormalizeWorkout(w, importedAt)
	require.NoError(t, err)

	instant := utc(t, "2026-11-10T17:10:00Z")
	require.Equal(t, []store.WorkoutEvent{
		{Type: store.WorkoutEventPause, Start: instant, End: instant},
		{Type: store.WorkoutEventResume, Start: instant, End: instant},
		{Type: store.WorkoutEventLap, Start: instant, End: instant},
		{Type: store.WorkoutEventSegment, Start: instant, End: instant},
		{Type: store.WorkoutEventMarker, Start: instant, End: instant},
		{Type: store.WorkoutEventPause, Start: instant, End: instant},
		{Type: store.WorkoutEventResume, Start: instant, End: instant},
	}, got.Events)
	require.Equal(t, []string{`event 8: type "HKWorkoutEventTypeTeleport" is not a known event type, ignored`}, got.Warnings)
}

// Given a workout whose events are all ignored, when NormalizeWorkout runs, then it has no events.
func TestNormalizeWorkout_OnlyIgnoredEvents_HasNoEvents(t *testing.T) {
	t.Parallel()

	w := workout()
	w.Events = []ingest.WorkoutEvent{{Type: "HKWorkoutEventTypePauseOrResumeRequest"}}

	got, err := ingest.NormalizeWorkout(w, importedAt)
	require.NoError(t, err)
	require.Nil(t, got.Events)
	require.Empty(t, got.Warnings)
}
