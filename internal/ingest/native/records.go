package native

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// The records of FR-7 of Ingestion pipeline. A record with a field missing or of the wrong type
// becomes an ingest.Invalid record whose reason names the first such field, never its value. A
// member whose value is null counts as left out, and a member that the format does not define is
// ignored. Times, units and identifiers are kept as received, for the pipeline to check.

// decodeSample decodes a quantity or category sample.
func decodeSample(raw json.RawMessage) ingest.Record {
	o, ok := newObject(raw, "")
	if !ok {
		return invalidRecord(ingest.SampleRecord, "", "sample is not an object")
	}

	typ := o.str("type", true)
	uuid := o.uuid("uuid", false)
	start := o.str("start", true)
	end := o.str("end", true)

	quantity := o.has("value") || o.has("unit")
	category := o.has("categoryValue")

	switch {
	case quantity && category:
		o.fail("sample has both a value and a category value")
	case !quantity && !category:
		o.fail("sample has neither a value nor a category value")
	}

	var (
		value *float64
		unit  string
		cat   string
	)

	if quantity {
		value, unit = o.number("value", true), o.str("unit", true)
	} else {
		cat = o.str("categoryValue", true)
	}

	source := o.source()
	metadata := o.metadata()

	if *o.reason != "" {
		return invalidRecord(ingest.SampleRecord, typ, *o.reason)
	}

	if quantity {
		return ingest.Record{Quantity: &ingest.QuantitySample{
			UUID: uuid, Type: typ, Start: start, End: end, Value: *value, Unit: unit, Source: source, Metadata: metadata,
		}}
	}

	return ingest.Record{Category: &ingest.CategorySample{
		UUID: uuid, Type: typ, Start: start, End: end, Value: cat, Source: source, Metadata: metadata,
	}}
}

// decodeWorkout decodes a workout with its totals, events and route.
func decodeWorkout(raw json.RawMessage) ingest.Record {
	o, ok := newObject(raw, "")
	if !ok {
		return invalidRecord(ingest.WorkoutRecord, "", "workout is not an object")
	}

	w := ingest.Workout{
		UUID:         o.uuid("uuid", true),
		ActivityType: o.str("activityType", true),
		Start:        o.str("start", true),
		End:          o.str("end", true),
	}

	if duration := o.number("duration", true); duration != nil {
		w.Duration = &ingest.Quantity{Value: *duration, Unit: "s"}
	}

	if totals, ok := o.object("totals", false); ok {
		w.Totals = ingest.WorkoutTotals{
			Distance:           totals.quantity("distance"),
			ActiveEnergy:       totals.quantity("activeEnergy"),
			TotalEnergy:        totals.quantity("totalEnergy"),
			ElevationAscended:  totals.quantity("elevationAscended"),
			ElevationDescended: totals.quantity("elevationDescended"),
		}
	}

	w.Indoor = o.boolean("indoor")
	w.Source = o.source()
	w.Metadata = o.metadata()

	for i, e := range o.array("events") {
		event, ok := o.element(e, fmt.Sprintf("event %d", i))
		if !ok {
			continue
		}

		w.Events = append(w.Events, ingest.WorkoutEvent{
			Type:  event.str("type", true),
			Start: event.str("start", true),
			End:   event.str("end", true),
		})
	}

	for i, p := range o.array("route") {
		point, ok := o.element(p, fmt.Sprintf("route point %d", i))
		if !ok {
			continue
		}

		rp := ingest.RoutePoint{
			Time:               point.str("time", true),
			Altitude:           point.number("altitude", false),
			Speed:              point.number("speed", false),
			Course:             point.number("course", false),
			HorizontalAccuracy: point.number("horizontalAccuracy", false),
			VerticalAccuracy:   point.number("verticalAccuracy", false),
		}

		if lat := point.number("latitude", true); lat != nil {
			rp.Latitude = *lat
		}

		if lon := point.number("longitude", true); lon != nil {
			rp.Longitude = *lon
		}

		w.Route = append(w.Route, rp)
	}

	if *o.reason != "" {
		return invalidRecord(ingest.WorkoutRecord, "", *o.reason)
	}

	return ingest.Record{Workout: &w}
}

// decodeDeletion decodes a deleted uuid.
func decodeDeletion(raw json.RawMessage) ingest.Record {
	var uuid string
	if json.Unmarshal(raw, &uuid) != nil {
		return invalidRecord(ingest.DeletionRecord, "", "deletion is not a string")
	}

	if !isUUID(uuid) {
		return invalidRecord(ingest.DeletionRecord, "", "deletion is not a uuid")
	}

	return ingest.Record{Deletion: &ingest.Deletion{UUID: uuid}}
}

// invalidRecord returns an invalid record of kind of, of the sample type typ, with reason.
func invalidRecord(of ingest.RecordKind, typ, reason string) ingest.Record {
	return ingest.Record{Invalid: &ingest.Invalid{Of: of, Type: typ, Reason: reason}}
}

// object reads the members of a JSON object of a record. Its reason, shared with the objects that
// it holds, is that of the first member missing or of the wrong type, empty while there is none.
type object struct {
	members map[string]json.RawMessage
	// path names the object in a reason, such as "source" or "route point 2", empty for the record.
	path   string
	reason *string
}

// newObject returns the object of raw named by path, and false when raw is not a JSON object.
func newObject(raw json.RawMessage, path string) (object, bool) {
	var reason string

	return object{reason: &reason}.child(raw, path)
}

