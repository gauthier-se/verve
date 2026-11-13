// Package native decodes the native ingest format of Verve, verve.ingest.v1 (FR-7 of Ingestion
// pipeline), into normalized records. It reads a payload twice, as a stream, so that a payload of
// any size is never held in memory: Open checks the whole payload and finds where each member
// starts, and Records reads the records one by one.
package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"

	"github.com/gauthier-se/verve/internal/ingest"
)

// Format is the format of the payloads that the package decodes.
const Format = "verve.ingest.v1"

// ErrInvalidPayload is the error of a body that is not a payload of Format: not valid JSON, not an
// object, without its format or client, or with a list of records that is not an array. Such a body
// fails as a whole (FR-6 of Ingestion pipeline), and the message of the error is its reason, which
// never quotes the body.
var ErrInvalidPayload = errors.New("invalid payload")

// Client is the client that sent a payload.
type Client struct {
	Name    string
	Version string
	// DeviceModel and OSVersion are those of the device that sent the payload, empty when the
	// client leaves them out.
	DeviceModel string
	OSVersion   string
}

// list is a list of records of a payload, in the order in which Records reads them.
type list int

const (
	samples list = iota
	workouts
	deletions
	lists
)

// names are the members of the lists of records.
var names = [lists]string{samples: "samples", workouts: "workouts", deletions: "deletions"}

// Payload is a payload of Format that Open checked.
type Payload struct {
	r      io.ReaderAt
	client Client
	// starts holds where the elements of each list of records start in r, just after the opening
	// bracket of its array, or -1 for a list that the payload leaves out.
	starts [lists]int64
}

// Open reads the whole payload of r once, as a stream, and checks that it is valid JSON and a
// payload of Format, its records aside. It returns an error wrapping ErrInvalidPayload when it is
// not, and another error when r fails.
func Open(r io.ReaderAt) (*Payload, error) {
	p := &Payload{r: r, starts: [lists]int64{-1, -1, -1}}
	dec := json.NewDecoder(io.NewSectionReader(r, 0, math.MaxInt64))

	tok, err := dec.Token()
	if err != nil {
		return nil, readError(err)
	}

	if tok != json.Delim('{') {
		return nil, invalid("payload is not a JSON object")
	}

	var format, client json.RawMessage

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, readError(err)
		}

		switch key := tok.(string); key {
		case "format":
			err = dec.Decode(&format)
		case "client":
			err = dec.Decode(&client)
		case names[samples]:
			err = p.scan(dec, samples)
		case names[workouts]:
			err = p.scan(dec, workouts)
		case names[deletions]:
			err = p.scan(dec, deletions)
		default:
			err = skip(dec)
		}

		if err != nil {
			return nil, err
		}
	}

	if _, err := dec.Token(); err != nil {
		return nil, readError(err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, readError(err)
		}

		return nil, invalid("payload has data after its end")
	}

	if err := checkFormat(format); err != nil {
		return nil, err
	}

	if p.client, err = decodeClient(client); err != nil {
		return nil, err
	}

	return p, nil
}

// Client returns the client of the payload.
func (p *Payload) Client() Client {
	return p.client
}

// Records reads the records of the payload, as a stream: its samples, then its workouts, then its
// deletions, so that deletions apply after the samples and workouts (FR-8 of Ingestion pipeline).
// The position of a record is its index in that order. A record with a field missing or of the
// wrong type is an ingest.Invalid record with its reason, and the others are read. Each call reads
// the payload again, and yields the same records.
//
// It yields an error, and stops, when the payload fails to read.
func (p *Payload) Records() iter.Seq2[ingest.Record, error] {
	decoders := [lists]func(json.RawMessage) ingest.Record{
		samples:   decodeSample,
		workouts:  decodeWorkout,
		deletions: decodeDeletion,
	}

	return func(yield func(ingest.Record, error) bool) {
		var position int64

		for l, start := range p.starts {
			if start < 0 {
				continue
			}

			// The elements start after the bracket that opens the array, which the first pass read.
			dec := json.NewDecoder(io.MultiReader(
				bytes.NewReader([]byte("[")),
				io.NewSectionReader(p.r, start, math.MaxInt64-start),
			))

			if _, err := dec.Token(); err != nil {
				yield(ingest.Record{}, fmt.Errorf("read %s: %w", names[l], err))

				return
			}

			for dec.More() {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					yield(ingest.Record{}, fmt.Errorf("read record %d: %w", position, err))

					return
				}

				r := decoders[l](raw)
				r.Position = position
				position++

				if !yield(r, nil) {
					return
				}
			}
		}
	}
}

// scan checks the array of records of l, one element at a time, and stores where its elements
// start. A null list counts as left out.
func (p *Payload) scan(dec *json.Decoder, l list) error {
	tok, err := dec.Token()

	switch {
	case err != nil:
		return readError(err)
	case tok == nil:
		p.starts[l] = -1

		return nil
	case tok != json.Delim('['):
		if err := skipRest(dec, tok); err != nil {
			return err
		}

		return invalid(names[l] + " is not an array")
	}

	p.starts[l] = dec.InputOffset()

	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return readError(err)
		}
	}

	if _, err := dec.Token(); err != nil {
		return readError(err)
	}

	return nil
}

// skip reads a value that the format does not define, token by token, so that even a large one
// is never held in memory.
func skip(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return readError(err)
	}

	return skipRest(dec, tok)
}

// skipRest reads the rest of the value whose first token is tok.
func skipRest(dec *json.Decoder, tok json.Token) error {
	depth := 0

	for {
		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}

		if depth == 0 {
			return nil
		}

		var err error
		if tok, err = dec.Token(); err != nil {
			return readError(err)
		}
	}
}

// checkFormat returns an error wrapping ErrInvalidPayload unless format is the JSON string Format.
func checkFormat(format json.RawMessage) error {
	if format == nil || string(format) == "null" {
		return invalid("format is missing")
	}

	var s string
	if json.Unmarshal(format, &s) != nil {
		return invalid("format is not a string")
	}

	if s != Format {
		return invalid("format is not " + Format)
	}

	return nil
}

// decodeClient decodes the client of a payload, or returns an error wrapping ErrInvalidPayload.
func decodeClient(raw json.RawMessage) (Client, error) {
	if raw == nil || string(raw) == "null" {
		return Client{}, invalid("client is missing")
	}

	o, ok := newObject(raw, "client")
	if !ok {
		return Client{}, invalid("client is not an object")
	}

	client := Client{
		Name:        o.str("name", true),
		Version:     o.str("version", true),
		DeviceModel: o.str("deviceModel", false),
		OSVersion:   o.str("osVersion", false),
	}

	if *o.reason != "" {
		return Client{}, invalid(*o.reason)
	}

	return client, nil
}

// invalid returns an error wrapping ErrInvalidPayload with reason.
func invalid(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidPayload, reason)
}

// readError returns the error of a read of the first pass: one wrapping ErrInvalidPayload for a
// payload that is not valid JSON or ends early, and err for a failure of the reader.
func readError(err error) error {
	var syntax *json.SyntaxError

	switch {
	case errors.As(err, &syntax):
		return invalid(fmt.Sprintf("payload is not valid JSON at byte %d", syntax.Offset))
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return invalid("payload ends early")
	default:
		return fmt.Errorf("read payload: %w", err)
	}
}
