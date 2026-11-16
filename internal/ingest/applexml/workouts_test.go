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

// Given a self-closing Workout followed by an element without space between them, then the
// workout is at the offset of its start tag, so that the positions still increase strictly.
func TestRecords_SelfClosingWorkoutIsAtItsStartTag(t *testing.T) {
	t.Parallel()

	doc := document(
		`<Workout workoutActivityType="HKWorkoutActivityTypeYoga" sourceName="Mat" startDate="2025-12-30 18:00:00 +0100" endDate="2025-12-30 18:40:00 +0100"/>` + heartRate,
	)

	records := readAll(t, doc)
	require.Len(t, records, 2)
	require.Equal(t, offset(t, doc, "<Workout", 0), records[0].Position)
	require.NotNil(t, records[0].Workout)
	require.Equal(t, offset(t, doc, "<Record", 0), records[1].Position)
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

// statistic returns a WorkoutStatistics element of the run of the type typ, whose other attributes
// are attrs.
func statistic(typ, attrs string) string {
	return ` <WorkoutStatistics type="` + typ + `" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100" ` + attrs + `/>`
}

// Given WorkoutStatistics, then the active energy is the sum of ActiveEnergyBurned, the total
// energy its sum with BasalEnergyBurned when both exist, the distance the sum of the first
// statistic whose type starts with Distance, and the average and maximum heart rates the average
// and maximum of HeartRate, each in its unit. A total without its statistic is left out.
func TestRecords_WorkoutTotalsFromTheStatistics(t *testing.T) {
	t.Parallel()

	quantity := func(value float64, unit string) *ingest.Quantity {
		return &ingest.Quantity{Value: value, Unit: unit}
	}

	tests := []struct {
		name       string
		statistics []string
		want       ingest.WorkoutTotals
	}{
		{
			name: "every statistic",
			statistics: []string{
				statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="250.408" unit="kcal"`),
				statistic("HKQuantityTypeIdentifierBasalEnergyBurned", `sum="29.414" unit="kcal"`),
				statistic("HKQuantityTypeIdentifierStepCount", `sum="4171.189" unit="count"`),
				statistic("HKQuantityTypeIdentifierDistanceWalkingRunning", `sum="4.933" unit="km"`),
				statistic("HKQuantityTypeIdentifierDistanceCycling", `sum="12" unit="km"`),
				statistic("HKQuantityTypeIdentifierHeartRate", `average="142.538" minimum="114.030" maximum="171.045" unit="count/min"`),
				statistic("HKQuantityTypeIdentifierRunningPower", `average="222.863" minimum="178.290" maximum="267.436" unit="W"`),
			},
			want: ingest.WorkoutTotals{
				Distance:     quantity(4.933, "km"),
				ActiveEnergy: quantity(250.408, "kcal"),
				TotalEnergy:  quantity(279.822, "kcal"),
				AvgHeartRate: quantity(142.538, "count/min"),
				MaxHeartRate: quantity(171.045, "count/min"),
			},
		},
		{
			name: "basal energy in another unit",
			statistics: []string{
				statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="250" unit="kcal"`),
				statistic("HKQuantityTypeIdentifierBasalEnergyBurned", `sum="4.184" unit="kJ"`),
			},
			want: ingest.WorkoutTotals{ActiveEnergy: quantity(250, "kcal"), TotalEnergy: quantity(251, "kcal")},
		},
		{
			name: "active energy without basal energy",
			statistics: []string{
				statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="185.390" unit="kcal"`),
			},
			want: ingest.WorkoutTotals{ActiveEnergy: quantity(185.39, "kcal")},
		},
		{
			name: "basal energy without active energy",
			statistics: []string{
				statistic("HKQuantityTypeIdentifierBasalEnergyBurned", `sum="29.414" unit="kcal"`),
			},
		},
		{
			name: "heart rate without maximum",
			statistics: []string{
				statistic("HKQuantityTypeIdentifierHeartRate", `average="117.202" unit="count/min"`),
			},
			want: ingest.WorkoutTotals{AvgHeartRate: quantity(117.202, "count/min")},
		},
		{name: "without statistics"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := append(append([]string{run}, tt.statistics...), `</Workout>`)

			w := workoutOf(t, readAll(t, document(body...)))
			if tt.want.TotalEnergy != nil {
				require.NotNil(t, w.Totals.TotalEnergy)
				require.InDelta(t, tt.want.TotalEnergy.Value, w.Totals.TotalEnergy.Value, 1e-9)
				w.Totals.TotalEnergy.Value = tt.want.TotalEnergy.Value
			}

			require.Equal(t, tt.want, w.Totals)
		})
	}
}

// Given a workout without such statistics and with totalDistance and totalEnergyBurned and their
// units, as older exports write, then they give the distance and the active energy. Given both,
// then the statistics win.
func TestRecords_WorkoutTotalsOfOlderExports(t *testing.T) {
	t.Parallel()

	older := `<Workout workoutActivityType="HKWorkoutActivityTypeCycling" duration="45" durationUnit="min" totalDistance="18.2" totalDistanceUnit="km" totalEnergyBurned="410" totalEnergyBurnedUnit="kcal" sourceName="iPhone" startDate="2019-06-01 18:00:00 +0200" endDate="2019-06-01 18:45:00 +0200">`

	w := workoutOf(t, readAll(t, document(older, `</Workout>`)))
	require.Equal(t, ingest.WorkoutTotals{
		Distance:     &ingest.Quantity{Value: 18.2, Unit: "km"},
		ActiveEnergy: &ingest.Quantity{Value: 410, Unit: "kcal"},
	}, w.Totals)

	w = workoutOf(t, readAll(t, document(
		older,
		statistic("HKQuantityTypeIdentifierDistanceCycling", `sum="18.4" unit="km"`),
		statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="412" unit="kcal"`),
		`</Workout>`,
	)))
	require.Equal(t, &ingest.Quantity{Value: 18.4, Unit: "km"}, w.Totals.Distance)
	require.Equal(t, &ingest.Quantity{Value: 412, Unit: "kcal"}, w.Totals.ActiveEnergy)
}

