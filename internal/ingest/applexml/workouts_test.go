package applexml_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

const (
	// run is the start tag of a run of the watch.
	run = `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="25" durationUnit="min" sourceName="Apple Watch de Alex" sourceVersion="26.0" device="&lt;&lt;HKDevice: 0x1b82daec0&gt;, name:Apple Watch, manufacturer:Apple Inc., model:Watch, hardware:Watch7,3, software:26.0, creation date:2025-12-29 23:00:00 +0000&gt;" creationDate="2025-12-30 07:25:00 +0100" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">`
	// effort is a record inside a workout.
	effort = `<Record type="HKQuantityTypeIdentifierEstimatedWorkoutEffortScore" sourceName="Apple Watch de Alex" unit="appleEffortScore" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100" value="7"/>`
	// power is another record inside a workout.
	power = `<Record type="HKQuantityTypeIdentifierRunningPower" sourceName="Apple Watch de Alex" unit="W" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:00:00 +0100" value="250"/>`
)

// workoutOf returns the workout of records, which must hold exactly one.
func workoutOf(t *testing.T, records []ingest.Record) *ingest.Workout {
	t.Helper()

	var workouts []*ingest.Workout

	for _, r := range records {
		if r.Workout != nil {
			workouts = append(workouts, r.Workout)
		}
	}

	require.Len(t, workouts, 1)

	return workouts[0]
}

// Given a Workout, then it yields one workout record whose position is the byte offset of its end
// tag, after the records of its inner Record items, each at the offset of its start tag. The
// positions of the document still increase strictly.
func TestRecords_WorkoutComesAfterItsInnerRecordsAtItsEndTag(t *testing.T) {
	t.Parallel()

	doc := document(
		heartRate,
		run,
		` <MetadataEntry key="HKIndoorWorkout" value="0"/>`,
		` `+power,
		` <WorkoutEvent type="HKWorkoutEventTypePause" date="2025-12-30 07:12:30 +0100"/>`,
		` `+effort,
		`</Workout>`,
		sleep,
	)

	records := readAll(t, doc)
	require.Len(t, records, 5)

	require.Equal(t, offset(t, doc, "<Record", 0), records[0].Position)
	require.Equal(t, "HKQuantityTypeIdentifierHeartRate", records[0].Quantity.Type)
	require.Equal(t, offset(t, doc, "<Record", 1), records[1].Position)
	require.Equal(t, "HKQuantityTypeIdentifierRunningPower", records[1].Quantity.Type)
	require.Equal(t, offset(t, doc, "<Record", 2), records[2].Position)
	require.Equal(t, "HKQuantityTypeIdentifierEstimatedWorkoutEffortScore", records[2].Quantity.Type)
	require.Equal(t, offset(t, doc, "</Workout>", 0), records[3].Position)
	require.NotNil(t, records[3].Workout)
	require.Equal(t, offset(t, doc, "<Record", 3), records[4].Position)
	require.NotNil(t, records[4].Category)

	for i := 1; i < len(records); i++ {
		require.Less(t, records[i-1].Position, records[i].Position)
	}
}

// Given a workout, then its activity type is workoutActivityType, its start and end are its dates,
// its duration is duration in durationUnit, its source comes from sourceName and device as for a
// record, and it has no uuid.
func TestRecords_WorkoutAttributes(t *testing.T) {
	t.Parallel()

	w := workoutOf(t, readAll(t, document(run, `</Workout>`)))

	require.Empty(t, w.UUID)
	require.Equal(t, "HKWorkoutActivityTypeRunning", w.ActivityType)
	require.Equal(t, "2025-12-30T07:00:00+01:00", w.Start)
	require.Equal(t, "2025-12-30T07:25:00+01:00", w.End)
	require.Equal(t, &ingest.Quantity{Value: 25, Unit: "min"}, w.Duration)
	require.Equal(t, store.SourceInfo{
		Name: "Apple Watch de Alex", DeviceName: "Apple Watch", Manufacturer: "Apple Inc.", DeviceModel: "Watch",
		HardwareVersion: "Watch7,3", SoftwareVersion: "26.0",
	}, w.Source)

	t.Run("without duration", func(t *testing.T) {
		t.Parallel()

		w := workoutOf(t, readAll(t, document(
			`<Workout workoutActivityType="HKWorkoutActivityTypeYoga" sourceName="Mat" startDate="2025-12-30 18:00:00 +0100" endDate="2025-12-30 18:40:00 +0100"/>`,
		)))
		require.Nil(t, w.Duration, "the pipeline gets it from the events")
		require.Equal(t, store.SourceInfo{Name: "Mat"}, w.Source)
	})
}

