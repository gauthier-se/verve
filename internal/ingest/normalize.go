package ingest

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// The normalization of FR-2 stages 2 and 3 of Ingestion pipeline: units converted, times parsed and
// checked. The functions are pure: an error means that the record is invalid, and its message is
// the reason, short enough for a sample error message of the report, and never a value.

// maxAhead is how far after the creation of its import a record may start without being
// suspicious.
const maxAhead = 24 * time.Hour

// Times are the times of a record, in UTC with millisecond precision.
type Times struct {
	Start time.Time
	End   time.Time
	// UTCOffset is the offset of the start, in seconds east of UTC.
	UTCOffset int
	// Suspicious is true for a record that starts before 2000-01-01T00:00:00Z, or more than one day
	// after its import was created: it is still stored, and counted as suspicious.
	Suspicious bool
}

// NormalizedSample is a quantity sample whose value is in the unit of its type.
type NormalizedSample struct {
	Times
	Value float64
	// Min and Max are those of a pre-aggregated point, nil for other samples.
	Min, Max *float64
}

// Event is an event of a workout, with its times parsed and its type as received.
type Event struct {
	Type  string
	Start time.Time
	End   time.Time
}

// NormalizedWorkout is a workout whose totals are in canonical units, and its events and route
// parsed. A total that the workout lacks is nil.
type NormalizedWorkout struct {
	Times
	// Duration is the time of the workout in seconds, pauses excluded.
	Duration float64
	// Distance, ElevationGain and ElevationLoss are in m.
	Distance *float64
	// ActiveEnergy and TotalEnergy are in kcal.
	ActiveEnergy  *float64
	TotalEnergy   *float64
	ElevationGain *float64
	ElevationLoss *float64
	// AvgHeartRate and MaxHeartRate are in count/min.
	AvgHeartRate *float64
	MaxHeartRate *float64
	// Events are in the order received, nil when the workout has none.
	Events []Event
	// Route is nil when the workout has none.
	Route []route.Point
}

// The event types that pause and resume a workout.
const (
	eventPause         = "HKWorkoutEventTypePause"
	eventResume        = "HKWorkoutEventTypeResume"
	eventMotionPaused  = "HKWorkoutEventTypeMotionPaused"
	eventMotionResumed = "HKWorkoutEventTypeMotionResumed"
)

// NormalizeQuantity converts the value, min and max of s to the unit of its type t, and parses its
// times, flagging them suspicious against importedAt, when its import was created.
func NormalizeQuantity(s QuantitySample, t store.MetricType, importedAt time.Time) (NormalizedSample, error) {
	times, err := recordTimes(s.Start, s.End, importedAt)
	if err != nil {
		return NormalizedSample{}, err
	}

	value, err := ConvertValue(s.Value, s.Unit, t)
	if err != nil {
		return NormalizedSample{}, err
	}

	lo, err := convertOptional(s.Min, s.Unit, t)
	if err != nil {
		return NormalizedSample{}, err
	}

	hi, err := convertOptional(s.Max, s.Unit, t)
	if err != nil {
		return NormalizedSample{}, err
	}

	return NormalizedSample{Times: times, Value: value, Min: lo, Max: hi}, nil
}

// NormalizeCategory parses the times of s, flagging them suspicious against importedAt, when its
// import was created.
func NormalizeCategory(s CategorySample, importedAt time.Time) (Times, error) {
	return recordTimes(s.Start, s.End, importedAt)
}

// NormalizeWorkout parses the times of w, its events and its route, flagging them suspicious
// against importedAt, when its import was created, and converts its totals to canonical units. A
// workout without duration gets its time range without its pauses: from each pause, or motion
// pause, to the next resume, or to its end. A route point must be within the latitudes [-90, 90],
// the longitudes [-180, 180] and the times of the workout. A negative speed, course or accuracy,
// which CoreLocation writes when the value is unknown, counts as left out.
func NormalizeWorkout(w Workout, importedAt time.Time) (NormalizedWorkout, error) {
	times, err := recordTimes(w.Start, w.End, importedAt)
	if err != nil {
		return NormalizedWorkout{}, err
	}

	n := NormalizedWorkout{Times: times}

	if n.Events, err = parseEvents(w.Events); err != nil {
		return NormalizedWorkout{}, err
	}

	if n.Duration, err = duration(w.Duration, times, n.Events); err != nil {
		return NormalizedWorkout{}, err
	}

	totals := []struct {
		name string
		in   *Quantity
		unit string
		out  **float64
	}{
		{name: "distance", in: w.Totals.Distance, unit: "m", out: &n.Distance},
		{name: "active energy", in: w.Totals.ActiveEnergy, unit: "kcal", out: &n.ActiveEnergy},
		{name: "total energy", in: w.Totals.TotalEnergy, unit: "kcal", out: &n.TotalEnergy},
		{name: "elevation ascended", in: w.Totals.ElevationAscended, unit: "m", out: &n.ElevationGain},
		{name: "elevation descended", in: w.Totals.ElevationDescended, unit: "m", out: &n.ElevationLoss},
		{name: "average heart rate", in: w.Totals.AvgHeartRate, unit: "count/min", out: &n.AvgHeartRate},
		{name: "max heart rate", in: w.Totals.MaxHeartRate, unit: "count/min", out: &n.MaxHeartRate},
	}

	for _, total := range totals {
		if total.in == nil {
			continue
		}

		value, err := catalog.Convert(total.in.Value, total.in.Unit, total.unit)
		if err != nil {
			return NormalizedWorkout{}, fmt.Errorf("%s: %w", total.name, err)
		}

		*total.out = &value
	}

	if n.Route, err = parseRoute(w.Route, times); err != nil {
		return NormalizedWorkout{}, err
	}

	return n, nil
}