// Given a statistic or an older total that is not a number, then the workout is invalid, with a
// reason that names the attribute and never its value.
func TestRecords_InvalidWorkoutTotals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		workout []string
		reason  string
	}{
		{
			name:    "sum of a statistic",
			workout: []string{run, statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="lots" unit="kcal"`)},
			reason:  "WorkoutStatistics sum is not a number",
		},
		{
			name:    "maximum of the heart rate",
			workout: []string{run, statistic("HKQuantityTypeIdentifierHeartRate", `average="140" maximum="high" unit="count/min"`)},
			reason:  "WorkoutStatistics maximum is not a number",
		},
		{
			name:    "total distance",
			workout: []string{`<Workout workoutActivityType="HKWorkoutActivityTypeCycling" totalDistance="far" totalDistanceUnit="km" sourceName="iPhone" startDate="2019-06-01 18:00:00 +0200" endDate="2019-06-01 18:45:00 +0200">`},
			reason:  "totalDistance is not a number",
		},
		{
			name: "basal energy that cannot be added to the active energy",
			workout: []string{
				run,
				statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="250" unit="kcal"`),
				statistic("HKQuantityTypeIdentifierBasalEnergyBurned", `sum="30" unit="km"`),
			},
			reason: "WorkoutStatistics unit of HKQuantityTypeIdentifierBasalEnergyBurned is not a unit of energy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			records := readAll(t, document(append(tt.workout, `</Workout>`)...))
			require.Len(t, records, 1)
			require.Equal(t, &ingest.Invalid{Of: ingest.WorkoutRecord, Reason: tt.reason}, records[0].Invalid)
		})
	}
}

