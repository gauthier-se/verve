package synthexport

import (
	"context"
	"math"
	"math/rand/v2"
	"strconv"
	"time"
)

const (
	activityRunning  = "HKWorkoutActivityTypeRunning"
	activityWalking  = "HKWorkoutActivityTypeWalking"
	activityStrength = "HKWorkoutActivityTypeTraditionalStrengthTraining"

	typeEffortScore = "HKQuantityTypeIdentifierEstimatedWorkoutEffortScore"

	// loopRadius is the size of a route, in degrees: about a kilometer around latitude 0, longitude
	// 0, a place in the ocean that nobody mistakes for a real run, where coordinates change sign.
	loopRadius = 0.01
)

var (
	sourceStrava = source{name: "Strava", version: "2400000"}
	sourceLifts  = source{name: "Liftly", version: "21"}
)

// workout is a Workout element, derived from its day alone, since export.xml reads it again for the
// top level record of its effort score, and the zip for its route.
type workout struct {
	activity   string
	source     source
	device     string
	start, end time.Time
	metadata   []metadata
	statistics []statistic
	events     bool
	zones      bool
	route      bool
	effort     record // none when empty
}

// statistic is a WorkoutStatistics element: a sum when perMinute is set, or else an average around
// the value of average, with its minimum and maximum.
type statistic struct {
	typ, unit          string
	perMinute, average float64
}

func strengthStatistics() []statistic {
	return []statistic{
		{typ: "HKQuantityTypeIdentifierActiveEnergyBurned", unit: "kcal", perMinute: 6},
		{typ: "HKQuantityTypeIdentifierBasalEnergyBurned", unit: "kcal", perMinute: 1.2},
		{typ: typeHeartRate, unit: "count/min", average: 115},
	}
}

func walkStatistics() []statistic {
	return append(strengthStatistics(),
		statistic{typ: "HKQuantityTypeIdentifierDistanceWalkingRunning", unit: "km", perMinute: 0.09})
}

func runStatistics() []statistic {
	return []statistic{
		{typ: "HKQuantityTypeIdentifierActiveEnergyBurned", unit: "kcal", perMinute: 11},
		{typ: "HKQuantityTypeIdentifierBasalEnergyBurned", unit: "kcal", perMinute: 1.2},
		{typ: "HKQuantityTypeIdentifierDistanceWalkingRunning", unit: "km", perMinute: 0.18},
		{typ: "HKQuantityTypeIdentifierStepCount", unit: "count", perMinute: 165},
		{typ: typeHeartRate, unit: "count/min", average: 150},
		{typ: "HKQuantityTypeIdentifierRunningPower", unit: "W", average: 230},
	}
}

// workoutsOn returns the workouts of a day, on a weekly schedule: a run, and every other week its
// Strava copy, a strength session, a walk, then a session logged in an app. The seed changes none of
// their dates, which name the routes and set the number of their points.
func (e exporter) workoutsOn(day int) []workout {
	const (
		runDay, strengthDay, walkDay, appDay = 0, 2, 4, 5
		daysPerWeek, copyEvery               = 7, 14
	)

	r := e.rng(streamWorkout, day)
	start := e.dayStart(day).Add(7*time.Hour + time.Duration(day*37%120)*time.Minute)
	end := start.Add(25*time.Minute + time.Duration(day*411%1800)*time.Second)
	base := workout{start: start, end: end, source: sourceWatch, device: e.deviceOf(sourceWatch, day, r)}

	outdoor := func(activity string, statistics []statistic) workout {
		w := base
		w.activity, w.statistics = activity, statistics
		w.metadata = watchMetadata(r, false)
		w.events, w.zones, w.route = true, true, true
		w.effort = w.effortScore(r)

		return w
	}

	switch day % daysPerWeek {
	case runDay:
		run := outdoor(activityRunning, runStatistics())

		if day%copyEvery != 0 {
			return []workout{run}
		}

		return []workout{run, {
			activity: activityRunning, source: sourceStrava, start: start.Add(5 * time.Second), end: end.Add(-5 * time.Second),
			metadata: []metadata{
				{"HKExternalUUID", "strava://activities/" + strconv.FormatUint(10_000_000_000+r.Uint64N(1_000_000_000), 10)},
				{"HKIndoorWorkout", "0"},
			},
		}}
	case strengthDay:
		strength := base
		strength.activity, strength.statistics = activityStrength, strengthStatistics()
		strength.metadata = watchMetadata(r, true)
		strength.zones = true

		return []workout{strength}
	case walkDay:
		return []workout{outdoor(activityWalking, walkStatistics())}
	case appDay:
		return []workout{{
			activity: activityStrength, source: sourceLifts, start: start, end: end,
			statistics: strengthStatistics()[:1],
			metadata:   []metadata{{"HKIndoorWorkout", "1"}, {"LIFTLY_WORKOUT_ID", uuid(r)}},
		}}
	}

	return nil
}

