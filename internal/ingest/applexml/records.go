// Package applexml reads the export.xml of an Apple Health export (XML import) as a stream of
// normalized records, so that an export of several gigabytes never loads into memory.
package applexml

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"strconv"
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
		t := newTokenizer(r)

		for {
			tok, err := t.next()
			if errors.Is(err, io.EOF) {
				return
			}

			if err != nil {
				yield(ingest.Record{}, err)

				return
			}

			start, ok := tok.(xml.StartElement)
			if !ok || t.depth() != topLevel {
				continue
			}

			position := t.start

			switch start.Name.Local {
			case "Record":
				if err := t.skip(); err != nil {
					yield(ingest.Record{}, err)

					return
				}

				record := newRecord(start.Attr)
				record.Position = position

				if !yield(record, nil) {
					return
				}
			default:
				if err := t.skip(); err != nil {
					yield(ingest.Record{}, err)

					return
				}
			}
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
	// open are the names of the elements that are open.
	open []xml.Name
	// closed is true once the root element is closed.
	closed bool
	// start is the byte offset of the last token read.
	start int64
}

// newTokenizer returns a tokenizer of the document of r.
func newTokenizer(r io.Reader) *tokenizer {
	src := &sourceReader{r: r}

	return &tokenizer{dec: xml.NewDecoder(src), src: src}
}

// depth returns the number of open elements.
func (t *tokenizer) depth() int {
	return len(t.open)
}

// next returns the next token, and io.EOF once the document ends after its root element.
func (t *tokenizer) next() (xml.Token, error) {
	t.start = t.dec.InputOffset()

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

	if quantity {
		return ingest.Record{Quantity: &ingest.QuantitySample{
			Type: typ, Start: start, End: end, Value: number, Unit: unit, Source: source,
		}}
	}

	return ingest.Record{Category: &ingest.CategorySample{
		Type: typ, Start: start, End: end, Value: value, Source: source,
	}}
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
