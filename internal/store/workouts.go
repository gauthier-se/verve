package store

import (
	"context"
	"encoding/json"
	"time"
)

// ConflictUpdate is the conflict rule of a workout with an external id (FR-1 of Deduplication and
// source priority): it replaces every field of the stored workout but its id, so that its route
// stays attached, and the workout is updated. Samples do not take it.
const ConflictUpdate ConflictRule = "update"

// LocationType is where a workout took place.
type LocationType string

// The location types of a workout: those of a HealthKit workout session, and those of a swim.
const (
	LocationUnknown   LocationType = "unknown"
	LocationIndoor    LocationType = "indoor"
	LocationOutdoor   LocationType = "outdoor"
	LocationPool      LocationType = "pool"
	LocationOpenWater LocationType = "open_water"
)

// Known reports whether l is one of the location types.
func (l LocationType) Known() bool {
	switch l {
	case LocationUnknown, LocationIndoor, LocationOutdoor, LocationPool, LocationOpenWater:
		return true
	default:
		return false
	}
}

// WorkoutEventType is the type of an event of a workout.
type WorkoutEventType string

// The types of the events of a workout.
const (
	WorkoutEventPause   WorkoutEventType = "pause"
	WorkoutEventResume  WorkoutEventType = "resume"
	WorkoutEventLap     WorkoutEventType = "lap"
	WorkoutEventSegment WorkoutEventType = "segment"
	WorkoutEventMarker  WorkoutEventType = "marker"
)

// Known reports whether e is one of the types of the events of a workout.
func (e WorkoutEventType) Known() bool {
	switch e {
	case WorkoutEventPause, WorkoutEventResume, WorkoutEventLap, WorkoutEventSegment, WorkoutEventMarker:
		return true
	default:
		return false
	}
}

// WorkoutEvent is an event of a workout. Times are in UTC with millisecond precision. An instant
// event, such as a pause, ends when it starts.
type WorkoutEvent struct {
	Type  WorkoutEventType
	Start time.Time
	End   time.Time
}

// Workout is a workout of a user (FR-4 of Storage and aggregates). Times are in UTC with
// millisecond precision, and values in canonical units. A total that the workout lacks is nil.
type Workout struct {
	ID       int64
	UserID   int64
	SourceID int64
	Origin   Origin
	// ExternalID is the id of the workout at its origin, such as its uuid, empty when it has none.
	ExternalID string
	// Activity is the key of its activity type in the activity type catalog.
	Activity string
	Start    time.Time
	End      time.Time
	// UTCOffset is the offset of the local time where the workout was recorded, in seconds east of
	// UTC.
	UTCOffset int
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
	// Indoor is the indoor flag of the workout, nil when unknown.
	Indoor       *bool
	LocationType LocationType
	// Events are the events of the workout, in the order received, nil when it has none.
	Events []WorkoutEvent
	// Metadata is a JSON object, nil when the workout has none.
	Metadata json.RawMessage
	// ImportID is the import that stored the workout last. It references nothing.
	ImportID int64
	// NaturalKey is the hash that identifies the workout among those of its user.
	NaturalKey []byte
}

// Cursor returns the position of the workout in the lists of WorkoutsRepository.List.
func (w Workout) Cursor() Cursor {
	return Cursor{At: w.Start, ID: w.ID}
}

// NewWorkout is a workout to upsert, with the rule to apply when its natural key is already
// stored: ConflictUpdate or ConflictIgnore. Its ID and UserID are ignored.
type NewWorkout struct {
	Workout

	OnConflict ConflictRule
}

// UpsertOutcome is what an upsert did with a record.
type UpsertOutcome string

// The outcomes of an upsert.
const (
	OutcomeInserted  UpsertOutcome = "inserted"
	OutcomeUpdated   UpsertOutcome = "updated"
	OutcomeDuplicate UpsertOutcome = "duplicate"
)

// WorkoutUpsert is the result of the upsert of a workout: what happened to it, and the id of the
// stored workout.
type WorkoutUpsert struct {
	ID      int64
	Outcome UpsertOutcome
}

// WorkoutsQuery asks for the workouts whose start is in [From, To), most recent first, by start then
// id, after a position.
type WorkoutsQuery struct {
	From time.Time
	To   time.Time
	// After is the position of the last workout of the previous page, or zero for the first page.
	After Cursor
	// Limit is the maximum number of workouts, at least 1.
	Limit int
}

// WorkoutsRepository stores the workouts of each user. A workout of another user is never returned.
type WorkoutsRepository interface {
	// Upsert stores workout for userID. A workout whose natural key userID has not stored yet is
	// inserted, and otherwise handled by its OnConflict rule.
	//
	// It returns ErrInvalid, and writes nothing, when the workout has an empty natural key or
	// activity, a rule other than ConflictUpdate and ConflictIgnore, an unknown origin or location
	// type, an end before its start, a negative duration, an event of an unknown type or ending
	// before its start, metadata that is not a JSON object, or a source that userID does not have.
	Upsert(ctx context.Context, userID int64, workout NewWorkout) (WorkoutUpsert, error)
	// Get returns the workout of id of userID, or ErrNotFound.
	Get(ctx context.Context, userID, id int64) (Workout, error)
	// List returns the workouts of userID that query asks for. It returns ErrInvalid when the limit
	// is below 1 or To is not after From.
	List(ctx context.Context, userID int64, query WorkoutsQuery) ([]Workout, error)
	// DeleteByUser removes at most limit workouts of userID and returns how many it removed. It
	// returns ErrInvalid when limit is below 1.
	DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error)
}
