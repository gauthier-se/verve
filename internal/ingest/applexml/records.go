// Package applexml reads the export.xml of an Apple Health export (XML import) as a stream of
// normalized records, so that an export of several gigabytes never loads into memory.
package applexml

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// ErrMalformed is the error of a document that is truncated or is not well-formed XML, which fails
// its import without retry (FR-5 of XML import).
var ErrMalformed = errors.New("malformed XML")

// Records reads the records of the export.xml document of r, as a stream (FR-5 and FR-6 of XML
// import). The position of a record is the byte offset of its start tag in the document. A record
// that the adapter cannot read, such as one whose value is not a number, is an ingest.Invalid
// record with its reason, and the others are read.
//
// It yields an error, and stops, after the records that came before it: one wrapping ErrMalformed
// for a document that is truncated or is not well-formed XML, and the error of r when r fails.
func Records(ctx context.Context, r io.Reader) iter.Seq2[ingest.Record, error] {
	return func(yield func(ingest.Record, error) bool) {
		t, err := newTokenizer(r)
		if err == nil {
			err = t.records(ctx, func(r ingest.Record) bool { return yield(r, nil) })
		}

		if err != nil {
			yield(ingest.Record{}, err)
		}
	}
}

// unsupportedElements are the elements under the root that count as unsupported under their name
// (FR-6 of XML import).
var unsupportedElements = []string{"ActivitySummary", "ClinicalRecord", "Audiogram", "VisionPrescription"}

// records reads the elements under the root element and yields their records, until the document
// ends, yield returns false or ctx is done, which it checks at each of them. Elements of no record,
// such as ExportDate and Me, are skipped with their children.
func (t *tokenizer) records(ctx context.Context, yield func(ingest.Record) bool) error {
	for {
		tok, err := t.next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		start, ok := tok.(xml.StartElement)
		if !ok || t.depth() != topLevel {
			continue
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read export.xml at byte %d: %w", t.start, err)
		}

		more := true

		switch name := start.Name.Local; {
		case name == "Record":
			var record ingest.Record

			if record, err = t.record(start); err == nil {
				more = yield(record)
			}
		case name == "Correlation":
			more, err = t.correlation(yield)
		case name == "Workout":
			more, err = t.workout(start, yield)
		case slices.Contains(unsupportedElements, name):
			position := t.start

			if err = t.skip(); err == nil {
				more = yield(ingest.Record{Position: position, Unsupported: &ingest.Unsupported{Identifier: name}})
			}
		default:
			err = t.skip()
		}

		if err != nil || !more {
			return err
		}
	}
}

// correlation reads the children of a Correlation element, whose start tag was read last, and
// yields its inner records as top-level records. The correlation itself, its attributes and its
// metadata yield nothing (FR-6 of XML import). It returns false once yield does.
func (t *tokenizer) correlation(yield func(ingest.Record) bool) (bool, error) {
	depth := t.depth()

	for {
		tok, err := t.next()
		if err != nil {
			return false, err
		}

		if t.depth() < depth {
			return true, nil
		}

		start, ok := tok.(xml.StartElement)
		if !ok || t.depth() != depth+1 || start.Name.Local != "Record" {
			continue
		}

		record, err := t.record(start)
		if err != nil {
			return false, err
		}

		if !yield(record) {
			return false, nil
		}
	}
}

// topLevel is the depth of the elements under the root element, once their start tag is read.
const topLevel = 2

// attributes are the attributes of an element.
type attributes []xml.Attr

// get returns the attribute name, and false when the element lacks it.
func (a attributes) get(name string) (string, bool) {
	for _, attr := range a {
		if attr.Name.Local == name {
			return attr.Value, true
		}
	}

	return "", false
}

// record reads the children of the Record element of start, whose start tag was read last, and
// returns its record, at the position of its start tag.
func (t *tokenizer) record(start xml.StartElement) (ingest.Record, error) {
	var m metadata

	position, depth := t.start, t.depth()

	for {
		tok, err := t.next()
		if err != nil {
			return ingest.Record{}, err
		}

		if t.depth() < depth {
			break
		}

		child, ok := tok.(xml.StartElement)
		if !ok || t.depth() != depth+1 {
			continue
		}

		switch child.Name.Local {
		case "MetadataEntry":
			m.entry(newMetadataEntry(child.Attr))
		case metadataBeats:
			beats, err := t.children("InstantaneousBeatsPerMinute")
			if err != nil {
				return ingest.Record{}, err
			}

			m.objects(metadataBeats, beats)
		}
	}

	record := newRecord(start.Attr)
	record.Position = position

	switch {
	case record.Quantity != nil:
		record.Quantity.Metadata = m.object()
	case record.Category != nil:
		record.Category.Metadata = m.object()
	}

	return record, nil
}