// child returns the object of raw, named by path, that shares the reason of o, and false when raw
// is not a JSON object.
func (o object) child(raw json.RawMessage, path string) (object, bool) {
	var members map[string]json.RawMessage
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) || json.Unmarshal(raw, &members) != nil {
		return object{}, false
	}

	return object{members: members, path: path, reason: o.reason}, true
}

// fail sets the reason of o, unless it already has one.
func (o object) fail(format string, args ...any) {
	if *o.reason != "" {
		return
	}

	reason := fmt.Sprintf(format, args...)
	if o.path != "" {
		reason = o.path + ": " + reason
	}

	*o.reason = reason
}

// value returns the member name, and false when it is left out or null.
func (o object) value(name string) (json.RawMessage, bool) {
	v, ok := o.members[name]
	if !ok || string(v) == "null" {
		return nil, false
	}

	return v, true
}

// has reports whether o has the member name.
func (o object) has(name string) bool {
	_, ok := o.value(name)

	return ok
}

// str returns the string member name, empty when it is left out or not a string.
func (o object) str(name string, required bool) string {
	v, ok := o.value(name)
	if !ok {
		if required {
			o.fail("%s is missing", name)
		}

		return ""
	}

	var s string
	if json.Unmarshal(v, &s) != nil {
		o.fail("%s is not a string", name)

		return ""
	}

	return s
}

// uuid returns the uuid member name, empty when it is left out or not a uuid.
func (o object) uuid(name string, required bool) string {
	s := o.str(name, required)
	if s != "" && !isUUID(s) {
		o.fail("%s is not a uuid", name)

		return ""
	}

	return s
}

// number returns the number member name, nil when it is left out, not a number, or too large for
// a finite float64.
func (o object) number(name string, required bool) *float64 {
	v, ok := o.value(name)
	if !ok {
		if required {
			o.fail("%s is missing", name)
		}

		return nil
	}

	var n float64
	if json.Unmarshal(v, &n) != nil {
		if c := v[0]; c == '-' || ('0' <= c && c <= '9') {
			o.fail("%s is not a finite number", name)
		} else {
			o.fail("%s is not a number", name)
		}

		return nil
	}

	return &n
}

// boolean returns the boolean member name, nil when it is left out or not a boolean.
func (o object) boolean(name string) *bool {
	v, ok := o.value(name)
	if !ok {
		return nil
	}

	var b bool
	if json.Unmarshal(v, &b) != nil {
		o.fail("%s is not a boolean", name)

		return nil
	}

	return &b
}

// object returns the object member name, and false when it is left out or not an object.
func (o object) object(name string, required bool) (object, bool) {
	v, ok := o.value(name)
	if !ok {
		if required {
			o.fail("%s is missing", name)
		}

		return object{}, false
	}

	path := name
	if o.path != "" {
		path = o.path + ": " + name
	}

	child, ok := o.child(v, path)
	if !ok {
		o.fail("%s is not an object", name)
	}

	return child, ok
}

// element returns the object raw of an array, named by path, and false when it is not an object.
func (o object) element(raw json.RawMessage, path string) (object, bool) {
	child, ok := o.child(raw, path)
	if !ok {
		o.fail("%s is not an object", path)
	}

	return child, ok
}

// array returns the elements of the array member name, nil when it is left out or not an array.
func (o object) array(name string) []json.RawMessage {
	v, ok := o.value(name)
	if !ok {
		return nil
	}

	var elements []json.RawMessage
	if !bytes.HasPrefix(v, []byte("[")) || json.Unmarshal(v, &elements) != nil {
		o.fail("%s is not an array", name)

		return nil
	}

	return elements
}

// quantity returns the quantity member name, nil when it is left out or invalid. Its unit is kept as
// received, for unit conversion to check.
func (o object) quantity(name string) *ingest.Quantity {
	q, ok := o.object(name, false)
	if !ok {
		return nil
	}

	value, unit := q.number("value", true), q.str("unit", true)
	if value == nil {
		return nil
	}

	return &ingest.Quantity{Value: *value, Unit: unit}
}

// source returns the required source member of a sample or workout.
func (o object) source() store.SourceInfo {
	s, ok := o.object("source", true)
	if !ok {
		return store.SourceInfo{}
	}

	info := store.SourceInfo{Name: s.str("name", true), BundleID: s.str("bundleId", false)}

	if d, ok := s.object("device", false); ok {
		info.DeviceName = d.str("name", false)
		info.Manufacturer = d.str("manufacturer", false)
		info.DeviceModel = d.str("model", false)
		info.HardwareVersion = d.str("hardwareVersion", false)
		info.SoftwareVersion = d.str("softwareVersion", false)
	}

	return info
}

// metadata returns the metadata member as received, nil when it is left out or invalid: an object
// whose values are strings, numbers or booleans.
func (o object) metadata() json.RawMessage {
	m, ok := o.object("metadata", false)
	if !ok {
		return nil
	}

	for _, v := range m.members {
		switch v[0] {
		case '{', '[', 'n':
			o.fail("metadata has a value that is not a string, a number or a boolean")

			return nil
		}
	}

	raw, _ := o.value("metadata")

	return raw
}

// hexDigits are the digits of a uuid, in either case.
const hexDigits = "0123456789abcdefABCDEF"

// isUUID reports whether s is the 36-character text form of a UUID, whatever its case.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	for i := range len(s) {
		c := s[i]

		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !strings.ContainsRune(hexDigits, rune(c)) {
				return false
			}
		}
	}

	return true
}
