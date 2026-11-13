package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// workouts implements store.WorkoutsRepository.
type workouts struct {
	q   queries
	clk clock.Clock
}

// Upsert checks the workout, its source included, before its first write, so that a refused
// workout writes nothing even in a WithTx whose caller commits anyway.
func (r workouts) Upsert(ctx context.Context, userID int64, workout store.NewWorkout) (store.WorkoutUpsert, error) {
	if err := checkWorkout(workout); err != nil {
		return store.WorkoutUpsert{}, fmt.Errorf("upsert workout of user %d: %w", userID, err)
	}

	events, err := encodeEvents(workout.Events)
	if err != nil {
		return store.WorkoutUpsert{}, fmt.Errorf("upsert workout of user %d: %w", userID, err)
	}

	var result store.WorkoutUpsert

	err = r.q.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetUserSource(ctx, sqlcgen.GetUserSourceParams{UserID: userID, ID: workout.SourceID})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("source %d: %w", workout.SourceID, store.ErrInvalid)
		}

		if err != nil {
			return err
		}

		result, err = upsertWorkout(ctx, q, userID, workout, events)

		return err
	})
	if err != nil {
		return store.WorkoutUpsert{}, fmt.Errorf("upsert workout of user %d: %w", userID, err)
	}

	return result, nil
}

func (r workouts) Get(ctx context.Context, userID, id int64) (store.Workout, error) {
	row, err := r.q.read().GetUserWorkout(ctx, sqlcgen.GetUserWorkoutParams{UserID: userID, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return store.Workout{}, fmt.Errorf("get workout %d of user %d: %w", id, userID, store.ErrNotFound)
	}

	if err != nil {
		return store.Workout{}, fmt.Errorf("get workout %d of user %d: %w", id, userID, err)
	}

	workout, err := workoutOf(row)
	if err != nil {
		return store.Workout{}, fmt.Errorf("get workout %d of user %d: %w", id, userID, err)
	}

	return workout, nil
}

func (r workouts) List(ctx context.Context, userID int64, query store.WorkoutsQuery) ([]store.Workout, error) {
	if query.Limit < 1 || !query.To.After(query.From) {
		return nil, fmt.Errorf("list workouts of user %d: limit %d, from %s to %s: %w", userID, query.Limit,
			query.From, query.To, store.ErrInvalid)
	}

	// The position before every workout, so that the query always has one.
	params := sqlcgen.ListUserWorkoutsParams{
		UserID:      userID,
		FromAt:      toMillis(query.From),
		ToAt:        toMillis(query.To),
		AfterAt:     math.MaxInt64,
		AfterID:     math.MaxInt64,
		MaxWorkouts: int64(query.Limit),
	}

	if query.After != (store.Cursor{}) {
		params.AfterAt = toMillis(query.After.At)
		params.AfterID = query.After.ID
	}

	rows, err := r.q.read().ListUserWorkouts(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list workouts of user %d: %w", userID, err)
	}

	list := make([]store.Workout, 0, len(rows))
	for _, row := range rows {
		workout, err := workoutOf(row)
		if err != nil {
			return nil, fmt.Errorf("list workouts of user %d: %w", userID, err)
		}

		list = append(list, workout)
	}

	return list, nil
}

func (r workouts) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete workouts of user %d: limit %d below 1: %w", userID, limit, store.ErrInvalid)
	}

	var deleted int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		deleted, err = q.DeleteUserWorkouts(ctx, sqlcgen.DeleteUserWorkoutsParams{UserID: userID, BatchSize: int64(limit)})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete workouts of user %d: %w", userID, err)
	}

	return deleted, nil
}

func (r workouts) DeleteByKey(ctx context.Context, userID int64, key []byte) (store.Workout, error) {
	var removed store.Workout

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		row, err := q.DeleteUserWorkoutByNaturalKey(ctx, sqlcgen.DeleteUserWorkoutByNaturalKeyParams{UserID: userID, NaturalKey: key})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		removed, err = workoutOf(row)

		return err
	})
	if err != nil {
		return store.Workout{}, fmt.Errorf("delete workout of user %d by key: %w", userID, err)
	}

	return removed, nil
}