// children reads the children of the element whose start tag was read last, and returns the
// attributes of those named name, in document order.
func (t *tokenizer) children(name string) ([]attributes, error) {
	var children []attributes

	depth := t.depth()

	for {
		tok, err := t.next()
		if err != nil {
			return nil, err
		}

		if t.depth() < depth {
			return children, nil
		}

		if child, ok := tok.(xml.StartElement); ok && t.depth() == depth+1 && child.Name.Local == name {
			children = append(children, child.Attr)
		}
	}
}

// metadataBeats is the metadata key of the beats of a heart rate variability record (FR-6 of XML
// import).
const metadataBeats = "HeartRateVariabilityMetadataList"

// metadata writes the metadata of a record as a JSON object, its keys in document order. A key that
// a record holds twice keeps its first value.
type metadata struct {
	buf  bytes.Buffer
	enc  *json.Encoder
	keys []string
}

// metadataEntry is a MetadataEntry element: a metadata key and its value as received.
type metadataEntry struct {
	key, value string
}

// newMetadataEntry returns the entry of the attributes of a MetadataEntry element.
func newMetadataEntry(attrs []xml.Attr) metadataEntry {
	a := attributes(attrs)
	key, _ := a.get("key")
	value, _ := a.get("value")

	return metadataEntry{key: key, value: value}
}

// entry adds the key of e, with its value as a string, as received.
func (m *metadata) entry(e metadataEntry) {
	if m.key(e.key) {
		m.string(e.value)
	}
}

// objects adds key with an array of objects holding the attributes of each of items as received,
// such as the beats of a HeartRateVariabilityMetadataList.
func (m *metadata) objects(key string, items []attributes) {
	if !m.key(key) {
		return
	}

	m.array(len(items), func(i int) { m.attributes(items[i]) })
}

// array writes an array of n items, each written by item.
func (m *metadata) array(n int, item func(i int)) {
	m.buf.WriteByte('[')

	for i := range n {
		if i > 0 {
			m.buf.WriteByte(',')
		}

		item(i)
	}

	m.buf.WriteByte(']')
}

// attributes writes an object of attrs, as received.
func (m *metadata) attributes(attrs attributes) {
	m.buf.WriteByte('{')
	m.members(attrs)
	m.buf.WriteByte('}')
}

// members writes the members of an object of attrs, as received, without its braces.
func (m *metadata) members(attrs attributes) {
	for i, attr := range attrs {
		if i > 0 {
			m.buf.WriteByte(',')
		}

		m.string(attr.Name.Local)
		m.buf.WriteByte(':')
		m.string(attr.Value)
	}
}

// key writes key and returns true, or returns false when the object already has it.
func (m *metadata) key(key string) bool {
	if slices.Contains(m.keys, key) {
		return false
	}

	m.keys = append(m.keys, key)

	if m.enc == nil {
		m.enc = json.NewEncoder(&m.buf)
		m.enc.SetEscapeHTML(false)
		m.buf.WriteByte('{')
	} else {
		m.buf.WriteByte(',')
	}

	m.string(key)
	m.buf.WriteByte(':')

	return true
}

// string writes s as a JSON string, without escaping the characters that HTML reserves, so that
// the stored metadata holds them as received.
func (m *metadata) string(s string) {
	_ = m.enc.Encode(s) // a string always encodes

	m.buf.Truncate(m.buf.Len() - 1) // the newline of Encode
}

// object returns the JSON object, nil when the record has no metadata.
func (m *metadata) object() json.RawMessage {
	if m.enc == nil {
		return nil
	}

	m.buf.WriteByte('}')

	return m.buf.Bytes()
}

// newRecord returns the sample of a Record element of attrs: a quantity sample when it has a unit,
// a category sample otherwise, and an invalid record whose reason names the first attribute that
// is missing or invalid, never its value.
func newRecord(attrs []xml.Attr) ingest.Record {
	a := attributes(attrs)

	typ, _ := a.get("type")
	if typ == "" {
		return invalidRecord("", "type is missing")
	}

	unit, quantity := a.get("unit")
	value, hasValue := a.get("value")

	var number float64

	switch {
	case quantity && !hasValue, !quantity && value == "":
		return invalidRecord(typ, "value is missing")
	case quantity:
		n, ok := parseNumber(value)
		if !ok {
			return invalidRecord(typ, "value is not a number")
		}

		number = n
	}

	start, reason := date(a, "startDate")
	if reason != "" {
		return invalidRecord(typ, reason)
	}

	end, reason := date(a, "endDate")
	if reason != "" {
		return invalidRecord(typ, reason)
	}

	source := source(a)

	if quantity {
		return ingest.Record{Quantity: &ingest.QuantitySample{
			Type: typ, Start: start, End: end, Value: number, Unit: unit, Source: source,
		}}
	}

	return ingest.Record{Category: &ingest.CategorySample{
		Type: typ, Start: start, End: end, Value: value, Source: source,
	}}
}

