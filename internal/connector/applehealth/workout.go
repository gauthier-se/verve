package applehealth

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/units"
)

// routeRef is a pending GPX reference gathered while parsing a Workout: the
// enclosing WorkoutRoute's provenance plus the FileReference path to copy once
// the workout closes.
type routeRef struct {
	source string
	start  string
	end    string
	path   string
}

// workoutBuilder accumulates a single <Workout> subtree as the streaming parser
// walks it, so the whole workout — attributes, statistics, route references — is
// assembled before its Session (and routes) are written on the closing tag. It
// exists because a Session's fields are spread across child elements, unlike a
// scalar Record which is self-contained in its attributes.
type workoutBuilder struct {
	activityType string
	source       string
	start        string
	end          string
	duration     float64
	distance     *float64
	energy       *float64

	curRoute *routeRef // the WorkoutRoute currently open, awaiting its FileReference
	routes   []routeRef
}

// newWorkoutBuilder starts a builder from a <Workout> element's attributes,
// normalizing the activity type to a neutral slug, the times to RFC 3339 UTC,
// and the duration to seconds.
func newWorkoutBuilder(attrs []xml.Attr) *workoutBuilder {
	var activity, source, start, end, dur, durUnit string
	for _, a := range attrs {
		switch a.Name.Local {
		case "workoutActivityType":
			activity = a.Value
		case "sourceName":
			source = a.Value
		case "startDate":
			start = a.Value
		case "endDate":
			end = a.Value
		case "duration":
			dur = a.Value
		case "durationUnit":
			durUnit = a.Value
		}
	}
	return &workoutBuilder{
		activityType: normalizeActivityType(activity),
		source:       source,
		start:        normalizeTime(start),
		end:          normalizeTime(end),
		duration:     durationSeconds(dur, durUnit),
	}
}

// durationSeconds parses a workout duration and normalizes it to seconds. An
// unparseable or unconvertible value falls back to the raw number (Apple reports
// minutes) rather than failing the import.
func durationSeconds(value, unit string) float64 {
	raw, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	if unit == "" {
		return raw
	}
	secs, err := units.Convert(raw, unit, "s")
	if err != nil {
		return raw
	}
	return secs
}

// addStatistic folds one <WorkoutStatistics> into the workout's totals: a
// distance-type statistic sets total_distance (km), an active-energy statistic
// sets total_energy (kcal). Other statistics (heart rate, step count, basal
// energy…) are summary detail out of scope for this slice.
func (b *workoutBuilder) addStatistic(attrs []xml.Attr) {
	var typ, sum, unit string
	for _, a := range attrs {
		switch a.Name.Local {
		case "type":
			typ = a.Value
		case "sum":
			sum = a.Value
		case "unit":
			unit = a.Value
		}
	}
	if sum == "" {
		return
	}
	raw, err := strconv.ParseFloat(sum, 64)
	if err != nil {
		return
	}
	switch {
	case strings.HasPrefix(typ, "HKQuantityTypeIdentifierDistance"):
		if km, err := units.Convert(raw, unit, "km"); err == nil {
			b.distance = &km
		}
	case typ == "HKQuantityTypeIdentifierActiveEnergyBurned":
		if kcal, err := units.Convert(raw, unit, "kcal"); err == nil {
			b.energy = &kcal
		}
	}
}

// startRoute begins a <WorkoutRoute>: its provenance is captured now; its GPX
// path arrives on the nested <FileReference>.
func (b *workoutBuilder) startRoute(attrs []xml.Attr) {
	var source, start, end string
	for _, a := range attrs {
		switch a.Name.Local {
		case "sourceName":
			source = a.Value
		case "startDate":
			start = a.Value
		case "endDate":
			end = a.Value
		}
	}
	b.curRoute = &routeRef{source: source, start: normalizeTime(start), end: normalizeTime(end)}
}

// addFileRef attaches a <FileReference> path to the open route and queues it.
func (b *workoutBuilder) addFileRef(attrs []xml.Attr) {
	if b.curRoute == nil {
		return
	}
	for _, a := range attrs {
		if a.Name.Local == "path" {
			b.curRoute.path = a.Value
		}
	}
	if b.curRoute.path != "" {
		b.routes = append(b.routes, *b.curRoute)
	}
	b.curRoute = nil
}

// session materializes the accumulated workout as a data.Session for one owner.
func (b *workoutBuilder) session(accountID int64) data.Session {
	return data.Session{
		AccountID:     accountID,
		ActivityType:  b.activityType,
		StartAt:       b.start,
		EndAt:         b.end,
		Duration:      b.duration,
		TotalDistance: b.distance,
		TotalEnergy:   b.energy,
		Source:        b.source,
		ContentKey:    sessionContentKey(b.activityType, b.source, b.start, b.end),
	}
}