// checkWorkout returns ErrInvalid when workout has a field that the store refuses, the reference to
// its source aside.
func checkWorkout(workout store.NewWorkout) error {
	switch {
	case workout.OnConflict != store.ConflictUpdate && workout.OnConflict != store.ConflictIgnore:
		return fmt.Errorf("conflict rule %q: %w", workout.OnConflict, store.ErrInvalid)
	case !workout.Origin.Known():
		return fmt.Errorf("origin %q: %w", workout.Origin, store.ErrInvalid)
	case !workout.LocationType.Known():
		return fmt.Errorf("location type %q: %w", workout.LocationType, store.ErrInvalid)
	case len(workout.NaturalKey) == 0:
		return fmt.Errorf("empty natural key: %w", store.ErrInvalid)
	case workout.Activity == "":
		return fmt.Errorf("empty activity: %w", store.ErrInvalid)
	case toMillis(workout.End) < toMillis(workout.Start):
		return fmt.Errorf("end before start: %w", store.ErrInvalid)
	// Written so that NaN, which compares false, is refused too.
	case !(workout.Duration >= 0):
		return fmt.Errorf("duration %v: %w", workout.Duration, store.ErrInvalid)
	case workout.Metadata != nil && !isJSONObject(workout.Metadata):
		return fmt.Errorf("metadata is not a JSON object: %w", store.ErrInvalid)
	}

	for i, event := range workout.Events {
		switch {
		case !event.Type.Known():
			return fmt.Errorf("event %d: type %q: %w", i, event.Type, store.ErrInvalid)
		case toMillis(event.End) < toMillis(event.Start):
			return fmt.Errorf("event %d: end before start: %w", i, store.ErrInvalid)
		}
	}

	return nil
}

// upsertWorkout inserts workout, whose events are encoded as events, for the user of userID, or
// applies its rule to the stored workout of its natural key.
func upsertWorkout(ctx context.Context, q *sqlcgen.Queries, userID int64, workout store.NewWorkout, events string) (store.WorkoutUpsert, error) {
	id, err := q.InsertWorkout(ctx, sqlcgen.InsertWorkoutParams{
		UserID:        userID,
		SourceID:      workout.SourceID,
		Origin:        string(workout.Origin),
		ExternalID:    workout.ExternalID,
		Activity:      workout.Activity,
		StartAt:       toMillis(workout.Start),
		EndAt:         toMillis(workout.End),
		UtcOffset:     int64(workout.UTCOffset),
		Duration:      workout.Duration,
		Distance:      nullFloat(workout.Distance),
		ActiveEnergy:  nullFloat(workout.ActiveEnergy),
		TotalEnergy:   nullFloat(workout.TotalEnergy),
		ElevationGain: nullFloat(workout.ElevationGain),
		ElevationLoss: nullFloat(workout.ElevationLoss),
		AvgHeartRate:  nullFloat(workout.AvgHeartRate),
		MaxHeartRate:  nullFloat(workout.MaxHeartRate),
		Indoor:        nullBool(workout.Indoor),
		LocationType:  string(workout.LocationType),
		Events:        events,
		Metadata:      nullJSON(workout.Metadata),
		ImportID:      workout.ImportID,
		NaturalKey:    workout.NaturalKey,
	})
	if err == nil {
		return store.WorkoutUpsert{ID: id, Outcome: store.OutcomeInserted}, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return store.WorkoutUpsert{}, constraintError(err)
	}

	if workout.OnConflict == store.ConflictUpdate {
		id, err := updateWorkout(ctx, q, userID, workout, events)
		if err == nil {
			return store.WorkoutUpsert{ID: id, Outcome: store.OutcomeUpdated}, nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return store.WorkoutUpsert{}, constraintError(err)
		}
	}

	// The rule ignore, or the rule update of a workout that changed nothing: the stored one stays.
	id, err = q.GetUserWorkoutIDByNaturalKey(ctx, sqlcgen.GetUserWorkoutIDByNaturalKeyParams{
		UserID:     userID,
		NaturalKey: workout.NaturalKey,
	})
	if err != nil {
		return store.WorkoutUpsert{}, err
	}

	return store.WorkoutUpsert{ID: id, Outcome: store.OutcomeDuplicate}, nil
}