// watchMetadata returns what the watch records about a workout, with units inside the values.
func watchMetadata(r *rand.Rand, indoor bool) []metadata {
	meta := []metadata{
		{"HKIndoorWorkout", "1"},
		{"HKTimeZone", "Europe/Paris"},
		{"HKWeatherHumidity", strconv.Itoa(4000+r.IntN(5000)) + " %"},
		{"HKWeatherTemperature", fixed(40+r.Float64()*40, 4) + " degF"},
		{"HKAverageMETs", fixed(4+r.Float64()*8, 5) + " kcal/hr·kg"},
	}

	if !indoor {
		meta[0].value = "0"
		meta = append(meta, metadata{"HKElevationAscended", strconv.Itoa(1000+r.IntN(9000)) + " cm"})
	}

	return meta
}

func (w workout) effortScore(r *rand.Rand) record {
	return record{
		typ: typeEffortScore, unit: "appleEffortScore", value: integer(2, 7)(r, 0), source: w.source, device: w.device,
		start: w.start, end: w.end,
	}
}

// writeEffortScores writes at top level the records that workouts hold.
func (e exporter) writeEffortScores(day int, d document) {
	for _, w := range e.workoutsOn(day) {
		if w.effort.typ != "" {
			e.writeRecord(d, 1, w.effort)
		}
	}
}

func (e exporter) writeWorkouts(day int, d document) {
	r := e.rng(streamWorkoutDetail, day)

	for _, w := range e.workoutsOn(day) {
		d.open(1, "Workout",
			"workoutActivityType", w.activity,
			"duration", strconv.FormatFloat(w.minutes(), 'f', -1, 64),
			"durationUnit", "min",
			"sourceName", w.source.name,
			"sourceVersion", w.source.version,
			"device", w.device,
			"creationDate", e.date(w.end),
			"startDate", e.date(w.start),
			"endDate", e.date(w.end))
		writeMetadata(d, 2, w.metadata)

		if w.events {
			e.writeEvents(d, w)
		}

		for _, s := range w.statistics {
			e.writeStatistic(d, w, s, r)
		}

		if w.zones {
			writeZones(d, w)
		}

		if w.route {
			e.writeRouteReference(d, w, r)
		}

		if w.effort.typ != "" {
			e.writeRecord(d, 2, w.effort)
		}

		d.closeTag(1, "Workout")
	}
}

func (w workout) minutes() float64 {
	return w.end.Sub(w.start).Minutes()
}

// writeEvents writes a pause in the middle of the workout, with what ends it, then a segment every
// ten minutes. Only segments have a duration.
func (e exporter) writeEvents(d document, w workout) {
	const segment = 10 * time.Minute

	middle := w.start.Add(w.end.Sub(w.start) / 2)

	d.empty(2, "WorkoutEvent", "type", "HKWorkoutEventTypePause", "date", e.date(middle))
	d.empty(2, "WorkoutEvent", "type", "HKWorkoutEventTypeResume", "date", e.date(middle.Add(time.Minute)))

	for at := w.start; at.Add(segment).Before(w.end); at = at.Add(segment) {
		d.empty(2, "WorkoutEvent",
			"type", "HKWorkoutEventTypeSegment",
			"date", e.date(at),
			"duration", fixed(segment.Minutes(), 4),
			"durationUnit", "min")
	}
}

