// Package applexml reads the export.xml of an Apple Health export (XML import) as a stream of
// normalized records, so that an export of several gigabytes never loads into memory.
package applexml

import (
	"bufio"
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
// such as ExportDate, Me and, until workouts are read, Workout, are skipped with their children.
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

// tokenizer reads the tokens of a document and checks what xml.Decoder.RawToken leaves out: that
// every end tag matches its start tag, and that the document has one root element with nothing but
// space around it.
type tokenizer struct {
	dec *xml.Decoder
	src *sourceReader
	// dtd is where the DTD was in the document, which the decoder never reads.
	dtd gap
	// open are the names of the elements that are open.
	open []xml.Name
	// closed is true once the root element is closed.
	closed bool
	// start is the byte offset in the document of the last token read.
	start int64
}

// gap is a range of bytes of the document that the decoder does not read: size bytes at the offset
// at of what it reads.
type gap struct {
	at, size int64
}

// newTokenizer returns a tokenizer of the document of r, and the error of r when it fails while
// the inline DTD is skipped.
func newTokenizer(r io.Reader) (*tokenizer, error) {
	src := &sourceReader{r: r}

	rest, dtd, err := skipDTD(src)
	if err != nil {
		return nil, fmt.Errorf("read export.xml: %w", err)
	}

	return &tokenizer{dec: xml.NewDecoder(rest), src: src, dtd: dtd}, nil
}

// maxProlog is how far from the start of the document the inline DTD is looked for.
const maxProlog = 64 << 10

// doctype starts the inline DTD, after the less-than sign.
const doctype = "!DOCTYPE"

// skipDTD returns the document of r without its inline DTD, from <!DOCTYPE to the first ]>, so that
// the decoder never parses it, since some iOS versions produce a malformed DTD (FR-5 of XML
// import). The DTD is looked for in the prolog only: before the first less-than sign that starts
// neither a declaration, a comment nor a processing instruction. It returns where the DTD was, and
// the error of r when it fails.
func skipDTD(r io.Reader) (io.Reader, gap, error) {
	br := bufio.NewReader(r)

	var head []byte

	for len(head) < maxProlog {
		chunk, err := br.ReadSlice('<')
		head = append(head, chunk...)

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return bytes.NewReader(head), gap{}, nil
		case err != nil:
			return nil, gap{}, err
		}

		next, err := br.Peek(len(doctype))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, gap{}, err
		}

		if string(next) == doctype {
			size, err := discardDTD(br)
			if err != nil {
				return nil, gap{}, err
			}

			head = head[:len(head)-1]

			return io.MultiReader(bytes.NewReader(head), br), gap{at: int64(len(head)), size: 1 + size}, nil
		}

		if len(next) == 0 || (next[0] != '?' && next[0] != '!') {
			break
		}
	}

	return io.MultiReader(bytes.NewReader(head), br), gap{}, nil
}

// discardDTD discards the bytes of br up to the first ]> included, or to the end of the document,
// and returns how many it discarded.
func discardDTD(br *bufio.Reader) (int64, error) {
	var size int64

	for {
		chunk, err := br.ReadSlice(']')
		size += int64(len(chunk))

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return size, nil
		case err != nil:
			return 0, err
		}

		next, err := br.Peek(1)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}

		if string(next) == ">" {
			_, _ = br.Discard(1)

			return size + 1, nil
		}
	}
}

// depth returns the number of open elements.
func (t *tokenizer) depth() int {
	return len(t.open)
}

// next returns the next token, and io.EOF once the document ends after its root element.
func (t *tokenizer) next() (xml.Token, error) {
	t.start = t.dec.InputOffset()
	if t.start >= t.dtd.at {
		t.start += t.dtd.size
	}

	tok, err := t.dec.RawToken()

	switch {
	case t.src.err != nil:
		return nil, fmt.Errorf("read export.xml: %w", t.src.err)
	case errors.Is(err, io.EOF) && !t.closed:
		return nil, t.malformed(errors.New("document ends before its root element is closed"))
	case errors.Is(err, io.EOF):
		return nil, io.EOF
	case err != nil:
		return nil, t.malformed(err)
	}

	switch tok := tok.(type) {
	case xml.StartElement:
		if t.closed {
			return nil, t.malformed(errors.New("element after the root element"))
		}

		t.open = append(t.open, tok.Name)
	case xml.EndElement:
		if len(t.open) == 0 || t.open[len(t.open)-1] != tok.Name {
			return nil, t.malformed(errors.New("end tag does not match its start tag"))
		}

		t.open = t.open[:len(t.open)-1]
		t.closed = len(t.open) == 0
	case xml.CharData:
		if len(t.open) == 0 && !isSpace(tok) {
			return nil, t.malformed(errors.New("text outside the root element"))
		}
	}

	return tok, nil
}