// Given the metadata HKElevationAscended and HKElevationDescended (7092 cm), then they give the
// elevations, and HKIndoorWorkout 1 or 0 the indoor flag, which is unknown otherwise. These
// entries also stay in the metadata.
func TestRecords_WorkoutElevationsAndIndoorFlag(t *testing.T) {
	t.Parallel()

	indoor, outdoor := true, false

	tests := []struct {
		name      string
		entries   []string
		ascended  *ingest.Quantity
		descended *ingest.Quantity
		indoor    *bool
		metadata  string
	}{
		{
			name: "elevations outdoor",
			entries: []string{
				` <MetadataEntry key="HKIndoorWorkout" value="0"/>`,
				` <MetadataEntry key="HKElevationAscended" value="7092 cm"/>`,
				` <MetadataEntry key="HKElevationDescended" value="6810.5 cm"/>`,
			},
			ascended:  &ingest.Quantity{Value: 7092, Unit: "cm"},
			descended: &ingest.Quantity{Value: 6810.5, Unit: "cm"},
			indoor:    &outdoor,
			metadata:  `{"HKIndoorWorkout": "0", "HKElevationAscended": "7092 cm", "HKElevationDescended": "6810.5 cm"}`,
		},
		{
			name:     "indoor",
			entries:  []string{` <MetadataEntry key="HKIndoorWorkout" value="1"/>`},
			indoor:   &indoor,
			metadata: `{"HKIndoorWorkout": "1"}`,
		},
		{
			name:     "indoor flag of another value",
			entries:  []string{` <MetadataEntry key="HKIndoorWorkout" value="yes"/>`},
			metadata: `{"HKIndoorWorkout": "yes"}`,
		},
		{name: "without entries"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := append(append([]string{run}, tt.entries...), `</Workout>`)

			w := workoutOf(t, readAll(t, document(body...)))
			require.Equal(t, tt.ascended, w.Totals.ElevationAscended)
			require.Equal(t, tt.descended, w.Totals.ElevationDescended)
			require.Equal(t, tt.indoor, w.Indoor)

			if tt.metadata == "" {
				require.Nil(t, w.Metadata)
			} else {
				require.JSONEq(t, tt.metadata, string(w.Metadata))
			}
		})
	}

	t.Run("elevation that is not a quantity", func(t *testing.T) {
		t.Parallel()

		for _, value := range []string{"high", "7092", "cm 7092", "7092 cm cm"} {
			records := readAll(t, document(run, ` <MetadataEntry key="HKElevationDescended" value="`+value+`"/>`, `</Workout>`))
			require.Equal(t, []ingest.Record{{
				Position: records[0].Position,
				Invalid:  &ingest.Invalid{Of: ingest.WorkoutRecord, Reason: "HKElevationDescended is not a quantity"},
			}}, records, value)
		}
	})
}