func (e exporter) writeStatistic(d document, w workout, s statistic, r *rand.Rand) {
	const decimals = 3

	if s.perMinute > 0 {
		d.empty(2, "WorkoutStatistics",
			"type", s.typ,
			"startDate", e.date(w.start),
			"endDate", e.date(w.end),
			"sum", fixed(s.perMinute*w.minutes()*(0.9+0.2*r.Float64()), decimals),
			"unit", s.unit)

		return
	}

	average := s.average * (0.95 + 0.1*r.Float64())

	d.empty(2, "WorkoutStatistics",
		"type", s.typ,
		"startDate", e.date(w.start),
		"endDate", e.date(w.end),
		"average", fixed(average, decimals),
		"minimum", fixed(average*0.8, decimals),
		"maximum", fixed(average*1.2, decimals),
		"unit", s.unit)
}

// writeZones writes the five heart rate zones: the first one has no minimum, the last one no maximum.
func writeZones(d document, w workout) {
	zones := []struct {
		minimum, maximum string
		share            float64
	}{
		{"", "120", 0.1}, {"120", "135", 0.2}, {"135", "150", 0.4}, {"150", "165", 0.2}, {"165", "", 0.1},
	}

	d.open(2, "WorkoutZoneGroup", "type", typeHeartRate, "source", "system", "unit", "count/min")

	for _, z := range zones {
		d.empty(3, "WorkoutZone",
			"minimum", z.minimum,
			"maximum", z.maximum,
			"duration", fixed(w.minutes()*z.share, 4),
			"durationUnit", "min")
	}

	d.closeTag(2, "WorkoutZoneGroup")
}

// writeRouteReference writes the link to the GPX entry. Only a walk names the device of its route,
// as only a few routes do in a real export.
func (e exporter) writeRouteReference(d document, w workout, r *rand.Rand) {
	device := ""
	if w.activity == activityWalking {
		device = w.device
	}

	d.open(2, "WorkoutRoute",
		"sourceName", w.source.name,
		"sourceVersion", w.source.version,
		"device", device,
		"creationDate", e.date(w.end),
		"startDate", e.date(w.start),
		"endDate", e.date(w.end))
	writeMetadata(d, 3, []metadata{{"HKMetadataKeySyncIdentifier", uuid(r)}, {"HKMetadataKeySyncVersion", "1"}})
	d.empty(3, "FileReference", "path", "/workout-routes/"+w.routeFile())
	d.closeTag(2, "WorkoutRoute")
}

// routeFile names the route of a workout, in the offset of the export.
func (w workout) routeFile() string {
	return "route_" + w.start.Format("2006-01-02_3.04pm") + ".gpx"
}

// routeEntry returns the GPX entry of a workout: one loop around latitude 0, longitude 0, with
// Density points per minute, and at most one per second since dates have no fraction.
func (e exporter) routeEntry(day int, w workout) entry {
	return func(_ context.Context, d document) error {
		r := e.rng(streamRoute, day)
		step := max(time.Minute/time.Duration(e.opts.Density), time.Second)
		points := int(w.end.Sub(w.start) / step)

		d.write(`<?xml version="1.0" encoding="UTF-8"?>`, "\n",
			`<gpx version="1.1" creator="Apple Health Export" xmlns="http://www.topografix.com/GPX/1/1" `,
			`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" `,
			`xsi:schemaLocation="http://www.topografix.com/GPX/1/1 http://www.topografix.com/GPX/1/1/gpx.xsd">`, "\n",
			"  <metadata>\n    <time>", gpxTime(w.start), "</time>\n  </metadata>\n",
			"  <trk>\n    <name>Route ", w.start.Format("2006-01-02 3:04pm"), "</name>\n    <trkseg>\n")

		for i := range points {
			angle := 2 * math.Pi * float64(i) / float64(points)

			d.write(`      <trkpt lon="`, fixed(loopRadius*math.Cos(angle), 6), `" lat="`, fixed(loopRadius*math.Sin(angle), 6),
				`"><ele>`, fixed(100+20*math.Sin(angle), 6), "</ele><time>", gpxTime(w.start.Add(time.Duration(i)*step)),
				"</time><extensions><speed>", fixed(2.5+r.Float64(), 6), "</speed><course>", fixed(math.Mod(angle*180/math.Pi+90, 360), 6),
				"</course><hAcc>", fixed(3+r.Float64()*9, 6), "</hAcc><vAcc>", fixed(2+r.Float64()*4, 6),
				"</vAcc></extensions></trkpt>\n")
		}

		d.write("    </trkseg>\n  </trk>\n</gpx>\n")

		return nil
	}
}

func gpxTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