// updateWorkout replaces the stored workout of the natural key of workout, of the user of userID,
// and returns its id. It returns sql.ErrNoRows when no field but the import id differs.
func updateWorkout(ctx context.Context, q *sqlcgen.Queries, userID int64, workout store.NewWorkout, events string) (int64, error) {
	return q.UpdateWorkout(ctx, sqlcgen.UpdateWorkoutParams{
		SourceID:      workout.SourceID,
		Origin:        string(workout.Origin),
		ExternalID:    workout.ExternalID,
		Activity:      workout.Activity,
		StartAt:       toMillis(workout.Start),
		EndAt:         toMillis(workout.End),
		UtcOffset:     int64(workout.UTCOffset),
		Duration:      workout.Duration,
		Distance:      nullFloat(workout.Distance),
		ActiveEnergy:  nullFloat(workout.ActiveEnergy),
		TotalEnergy:   nullFloat(workout.TotalEnergy),
		ElevationGain: nullFloat(workout.ElevationGain),
		ElevationLoss: nullFloat(workout.ElevationLoss),
		AvgHeartRate:  nullFloat(workout.AvgHeartRate),
		MaxHeartRate:  nullFloat(workout.MaxHeartRate),
		Indoor:        nullBool(workout.Indoor),
		LocationType:  string(workout.LocationType),
		Events:        events,
		Metadata:      nullJSON(workout.Metadata),
		ImportID:      workout.ImportID,
		UserID:        userID,
		NaturalKey:    workout.NaturalKey,
	})
}

// workoutEvent is the JSON of an event in the events column, times in Unix milliseconds.
type workoutEvent struct {
	Type  store.WorkoutEventType `json:"type"`
	Start int64                  `json:"start"`
	End   int64                  `json:"end"`
}

// encodeEvents returns the JSON array of events, empty when there are none.
func encodeEvents(events []store.WorkoutEvent) (string, error) {
	encoded := make([]workoutEvent, 0, len(events))
	for _, event := range events {
		encoded = append(encoded, workoutEvent{Type: event.Type, Start: toMillis(event.Start), End: toMillis(event.End)})
	}

	data, err := json.Marshal(encoded)
	if err != nil {
		return "", fmt.Errorf("encode events: %w", err)
	}

	return string(data), nil
}

// decodeEvents returns the events of the JSON array data, nil when there are none.
func decodeEvents(data string) ([]store.WorkoutEvent, error) {
	var encoded []workoutEvent
	if err := json.Unmarshal([]byte(data), &encoded); err != nil {
		return nil, fmt.Errorf("decode events: %w", err)
	}

	if len(encoded) == 0 {
		return nil, nil
	}

	events := make([]store.WorkoutEvent, 0, len(encoded))
	for _, event := range encoded {
		events = append(events, store.WorkoutEvent{Type: event.Type, Start: fromMillis(event.Start), End: fromMillis(event.End)})
	}

	return events, nil
}

func nullBool(v *bool) sql.NullInt64 {
	switch {
	case v == nil:
		return sql.NullInt64{}
	case *v:
		return sql.NullInt64{Int64: 1, Valid: true}
	default:
		return sql.NullInt64{Int64: 0, Valid: true}
	}
}

func fromNullBool(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}

	b := v.Int64 != 0

	return &b
}

func workoutOf(row sqlcgen.Workout) (store.Workout, error) {
	events, err := decodeEvents(row.Events)
	if err != nil {
		return store.Workout{}, err
	}

	var metadata json.RawMessage
	if row.Metadata.Valid {
		metadata = json.RawMessage(row.Metadata.String)
	}

	return store.Workout{
		ID:            row.ID,
		UserID:        row.UserID,
		SourceID:      row.SourceID,
		Origin:        store.Origin(row.Origin),
		ExternalID:    row.ExternalID,
		Activity:      row.Activity,
		Start:         fromMillis(row.StartAt),
		End:           fromMillis(row.EndAt),
		UTCOffset:     int(row.UtcOffset),
		Duration:      row.Duration,
		Distance:      fromNullFloat(row.Distance),
		ActiveEnergy:  fromNullFloat(row.ActiveEnergy),
		TotalEnergy:   fromNullFloat(row.TotalEnergy),
		ElevationGain: fromNullFloat(row.ElevationGain),
		ElevationLoss: fromNullFloat(row.ElevationLoss),
		AvgHeartRate:  fromNullFloat(row.AvgHeartRate),
		MaxHeartRate:  fromNullFloat(row.MaxHeartRate),
		Indoor:        fromNullBool(row.Indoor),
		LocationType:  store.LocationType(row.LocationType),
		Events:        events,
		Metadata:      metadata,
		ImportID:      row.ImportID,
		NaturalKey:    row.NaturalKey,
	}, nil
}
