package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
)

// One Metric read inside one workout: the shape of a ride's heart rate, on the
// ride's own axis (ADR 0041).
//
// It hangs off a Session rather than off /v1/series because the axis belongs to
// the entity. There is no range parameter here and there cannot be one: the
// window is the workout's own, and the grain is derived from its length, so a
// caller has nothing to widen and ADR 0012's cap on the Series contract is
// untouched.

// workoutBuckets is how many points a workout's curve comes back as, at most. A
// 20-minute run and a 6-hour ride are both bounded by it, which is the property
// that keeps this from becoming a general sub-day read one parameter at a time.
// It is the same trade the Route already makes by simplifying a polyline
// server-side (ADR 0028): the screen gets a shape it can draw, the stored rows
// are untouched.
const workoutBuckets = 300

// ErrUnsupportedInWorkout is a Metric that has no meaning inside an hour.
var ErrUnsupportedInWorkout = errors.New("query: metric cannot be read inside a workout")

// WorkoutPoint is one bucket of a workout's curve: the mean of its readings and
// how many there were.
type WorkoutPoint struct {
	At    string  `json:"at"`
	Value float64 `json:"value"`
	Count int     `json:"count"`
}

// WorkoutSeries is one Metric over one workout. StartAt and EndAt are the
// workout's own bounds, echoed so a client draws an axis without inventing one.
type WorkoutSeries struct {
	Metric  string         `json:"metric"`
	Unit    string         `json:"unit"`
	Source  string         `json:"source"`
	StartAt string         `json:"start_at"`
	EndAt   string         `json:"end_at"`
	Points  []WorkoutPoint `json:"points"`
}

// WorkoutRequest is one curve: a Metric, and the window of the Session it is
// read inside.
type WorkoutRequest struct {
	AccountID int64
	Metric    string
	From      time.Time
	To        time.Time
}

// WorkoutSeriesFor folds the Measurements inside a workout's window into at most
// workoutBuckets points.
//
// A Metric with no readings in the window is an empty series rather than an
// error: the workout exists and the curve does not, which is a fact about the
// device rather than about the request.
func (e Engine) WorkoutSeriesFor(ctx context.Context, req WorkoutRequest) (WorkoutSeries, error) {
	metric, ok := catalog.Lookup(req.Metric)
	if !ok {
		return WorkoutSeries{}, fmt.Errorf("%w: %q", ErrUnknownMetric, req.Metric)
	}
	// `latest` has no meaning inside an hour (the last reading of a ride is a
	// figure, not a curve), a by-state rule reads another family entirely, and a
	// derived Metric is computed per bucket from operands the Formula folds over
	// days (ADR 0014).
	if metric.Nature != catalog.Imported || (metric.Aggregation != catalog.Average && metric.Aggregation != catalog.Sum) {
		return WorkoutSeries{}, fmt.Errorf("%w: %q", ErrUnsupportedInWorkout, req.Metric)
	}
	if !req.To.After(req.From) {
		return WorkoutSeries{}, ErrInvalidRange
	}

	out := WorkoutSeries{
		Metric:  metric.Slug,
		Unit:    metric.Unit,
		StartAt: rfc3339(req.From),
		EndAt:   rfc3339(req.To),
		Points:  []WorkoutPoint{},
	}

	source, err := e.workoutSource(ctx, req)
	if err != nil {
		return WorkoutSeries{}, err
	}
	if source == "" {
		return out, nil // nothing recorded in the window: an empty curve, no Source
	}
	out.Source = source

	points, err := e.workoutPoints(ctx, req, source)
	if err != nil {
		return WorkoutSeries{}, err
	}
	out.Points = points
	return out, nil
}

// workoutSource elects one Source for the whole workout.
//
// Not per day and not per bucket: inside one session the question "which device"
// has a single answer, and switching Source mid-ride would draw a discontinuity
// that is an artefact of the read rather than a thing that happened. The
// priority table is the one every read uses (ADR 0003).
func (e Engine) workoutSource(ctx context.Context, req WorkoutRequest) (string, error) {
	const q = `
		SELECT DISTINCT source FROM measurements
		 WHERE account_id = ? AND metric = ? AND start_at >= ? AND start_at < ?`
	rows, err := e.DB.QueryContext(ctx, q, req.AccountID, req.Metric, rfc3339(req.From), rfc3339(req.To))
	if err != nil {
		return "", fmt.Errorf("query: workout sources: %w", err)
	}
	defer rows.Close()

	var available []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", fmt.Errorf("query: scan workout source: %w", err)
		}
		available = append(available, s)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("query: workout sources: %w", err)
	}
	if len(available) == 0 {
		return "", nil
	}
	source, _ := catalog.ResolveSource(req.Metric, available)
	return source, nil
}

// workoutPoints buckets the winning Source's readings across the workout's span.
// An empty bucket yields no Point: a gap is never a zero (ADR 0014), and a strap
// that dropped out for a minute did not record a zero heart rate.
func (e Engine) workoutPoints(ctx context.Context, req WorkoutRequest, source string) ([]WorkoutPoint, error) {
	span := req.To.Sub(req.From)
	step := span / workoutBuckets
	if step < time.Second {
		step = time.Second // a two-minute workout is read per second, not per 400ms
	}

	const q = `
		SELECT start_at, value FROM measurements
		 WHERE account_id = ? AND metric = ? AND source = ?
		   AND start_at >= ? AND start_at < ?
		 ORDER BY start_at`
	rows, err := e.DB.QueryContext(ctx, q, req.AccountID, req.Metric, source, rfc3339(req.From), rfc3339(req.To))
	if err != nil {
		return nil, fmt.Errorf("query: workout points: %w", err)
	}
	defer rows.Close()

	var (
		out     []WorkoutPoint
		bucket  time.Time
		sum     float64
		count   int
		started bool
	)
	flush := func() {
		if count == 0 {
			return
		}
		out = append(out, WorkoutPoint{At: rfc3339(bucket), Value: sum / float64(count), Count: count})
	}

	for rows.Next() {
		var at string
		var value float64
		if err := rows.Scan(&at, &value); err != nil {
			return nil, fmt.Errorf("query: scan workout point: %w", err)
		}
		t, perr := time.Parse(time.RFC3339, at)
		if perr != nil {
			continue // a timestamp no Connector could parse has no place on an axis
		}
		slot := req.From.Add(t.Sub(req.From) / step * step)
		if !started || !slot.Equal(bucket) {
			flush()
			bucket, sum, count, started = slot, 0, 0, true
		}
		sum += value
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: workout points: %w", err)
	}
	flush()
	return out, nil
}
