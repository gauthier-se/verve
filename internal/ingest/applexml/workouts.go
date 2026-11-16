package applexml

import (
	"encoding/xml"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/ingest"
)

// workoutElement is what a Workout element holds, read from its start tag to its end tag.
type workoutElement struct {
	attrs attributes
	// entries are its MetadataEntry items, in document order.
	entries    []metadataEntry
	events     []attributes
	statistics []attributes
	zoneGroups []zoneGroup
}

// workout reads the children of the Workout element of start, whose start tag was read last. It
// yields its inner records as top-level records as they come, then its workout at the position of
// its end tag (FR-5 of XML import), or of its start tag when it is self-closing, since its end then
// takes no byte. It returns false once yield does.
func (t *tokenizer) workout(start xml.StartElement, yield func(ingest.Record) bool) (bool, error) {
	w := workoutElement{attrs: start.Attr}
	position, depth := t.start, t.depth()

	for {
		tok, err := t.next()
		if err != nil {
			return false, err
		}

		if t.depth() < depth {
			break
		}

		child, ok := tok.(xml.StartElement)
		if !ok || t.depth() != depth+1 {
			continue
		}

		switch child.Name.Local {
		case "Record":
			record, err := t.record(child)
			if err != nil {
				return false, err
			}

			if !yield(record) {
				return false, nil
			}
		case "MetadataEntry":
			w.entries = append(w.entries, newMetadataEntry(child.Attr))
		case "WorkoutEvent":
			w.events = append(w.events, child.Attr)
		case "WorkoutStatistics":
			w.statistics = append(w.statistics, child.Attr)
		case "WorkoutZoneGroup":
			zones, err := t.children("WorkoutZone")
			if err != nil {
				return false, err
			}

			w.zoneGroups = append(w.zoneGroups, zoneGroup{attrs: child.Attr, zones: zones})
		}
	}

	record := w.record()
	record.Position = position

	if !t.selfClosed {
		record.Position = t.start
	}

	return yield(record), nil
}

// record returns the workout of w, or an invalid workout record whose reason names the first
// attribute that is missing or invalid, never its value.
func (w workoutElement) record() ingest.Record {
	activity, _ := w.attrs.get("workoutActivityType")
	if activity == "" {
		return invalidWorkout("workoutActivityType is missing")
	}

	start, reason := date(w.attrs, "startDate")
	if reason != "" {
		return invalidWorkout(reason)
	}

	end, reason := date(w.attrs, "endDate")
	if reason != "" {
		return invalidWorkout(reason)
	}

	workout := &ingest.Workout{ActivityType: activity, Start: start, End: end, Source: source(w.attrs)}

	if workout.Duration, reason = quantity(w.attrs, "duration", "durationUnit"); reason != "" {
		return invalidWorkout(reason)
	}

	for _, e := range w.events {
		event, reason := workoutEvent(e)
		if reason != "" {
			return invalidWorkout("WorkoutEvent " + reason)
		}

		workout.Events = append(workout.Events, event)
	}

	if workout.Totals, reason = w.totals(); reason != "" {
		return invalidWorkout(reason)
	}

	switch indoor, _ := w.entry(metadataIndoor); indoor {
	case "1", "0":
		workout.Indoor = new(indoor == "1")
	}

	var m metadata

	for _, e := range w.entries {
		m.entry(e)
	}

	if len(w.statistics) > 0 {
		m.objects(metadataStatistics, w.statistics)
	}

	if len(w.zoneGroups) > 0 {
		m.zoneGroups(w.zoneGroups)
	}

	workout.Metadata = m.object()

	return ingest.Record{Workout: workout}
}

// The metadata keys and the statistic types that give the totals and the indoor flag of a workout
// (FR-6 of XML import).
const (
	metadataIndoor             = "HKIndoorWorkout"
	metadataElevationAscended  = "HKElevationAscended"
	metadataElevationDescended = "HKElevationDescended"
	statisticActiveEnergy      = "HKQuantityTypeIdentifierActiveEnergyBurned"
	statisticBasalEnergy       = "HKQuantityTypeIdentifierBasalEnergyBurned"
	statisticHeartRate         = "HKQuantityTypeIdentifierHeartRate"
	statisticDistance          = "HKQuantityTypeIdentifierDistance"
)

// entry returns the value of the first MetadataEntry of key, and false when w has none.
func (w workoutElement) entry(key string) (string, bool) {
	for _, e := range w.entries {
		if e.key == key {
			return e.value, true
		}
	}

	return "", false
}