// skip reads the tokens up to the end tag of the element whose start tag was read last.
func (t *tokenizer) skip() error {
	depth := t.depth()

	for t.depth() >= depth {
		if _, err := t.next(); err != nil {
			return err
		}
	}

	return nil
}

// malformed returns err as an error of a malformed document, at the offset of the last token.
func (t *tokenizer) malformed(err error) error {
	return fmt.Errorf("%w at byte %d: %w", ErrMalformed, t.start, err)
}

// isSpace reports whether data holds only XML white space.
func isSpace(data []byte) bool {
	for _, c := range data {
		switch c {
		case ' ', '\t', '\r', '\n':
		default:
			return false
		}
	}

	return true
}

// sourceReader reads the document, and keeps the first error of its reader other than io.EOF, so
// that a read error is never taken for a malformed document.
type sourceReader struct {
	r   io.Reader
	err error
}

func (s *sourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && s.err == nil {
		s.err = err
	}

	return n, err
}

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
			a := attributes(child.Attr)
			key, _ := a.get("key")
			value, _ := a.get("value")
			m.entry(key, value)
		case "HeartRateVariabilityMetadataList":
			beats, err := t.beats()
			if err != nil {
				return ingest.Record{}, err
			}

			m.beats(beats)
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

// beats reads the children of a HeartRateVariabilityMetadataList element, whose start tag was read
// last, and returns the attributes of its beats, in document order.
func (t *tokenizer) beats() ([]attributes, error) {
	var beats []attributes

	depth := t.depth()

	for {
		tok, err := t.next()
		if err != nil {
			return nil, err
		}

		if t.depth() < depth {
			return beats, nil
		}

		if beat, ok := tok.(xml.StartElement); ok && t.depth() == depth+1 && beat.Name.Local == "InstantaneousBeatsPerMinute" {
			beats = append(beats, beat.Attr)
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

// entry adds the key of a MetadataEntry, with its value as a string, as received.
func (m *metadata) entry(key, value string) {
	if m.key(key) {
		m.string(value)
	}
}

// beats adds the beats of a HeartRateVariabilityMetadataList, as an array of objects holding their
// attributes as received.
func (m *metadata) beats(beats []attributes) {
	if !m.key(metadataBeats) {
		return
	}

	m.buf.WriteByte('[')

	for i, beat := range beats {
		if i > 0 {
			m.buf.WriteByte(',')
		}

		m.buf.WriteByte('{')

		for j, attr := range beat {
			if j > 0 {
				m.buf.WriteByte(',')
			}

			m.string(attr.Name.Local)
			m.buf.WriteByte(':')
			m.string(attr.Value)
		}

		m.buf.WriteByte('}')
	}

	m.buf.WriteByte(']')
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
		return invalid("", "type is missing")
	}

	unit, quantity := a.get("unit")
	value, hasValue := a.get("value")

	var number float64

	switch {
	case quantity && !hasValue, !quantity && value == "":
		return invalid(typ, "value is missing")
	case quantity:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return invalid(typ, "value is not a number")
		}

		number = n
	}

	start, reason := date(a, "startDate")
	if reason != "" {
		return invalid(typ, reason)
	}

	end, reason := date(a, "endDate")
	if reason != "" {
		return invalid(typ, reason)
	}

	sourceName, _ := a.get("sourceName")
	source := store.SourceInfo{Name: sourceName}

	if device, ok := a.get("device"); ok {
		setDevice(&source, device)
	}

	if quantity {
		return ingest.Record{Quantity: &ingest.QuantitySample{
			Type: typ, Start: start, End: end, Value: number, Unit: unit, Source: source,
		}}
	}

	return ingest.Record{Category: &ingest.CategorySample{
		Type: typ, Start: start, End: end, Value: value, Source: source,
	}}
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

// invalid returns an invalid sample of the type typ, with reason.
func invalid(typ, reason string) ingest.Record {
	return ingest.Record{Invalid: &ingest.Invalid{Of: ingest.SampleRecord, Type: typ, Reason: reason}}
}

// dateLayouts are the layouts of the dates of an export (FR-8 of XML import): its format,
// yyyy-MM-dd HH:mm:ss Z, and its tolerated variants. An offset Z is read by both, and a one-digit
// hour and fractional seconds by time.Parse whatever the layout.
var dateLayouts = []string{"2006-01-02 15:04:05 Z0700", "2006-01-02 15:04:05 Z07:00"}

// date returns the date attribute name of a as RFC 3339, with its instant and offset, or the reason
// of an invalid record.
func date(a attributes, name string) (string, string) {
	value, ok := a.get(name)
	if !ok {
		return "", name + " is missing"
	}

	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.Format(time.RFC3339Nano), ""
		}
	}

	return "", name + " is not a date"
}