// Given a workout that its adapter cannot read, then it is an invalid workout record at its end
// tag, with a reason that names the attribute and never its value.
func TestRecords_InvalidWorkouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		workout string
		reason  string
	}{
		{
			name:    "without activity type",
			workout: `<Workout sourceName="Watch" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">`,
			reason:  "workoutActivityType is missing",
		},
		{
			name:    "start that is not a date",
			workout: `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" sourceName="Watch" startDate="yesterday" endDate="2025-12-30 07:25:00 +0100">`,
			reason:  "startDate is not a date",
		},
		{
			name:    "without end",
			workout: `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" sourceName="Watch" startDate="2025-12-30 07:00:00 +0100">`,
			reason:  "endDate is missing",
		},
		{
			name:    "duration that is not a number",
			workout: `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" duration="long" durationUnit="min" sourceName="Watch" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">`,
			reason:  "duration is not a number",
		},
		{
			name: "event without date",
			workout: `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" sourceName="Watch" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">` +
				`<WorkoutEvent type="HKWorkoutEventTypePause"/>`,
			reason: "WorkoutEvent date is missing",
		},
		{
			name: "event whose duration is not a number",
			workout: `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" sourceName="Watch" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">` +
				`<WorkoutEvent type="HKWorkoutEventTypeSegment" date="2025-12-30 07:00:00 +0100" duration="ten" durationUnit="min"/>`,
			reason: "WorkoutEvent duration is not a number",
		},
		{
			name: "event whose duration is not a time",
			workout: `<Workout workoutActivityType="HKWorkoutActivityTypeRunning" sourceName="Watch" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">` +
				`<WorkoutEvent type="HKWorkoutEventTypeSegment" date="2025-12-30 07:00:00 +0100" duration="10" durationUnit="km"/>`,
			reason: "WorkoutEvent durationUnit is not a unit of time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := document(tt.workout, `</Workout>`, heartRate)

			records := readAll(t, doc)
			require.Len(t, records, 2)
			require.Equal(t, ingest.Record{
				Position: offset(t, doc, "</Workout>", 0),
				Invalid:  &ingest.Invalid{Of: ingest.WorkoutRecord, Reason: tt.reason},
			}, records[0])
			require.NotNil(t, records[1].Quantity, "the next element is read")
		})
	}
}

// Given a WorkoutEvent, then it starts at date and ends duration in durationUnit later, or at date
// without duration, in document order.
func TestRecords_WorkoutEvents(t *testing.T) {
	t.Parallel()

	w := workoutOf(t, readAll(t, document(
		run,
		` <WorkoutEvent type="HKWorkoutEventTypePause" date="2025-12-30 07:12:30 +0100"/>`,
		` <WorkoutEvent type="HKWorkoutEventTypeResume" date="2025-12-30 07:13:30 +0100"/>`,
		` <WorkoutEvent type="HKWorkoutEventTypeSegment" date="2025-12-30 07:00:00 +0100" duration="10.0000" durationUnit="min"/>`,
		` <WorkoutEvent type="HKWorkoutEventTypeLap" date="2025-12-30 07:10:00 +0100" duration="90.5" durationUnit="s"/>`,
		` <WorkoutEvent type="HKWorkoutEventTypeMarker" date="2025-12-30 07:20:00 +0100" duration="0.25" durationUnit="hr"/>`,
		`</Workout>`,
	)))

	require.Equal(t, []ingest.WorkoutEvent{
		{Type: "HKWorkoutEventTypePause", Start: "2025-12-30T07:12:30+01:00", End: "2025-12-30T07:12:30+01:00"},
		{Type: "HKWorkoutEventTypeResume", Start: "2025-12-30T07:13:30+01:00", End: "2025-12-30T07:13:30+01:00"},
		{Type: "HKWorkoutEventTypeSegment", Start: "2025-12-30T07:00:00+01:00", End: "2025-12-30T07:10:00+01:00"},
		{Type: "HKWorkoutEventTypeLap", Start: "2025-12-30T07:10:00+01:00", End: "2025-12-30T07:11:30.5+01:00"},
		{Type: "HKWorkoutEventTypeMarker", Start: "2025-12-30T07:20:00+01:00", End: "2025-12-30T07:35:00+01:00"},
	}, w.Events)

	require.Nil(t, workoutOf(t, readAll(t, document(run, `</Workout>`))).Events, "a workout without events has none")
}