// source returns the source of the attributes of a record or a workout: its sourceName and the
// fields of its device string.
func source(a attributes) store.SourceInfo {
	sourceName, _ := a.get("sourceName")
	source := store.SourceInfo{Name: sourceName}

	if device, ok := a.get("device"); ok {
		setDevice(&source, device)
	}

	return source
}

// setDevice sets the device fields of source from the device string of a record, such as
// <<HKDevice: 0x141b957d8>, name:Apple Watch, manufacturer:Apple Inc., model:Watch,
// hardware:Watch7,3, software:26.0, creation date:2025-12-29 23:00:00 +0000> (FR-6 of XML import).
// A field that the string lacks is left empty, and its other fields are ignored.
func setDevice(source *store.SourceInfo, device string) {
	body, ok := strings.CutPrefix(device, "<<HKDevice: ")
	if !ok {
		return
	}

	if _, body, ok = strings.Cut(body, ">, "); !ok {
		return
	}

	for body = strings.TrimSuffix(body, ">"); body != ""; {
		var field string

		field, body = nextField(body)
		key, value, _ := strings.Cut(field, ":")

		switch key {
		case "name":
			source.DeviceName = value
		case "manufacturer":
			source.Manufacturer = value
		case "model":
			source.DeviceModel = value
		case "hardware":
			source.HardwareVersion = value
		case "software":
			source.SoftwareVersion = value
		}
	}
}

// nextField splits the fields of a device string into the first one and the others. A field ends
// at the comma and space that come before the key of the next one, letters and spaces followed by a
// colon, so that a value may hold a comma, as hardware:Watch7,3 does.
func nextField(fields string) (string, string) {
	for i := 0; ; {
		j := strings.Index(fields[i:], ", ")
		if j < 0 {
			return fields, ""
		}

		next := i + j + len(", ")
		if isKey(fields[next:]) {
			return fields[:i+j], fields[next:]
		}

		i = next
	}
}

// isKey reports whether s starts with the key of a device field: a letter, then letters and spaces,
// then a colon.
func isKey(s string) bool {
	for i, c := range s {
		switch {
		case c == ':':
			return i > 0
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case c == ' ' && i > 0:
		default:
			return false
		}
	}

	return false
}

// invalidRecord returns an invalid sample of the type typ, with reason.
func invalidRecord(typ, reason string) ingest.Record {
	return ingest.Record{Invalid: &ingest.Invalid{Of: ingest.SampleRecord, Type: typ, Reason: reason}}
}

// parseNumber returns the decimal number s, in plain or scientific notation, and false for anything
// else, such as a hexadecimal number or one that is not finite, which strconv.ParseFloat accepts.
func parseNumber(s string) (float64, bool) {
	if strings.ContainsFunc(s, func(c rune) bool { return !strings.ContainsRune("0123456789+-.eE", c) }) {
		return 0, false
	}

	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, false
	}

	return n, true
}

// dateLayouts are the layouts of the dates of an export (FR-8 of XML import): its format,
// yyyy-MM-dd HH:mm:ss Z, and its tolerated variants. An offset Z is read by both, and a one-digit
// hour and fractional seconds by time.Parse whatever the layout, which also takes a comma for the
// decimal point of the seconds, a variant that the format does not tolerate.
var dateLayouts = []string{"2006-01-02 15:04:05 Z0700", "2006-01-02 15:04:05 Z07:00"}

// date returns the date attribute name of a as RFC 3339, with its instant and offset, or the reason
// of an invalid record.
func date(a attributes, name string) (string, string) {
	t, reason := parseDate(a, name)
	if reason != "" {
		return "", reason
	}

	return t.Format(time.RFC3339Nano), ""
}

// parseDate returns the date attribute name of a, in its offset, or the reason of an invalid
// record.
func parseDate(a attributes, name string) (time.Time, string) {
	value, ok := a.get(name)
	if !ok {
		return time.Time{}, name + " is missing"
	}

	if strings.Contains(value, ",") {
		return time.Time{}, name + " is not a date"
	}

	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, ""
		}
	}

	return time.Time{}, name + " is not a date"
}