// Given a workout, then its metadata holds its MetadataEntry items as received, under
// WorkoutStatistics the array of its statistics, totals included, and under WorkoutZoneGroup the
// array of its zone groups, each an object of its attributes as received, a zone group holding its
// WorkoutZone items under zones.
func TestRecords_WorkoutMetadata(t *testing.T) {
	t.Parallel()

	w := workoutOf(t, readAll(t, document(
		run,
		` <MetadataEntry key="HKIndoorWorkout" value="0"/>`,
		` <MetadataEntry key="HKWeatherTemperature" value="61.8510 degF"/>`,
		` <MetadataEntry key="HKAverageMETs" value="10.70187 kcal/hr·kg"/>`,
		` <MetadataEntry key="Note" value="Côte &amp; &lt;pluie&gt;"/>`,
		statistic("HKQuantityTypeIdentifierActiveEnergyBurned", `sum="250.408" unit="kcal"`),
		statistic("HKQuantityTypeIdentifierRunningPower", `average="222.863" minimum="178.290" maximum="267.436" unit="W"`),
		` <WorkoutZoneGroup type="HKQuantityTypeIdentifierHeartRate" source="system" unit="count/min">`,
		`  <WorkoutZone maximum="120" duration="2.5000" durationUnit="min"/>`,
		`  <WorkoutZone minimum="120" maximum="135" duration="5.0000" durationUnit="min"/>`,
		`  <WorkoutZone minimum="165" duration="2.5000" durationUnit="min"/>`,
		` </WorkoutZoneGroup>`,
		` <WorkoutZoneGroup type="HKQuantityTypeIdentifierRunningPower" source="user" unit="W"/>`,
		`</Workout>`,
	)))

	require.JSONEq(t, `{
		"HKIndoorWorkout": "0",
		"HKWeatherTemperature": "61.8510 degF",
		"HKAverageMETs": "10.70187 kcal/hr·kg",
		"Note": "Côte & <pluie>",
		"WorkoutStatistics": [
			{"type": "HKQuantityTypeIdentifierActiveEnergyBurned", "startDate": "2025-12-30 07:00:00 +0100", "endDate": "2025-12-30 07:25:00 +0100", "sum": "250.408", "unit": "kcal"},
			{"type": "HKQuantityTypeIdentifierRunningPower", "startDate": "2025-12-30 07:00:00 +0100", "endDate": "2025-12-30 07:25:00 +0100", "average": "222.863", "minimum": "178.290", "maximum": "267.436", "unit": "W"}
		],
		"WorkoutZoneGroup": [
			{"type": "HKQuantityTypeIdentifierHeartRate", "source": "system", "unit": "count/min", "zones": [
				{"maximum": "120", "duration": "2.5000", "durationUnit": "min"},
				{"minimum": "120", "maximum": "135", "duration": "5.0000", "durationUnit": "min"},
				{"minimum": "165", "duration": "2.5000", "durationUnit": "min"}
			]},
			{"type": "HKQuantityTypeIdentifierRunningPower", "source": "user", "unit": "W", "zones": []}
		]
	}`, string(w.Metadata))
	require.Contains(t, string(w.Metadata), "Côte & <pluie>", "non-ASCII and reserved characters are kept as received")

	t.Run("statistics without entries", func(t *testing.T) {
		t.Parallel()

		w := workoutOf(t, readAll(t, document(run, statistic("HKQuantityTypeIdentifierStepCount", `sum="4171" unit="count"`), `</Workout>`)))
		require.JSONEq(t, `{"WorkoutStatistics": [
			{"type": "HKQuantityTypeIdentifierStepCount", "startDate": "2025-12-30 07:00:00 +0100", "endDate": "2025-12-30 07:25:00 +0100", "sum": "4171", "unit": "count"}
		]}`, string(w.Metadata))
	})
}

// Given a WorkoutRoute, then it is skipped until #268: its metadata and its file reference are
// none of the workout.
func TestRecords_WorkoutRouteIsSkipped(t *testing.T) {
	t.Parallel()

	doc := document(
		run,
		` <MetadataEntry key="HKIndoorWorkout" value="0"/>`,
		` <WorkoutRoute sourceName="Apple Watch de Alex" sourceVersion="26.0" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">`,
		`  <MetadataEntry key="HKMetadataKeySyncIdentifier" value="3F915F69-55AC-ABEF-337E-D33B479CA34D"/>`,
		`  <FileReference path="/workout-routes/route_2025-12-30_7.00am.gpx"/>`,
		` </WorkoutRoute>`,
		` `+effort,
		`</Workout>`,
	)

	records := readAll(t, doc)
	require.Len(t, records, 2)
	require.NotNil(t, records[0].Quantity)

	w := workoutOf(t, records)
	require.Nil(t, w.Route)
	require.JSONEq(t, `{"HKIndoorWorkout": "0"}`, string(w.Metadata))
}
