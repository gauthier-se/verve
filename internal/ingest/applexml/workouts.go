package applexml

import (
	"encoding/xml"
	"math"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/ingest"
)

// workoutElement is what a Workout element holds, read from its start tag to its end tag.
type workoutElement struct {
	attrs  attributes
	events []attributes
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
		case "WorkoutEvent":
			w.events = append(w.events, child.Attr)
		}
	}

	record := w.record()
	record.Position = position

	if !t.implicit {
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

	return ingest.Record{Workout: workout}
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

// invalidWorkout returns an invalid workout record, with reason.
func invalidWorkout(reason string) ingest.Record {
	return ingest.Record{Invalid: &ingest.Invalid{Of: ingest.WorkoutRecord, Reason: reason}}
}