// convertOptional converts value, when there is one, as ConvertValue does.
func convertOptional(value *float64, unit string, t store.MetricType) (*float64, error) {
	if value == nil {
		return nil, nil
	}

	converted, err := ConvertValue(*value, unit, t)
	if err != nil {
		return nil, err
	}

	return &converted, nil
}

// recordTimes parses the start and the end of a record, and flags them suspicious against
// importedAt.
func recordTimes(start, end string, importedAt time.Time) (Times, error) {
	times, err := parseRange(start, end)
	if err != nil {
		return Times{}, err
	}

	times.Suspicious = times.Start.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) ||
		times.Start.After(importedAt.Add(maxAhead))

	return times, nil
}

// parseRange parses a start and an end, which must not be before the start.
func parseRange(start, end string) (Times, error) {
	from, offset, err := parseTime(start)
	if err != nil {
		return Times{}, fmt.Errorf("start %w", err)
	}

	to, _, err := parseTime(end)
	if err != nil {
		return Times{}, fmt.Errorf("end %w", err)
	}

	if to.Before(from) {
		return Times{}, errors.New("end is before start")
	}

	return Times{Start: from, End: to, UTCOffset: offset}, nil
}

// errNotATime is the reason of a time that is not an RFC 3339 time with an offset. It replaces the
// error of time.Parse, which quotes the time received.
var errNotATime = errors.New("is not an RFC 3339 time with an offset")

// parseTime parses an RFC 3339 time with an offset to UTC with millisecond precision, and returns
// its offset in seconds east of UTC.
func parseTime(s string) (time.Time, int, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, 0, errNotATime
	}

	_, offset := t.Zone()

	return t.UTC().Truncate(time.Millisecond), offset, nil
}

// parseEvents parses the times of events, nil when there are none.
func parseEvents(events []WorkoutEvent) ([]Event, error) {
	if len(events) == 0 {
		return nil, nil
	}

	parsed := make([]Event, 0, len(events))

	for i, e := range events {
		times, err := parseRange(e.Start, e.End)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}

		parsed = append(parsed, Event{Type: e.Type, Start: times.Start, End: times.End})
	}

	return parsed, nil
}

// duration returns the duration of a workout in seconds: the one received, or else the time range
// of the workout without its pauses.
func duration(received *Quantity, times Times, events []Event) (float64, error) {
	if received == nil {
		return activeTime(times, events).Seconds(), nil
	}

	seconds, err := catalog.Convert(received.Value, received.Unit, "s")
	if err != nil {
		return 0, fmt.Errorf("duration: %w", err)
	}

	if seconds < 0 {
		return 0, errors.New("duration is negative")
	}

	return seconds, nil
}

// activeTime returns the time range of a workout without its pauses, each from a pause to the next
// resume, or to the end, and within the time range.
func activeTime(times Times, events []Event) time.Duration {
	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, func(a, b Event) int { return a.Start.Compare(b.Start) })

	within := func(t time.Time) time.Time {
		switch {
		case t.Before(times.Start):
			return times.Start
		case t.After(times.End):
			return times.End
		default:
			return t
		}
	}

	var (
		paused   bool
		pausedAt time.Time
		pauses   time.Duration
	)

	for _, e := range sorted {
		switch e.Type {
		case eventPause, eventMotionPaused:
			if !paused {
				paused, pausedAt = true, within(e.Start)
			}
		case eventResume, eventMotionResumed:
			if paused {
				paused, pauses = false, pauses+within(e.Start).Sub(pausedAt)
			}
		}
	}

	if paused {
		pauses += times.End.Sub(pausedAt)
	}

	return times.End.Sub(times.Start) - pauses
}

// parseRoute parses and checks the points of a route within the times of its workout, nil when
// there are none.
func parseRoute(points []RoutePoint, times Times) ([]route.Point, error) {
	if len(points) == 0 {
		return nil, nil
	}

	parsed := make([]route.Point, 0, len(points))

	for i, p := range points {
		at, _, err := parseTime(p.Time)

		switch {
		case err != nil:
			return nil, fmt.Errorf("route point %d: time %w", i, err)
		case !(p.Latitude >= -90 && p.Latitude <= 90):
			return nil, fmt.Errorf("route point %d: latitude is out of [-90, 90]", i)
		case !(p.Longitude >= -180 && p.Longitude <= 180):
			return nil, fmt.Errorf("route point %d: longitude is out of [-180, 180]", i)
		case at.Before(times.Start) || at.After(times.End):
			return nil, fmt.Errorf("route point %d: time is outside the workout", i)
		}

		parsed = append(parsed, route.Point{
			Time:               at,
			Lat:                p.Latitude,
			Lon:                p.Longitude,
			Altitude:           p.Altitude,
			Speed:              known(p.Speed),
			Course:             known(p.Course),
			HorizontalAccuracy: known(p.HorizontalAccuracy),
			VerticalAccuracy:   known(p.VerticalAccuracy),
		})
	}

	return parsed, nil
}

// known returns value, or nil when it is negative: CoreLocation writes -1 for an unknown speed,
// course or accuracy.
func known(value *float64) *float64 {
	if value == nil || *value < 0 {
		return nil
	}

	return value
}
