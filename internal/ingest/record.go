package ingest

import (
	"encoding/json"

	"github.com/gauthier-se/verve/internal/store"
)

// The normalized records of FR-1 of Ingestion pipeline: what adapters produce, whatever their
// payload format. Identifiers, units and times are as received. A time is an RFC 3339 string with
// an offset, a unit a HealthKit unit string of the Units table of docs/architecture.md.

// Quantity is a value in a unit, as received.
type Quantity struct {
	Value float64
	Unit  string
}

// QuantitySample is a quantity sample, as an adapter produces it.
type QuantitySample struct {
	// UUID is the HealthKit uuid of the sample, empty when it has none.
	UUID string
	// Type is the identifier of the type, such as HKQuantityTypeIdentifierHeartRate or a Health
	// Auto Export name.
	Type  string
	Start string
	// End equals Start for an instant.
	End   string
	Value float64
	Unit  string
	// Min and Max are those of a pre-aggregated point, in Unit, nil for other samples.
	Min, Max *float64
	Source   store.SourceInfo
	// Metadata is a JSON object, nil when the sample has none.
	Metadata json.RawMessage
}

// CategorySample is a category sample, as an adapter produces it.
type CategorySample struct {
	// UUID is the HealthKit uuid of the sample, empty when it has none.
	UUID string
	// Type is the identifier of the type, such as HKCategoryTypeIdentifierSleepAnalysis.
	Type  string
	Start string
	// End equals Start for an instant.
	End string
	// Value is the identifier of the category value, such as
	// HKCategoryValueSleepAnalysisAsleepCore.
	Value    string
	Source   store.SourceInfo
	Metadata json.RawMessage
}

// Workout is a workout with its events and its route, as an adapter produces it.
type Workout struct {
	// UUID is the id of the workout at its origin, such as its HealthKit uuid, empty when it has
	// none.
	UUID string
	// ActivityType is the identifier of the activity type, such as HKWorkoutActivityTypeRunning.
	ActivityType string
	Start        string
	End          string
	// Duration is the time of the workout without its pauses, nil when the adapter lacks it.
	Duration *Quantity
	Totals   WorkoutTotals
	// Indoor is the indoor flag of the workout, nil when unknown.
	Indoor   *bool
	Source   store.SourceInfo
	Metadata json.RawMessage
	// Events are the events of the workout, in the order received.
	Events []WorkoutEvent
	// Route is the route of the workout, in time order, nil when it has none.
	Route []RoutePoint
}

// WorkoutTotals are the totals of a workout, each nil when the workout lacks it.
type WorkoutTotals struct {
	Distance           *Quantity
	ActiveEnergy       *Quantity
	TotalEnergy        *Quantity
	ElevationAscended  *Quantity
	ElevationDescended *Quantity
	AvgHeartRate       *Quantity
	MaxHeartRate       *Quantity
}

// WorkoutEvent is an event of a workout, as received.
type WorkoutEvent struct {
	// Type is the identifier of the event type, such as HKWorkoutEventTypePause.
	Type  string
	Start string
	// End equals Start for an instant event.
	End string
}

// RoutePoint is a point of the route of a workout, as CoreLocation gives it. An optional field is
// nil when the adapter lacks it.
type RoutePoint struct {
	Time string
	// Latitude and Longitude are in degrees.
	Latitude, Longitude float64
	// Altitude is in meters.
	Altitude *float64
	// Speed is in meters per second.
	Speed *float64
	// Course is in degrees from true north.
	Course *float64
	// HorizontalAccuracy and VerticalAccuracy are in meters.
	HorizontalAccuracy, VerticalAccuracy *float64
}
