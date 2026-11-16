package applexml_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
)

// routed returns a run of the watch, from 06:00:00Z to 06:25:00Z, whose WorkoutRoute refers to
// the GPX file at path.
func routed(path string) []string {
	return []string{
		run,
		` <WorkoutRoute sourceName="Apple Watch de Alex" sourceVersion="26.0" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:25:00 +0100">`,
		`  <MetadataEntry key="HKMetadataKeySyncIdentifier" value="3F915F69-55AC-ABEF-337E-D33B479CA34D"/>`,
		`  <FileReference path="` + path + `"/>`,
		` </WorkoutRoute>`,
		`</Workout>`,
	}
}

// routePath is the path of the FileReference of the tests, from the folder of export.xml.
const routePath = "/workout-routes/route_2025-12-30_7.00am.gpx"

// gpx returns a GPX file of the points of the Health app.
func gpx(points ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="Apple Health Export" xmlns="http://www.topografix.com/GPX/1/1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.topografix.com/GPX/1/1 http://www.topografix.com/GPX/1/1/gpx.xsd">
 <metadata><time>2025-12-30T06:00:00Z</time></metadata>
 <trk><name>Route 2025-12-30 7:00am</name><trkseg>
` + strings.Join(points, "\n") + `
 </trkseg></trk>
</gpx>
`
}

// trkpt returns a point of a GPX file at time, with every field of the Health app.
func trkpt(time string) string {
	return fmt.Sprintf(`<trkpt lon="2.351400" lat="48.857000"><ele>35.500000</ele><time>%s</time>`+
		`<extensions><speed>2.710541</speed><course>90.000000</course><hAcc>9.084130</hAcc><vAcc>3.566704</vAcc></extensions></trkpt>`, time)
}

// point returns the route point of trkpt(time).
func point(time string) ingest.RoutePoint {
	return ingest.RoutePoint{
		Time: time, Latitude: 48.857, Longitude: 2.3514, Altitude: new(35.5), Speed: new(2.710541),
		Course: new(90.0), HorizontalAccuracy: new(9.08413), VerticalAccuracy: new(3.566704),
	}
}

// routedZip returns a zip of an export.xml in folder holding the workouts of body, and of the GPX
// files of routes, by their name in the zip.
func routedZip(t *testing.T, folder string, body []string, routes map[string]string) []byte {
	t.Helper()

	entries := []entry{{name: folder + "export.xml", body: document(body...)}}

	for name, route := range routes {
		entries = append(entries, entry{name: name, body: route})
	}

	return zipOf(t, entries...)
}

// workoutRecords returns the workout records of the export of data, which must read without error.
func workoutRecords(t *testing.T, data []byte) []ingest.Record {
	t.Helper()

	e, err := open(t, data)
	require.NoError(t, err)

	records, err := recordsOf(t, e)
	require.NoError(t, err)

	var workouts []ingest.Record

	for _, r := range records {
		if r.Workout != nil {
			workouts = append(workouts, r)
		}
	}

	return workouts
}

// Given a workout whose WorkoutRoute has a FileReference, then its path resolves against the
// folder of export.xml, and the workout record holds the points of that GPX entry in time order,
// with latitude, longitude, ele, time, speed, course, hAcc and vAcc.
func TestRecords_WorkoutRouteHoldsThePointsOfItsGPXEntry(t *testing.T) {
	t.Parallel()

	route := gpx(trkpt("2025-12-30T06:00:00Z"), trkpt("2025-12-30T06:02:00Z"), trkpt("2025-12-30T06:01:00Z"))
	want := []ingest.RoutePoint{point("2025-12-30T06:00:00Z"), point("2025-12-30T06:01:00Z"), point("2025-12-30T06:02:00Z")}

	for name, folder := range map[string]string{"in a folder": "apple_health_export/", "at the root": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			workouts := workoutRecords(t, routedZip(t, folder, routed(routePath), map[string]string{
				folder + strings.TrimPrefix(routePath, "/"): route,
			}))
			require.Len(t, workouts, 1)
			require.Equal(t, want, workouts[0].Workout.Route)
			require.Empty(t, workouts[0].Warning)
			require.Nil(t, workouts[0].Workout.Metadata, "the metadata of the WorkoutRoute is none of the workout")
		})
	}

	t.Run("optional fields left out", func(t *testing.T) {
		t.Parallel()

		workouts := workoutRecords(t, routedZip(t, "apple_health_export/", routed(routePath), map[string]string{
			"apple_health_export" + routePath: gpx(`<trkpt lon="-0.5" lat="-1.25"><time>2025-12-30T06:00:00Z</time></trkpt>`),
		}))
		require.Equal(t, []ingest.RoutePoint{{Time: "2025-12-30T06:00:00Z", Latitude: -1.25, Longitude: -0.5}}, workouts[0].Workout.Route)
	})

	t.Run("workout without route", func(t *testing.T) {
		t.Parallel()

		workouts := workoutRecords(t, routedZip(t, "apple_health_export/", []string{run, `</Workout>`}, nil))
		require.Nil(t, workouts[0].Workout.Route)
		require.Empty(t, workouts[0].Warning)
	})
}

// Given route points outside the time range of the workout, then they are dropped, and the workout
// carries a warning naming how many.
func TestRecords_RoutePointsOutsideTheWorkoutAreDropped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		points  []string
		want    []ingest.RoutePoint
		warning string
	}{
		{
			name: "before and after",
			points: []string{
				trkpt("2025-12-30T05:59:59Z"), trkpt("2025-12-30T06:00:00Z"),
				trkpt("2025-12-30T06:25:00Z"), trkpt("2025-12-30T06:25:01Z"),
			},
			want:    []ingest.RoutePoint{point("2025-12-30T06:00:00Z"), point("2025-12-30T06:25:00Z")},
			warning: "2 route points outside the workout are dropped",
		},
		{
			name:    "one point",
			points:  []string{trkpt("2025-12-30T06:10:00Z"), trkpt("2025-12-30T07:00:00+01:00"), trkpt("2025-12-30T07:30:00Z")},
			want:    []ingest.RoutePoint{point("2025-12-30T07:00:00+01:00"), point("2025-12-30T06:10:00Z")},
			warning: "1 route point outside the workout is dropped",
		},
		{
			name:    "every point",
			points:  []string{trkpt("2025-12-29T06:00:00Z")},
			want:    nil,
			warning: "1 route point outside the workout is dropped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workouts := workoutRecords(t, routedZip(t, "apple_health_export/", routed(routePath), map[string]string{
				"apple_health_export" + routePath: gpx(tt.points...),
			}))
			require.Equal(t, tt.want, workouts[0].Workout.Route)
			require.Equal(t, tt.warning, workouts[0].Warning)
		})
	}
}

// Given a workout whose GPX entry is missing from the zip, then it has no route and carries a
// warning. Given a bare export.xml with routes, then each such workout carries a warning, and the
// first one also the message of the report.
func TestRecords_RouteThatIsNotInTheExport(t *testing.T) {
	t.Parallel()

	t.Run("missing GPX entry", func(t *testing.T) {
		t.Parallel()

		workouts := workoutRecords(t, routedZip(t, "apple_health_export/", routed(routePath), map[string]string{
			strings.TrimPrefix(routePath, "/"):              gpx(trkpt("2025-12-30T06:00:00Z")),
			"apple_health_export/workout-routes/other.gpx": gpx(trkpt("2025-12-30T06:00:00Z")),
		}))
		require.Nil(t, workouts[0].Workout.Route, "the path resolves against the folder of export.xml only")
		require.Equal(t, "route file is missing", workouts[0].Warning)
	})

	t.Run("path out of the zip", func(t *testing.T) {
		t.Parallel()

		workouts := workoutRecords(t, routedZip(t, "apple_health_export/", routed("/../../route.gpx"), nil))
		require.Nil(t, workouts[0].Workout.Route)
		require.Equal(t, "route file is missing", workouts[0].Warning)
	})

	bare := document(append(append(routed(routePath), routed(routePath)...), run, `</Workout>`)...)
	wantWarnings := []string{"route is not available; routes are not available in a bare export.xml", "route is not available", ""}

	t.Run("bare export.xml", func(t *testing.T) {
		t.Parallel()

		e, err := open(t, []byte(bare))
		require.NoError(t, err)

		records, err := recordsOf(t, e)
		require.NoError(t, err)

		var warnings []string

		for _, r := range records {
			require.Nil(t, r.Workout.Route)
			warnings = append(warnings, r.Warning)
		}

		require.Equal(t, wantWarnings, warnings)
	})

	t.Run("document", func(t *testing.T) {
		t.Parallel()

		var warnings []string

		for _, r := range readAll(t, bare) {
			warnings = append(warnings, r.Warning)
		}

		require.Equal(t, wantWarnings, warnings, "Records reads a bare export.xml")
	})
}

// Given a GPX entry that is not well-formed XML, whose point is not readable or that is
// corrupted, then the read ends with applexml.ErrMalformed.
func TestRecords_MalformedGPXEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route string
	}{
		{name: "truncated", route: gpx(trkpt("2025-12-30T06:00:00Z"))[:300]},
		{name: "latitude not a number", route: gpx(`<trkpt lon="2.3" lat="north"><time>2025-12-30T06:00:00Z</time></trkpt>`)},
		{name: "longitude missing", route: gpx(`<trkpt lat="48.8"><time>2025-12-30T06:00:00Z</time></trkpt>`)},
		{name: "time not a date", route: gpx(`<trkpt lon="2.3" lat="48.8"><time>yesterday</time></trkpt>`)},
		{name: "time missing", route: gpx(`<trkpt lon="2.3" lat="48.8"><ele>35</ele></trkpt>`)},
		{name: "speed not a number", route: gpx(strings.Replace(trkpt("2025-12-30T06:00:00Z"), "2.710541", "fast", 1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, err := open(t, routedZip(t, "apple_health_export/", routed(routePath), map[string]string{
				"apple_health_export" + routePath: tt.route,
			}))
			require.NoError(t, err)

			_, err = recordsOf(t, e)
			require.ErrorIs(t, err, applexml.ErrMalformed)
		})
	}

	t.Run("corrupted entry", func(t *testing.T) {
		t.Parallel()

		name := "apple_health_export" + routePath
		data := routedZip(t, "apple_health_export/", routed(routePath), map[string]string{
			name: gpx(trkpt("2025-12-30T06:00:00Z"), trkpt("2025-12-30T06:01:00Z"), trkpt("2025-12-30T06:02:00Z")),
		})

		e, err := open(t, corrupt(data, dataOffset(t, data, name)+20, 16))
		require.NoError(t, err)

		_, err = recordsOf(t, e)
		require.ErrorIs(t, err, applexml.ErrMalformed)
	})
}