// totals returns the totals of w: from the first statistic of each type, every type that starts
// with statisticDistance counting as one, then from the attributes totalDistance and
// totalEnergyBurned of older exports for a distance and an active energy without statistic, and
// from the elevation entries of its metadata. It returns the reason of an invalid workout
// otherwise.
func (w workoutElement) totals() (ingest.WorkoutTotals, string) {
	var (
		totals ingest.WorkoutTotals
		basal  *ingest.Quantity
		seen   []string
		reason string
	)

	for _, s := range w.statistics {
		typ, _ := s.get("type")
		if strings.HasPrefix(typ, statisticDistance) {
			typ = statisticDistance
		}

		if slices.Contains(seen, typ) {
			continue
		}

		seen = append(seen, typ)

		switch typ {
		case statisticActiveEnergy:
			totals.ActiveEnergy, reason = quantity(s, "sum", "unit")
		case statisticBasalEnergy:
			basal, reason = quantity(s, "sum", "unit")
		case statisticDistance:
			totals.Distance, reason = quantity(s, "sum", "unit")
		case statisticHeartRate:
			if totals.AvgHeartRate, reason = quantity(s, "average", "unit"); reason == "" {
				totals.MaxHeartRate, reason = quantity(s, "maximum", "unit")
			}
		}

		if reason != "" {
			return ingest.WorkoutTotals{}, "WorkoutStatistics " + reason
		}
	}

	if active := totals.ActiveEnergy; active != nil && basal != nil {
		value, err := catalog.Convert(basal.Value, basal.Unit, active.Unit)
		if err != nil {
			return ingest.WorkoutTotals{}, "WorkoutStatistics unit of " + statisticBasalEnergy + " is not a unit of energy"
		}

		totals.TotalEnergy = &ingest.Quantity{Value: active.Value + value, Unit: active.Unit}
	}

	if totals.Distance == nil {
		if totals.Distance, reason = quantity(w.attrs, "totalDistance", "totalDistanceUnit"); reason != "" {
			return ingest.WorkoutTotals{}, reason
		}
	}

	if totals.ActiveEnergy == nil {
		if totals.ActiveEnergy, reason = quantity(w.attrs, "totalEnergyBurned", "totalEnergyBurnedUnit"); reason != "" {
			return ingest.WorkoutTotals{}, reason
		}
	}

	if totals.ElevationAscended, reason = w.elevation(metadataElevationAscended); reason != "" {
		return ingest.WorkoutTotals{}, reason
	}

	if totals.ElevationDescended, reason = w.elevation(metadataElevationDescended); reason != "" {
		return ingest.WorkoutTotals{}, reason
	}

	return totals, ""
}

// elevation returns the quantity of the metadata entry key, a number and a unit such as 7092 cm,
// nil when w lacks it, or the reason of an invalid workout.
func (w workoutElement) elevation(key string) (*ingest.Quantity, string) {
	value, ok := w.entry(key)
	if !ok {
		return nil, ""
	}

	number, unit, _ := strings.Cut(value, " ")

	n, ok := parseNumber(number)
	if !ok || unit == "" || strings.Contains(unit, " ") {
		return nil, key + " is not a quantity"
	}

	return &ingest.Quantity{Value: n, Unit: unit}, ""
}

// quantity returns the number of the attribute name of a in the unit of the attribute unit, nil
// when a lacks it, or the reason of an invalid record.
func quantity(a attributes, name, unit string) (*ingest.Quantity, string) {
	value, ok := a.get(name)
	if !ok {
		return nil, ""
	}

	n, ok := parseNumber(value)
	if !ok {
		return nil, name + " is not a number"
	}

	u, _ := a.get(unit)

	return &ingest.Quantity{Value: n, Unit: u}, ""
}

// workoutEvent returns the event of the attributes of a WorkoutEvent element: from date to
// duration in durationUnit later, or at date without duration. It returns the reason of an invalid
// event otherwise.
func workoutEvent(a attributes) (ingest.WorkoutEvent, string) {
	typ, _ := a.get("type")

	start, reason := parseDate(a, "date")
	if reason != "" {
		return ingest.WorkoutEvent{}, reason
	}

	end := start

	d, reason := quantity(a, "duration", "durationUnit")
	if reason != "" {
		return ingest.WorkoutEvent{}, reason
	}

	if d != nil {
		seconds, err := catalog.Convert(d.Value, d.Unit, "s")
		if err != nil {
			return ingest.WorkoutEvent{}, "durationUnit is not a unit of time"
		}

		end = start.Add(time.Duration(math.Round(seconds * float64(time.Second))))
	}

	return ingest.WorkoutEvent{Type: typ, Start: start.Format(time.RFC3339Nano), End: end.Format(time.RFC3339Nano)}, ""
}

// zoneGroup is a WorkoutZoneGroup element: its attributes and those of its WorkoutZone items.
type zoneGroup struct {
	attrs attributes
	zones []attributes
}

// The metadata keys of the statistics and of the zone groups of a workout (FR-6 of XML import).
const (
	metadataStatistics = "WorkoutStatistics"
	metadataZoneGroups = "WorkoutZoneGroup"
)

// zoneGroups adds the zone groups of a workout under metadataZoneGroups, as an array of objects
// holding their attributes as received, and their zones under zones, as objects of their
// attributes.
func (m *metadata) zoneGroups(groups []zoneGroup) {
	if !m.key(metadataZoneGroups) {
		return
	}

	m.array(len(groups), func(i int) {
		g := groups[i]

		m.buf.WriteByte('{')
		m.members(g.attrs)

		if len(g.attrs) > 0 {
			m.buf.WriteByte(',')
		}

		m.string("zones")
		m.buf.WriteByte(':')
		m.array(len(g.zones), func(j int) { m.attributes(g.zones[j]) })
		m.buf.WriteByte('}')
	})
}

// invalidWorkout returns an invalid workout record, with reason.
func invalidWorkout(reason string) ingest.Record {
	return ingest.Record{Invalid: &ingest.Invalid{Of: ingest.WorkoutRecord, Reason: reason}}
}
