package native_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/native"
)

// update rewrites the golden files, whose diff then shows every change of the decoding.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// decoded is what a golden file holds of a payload: its client and its records.
type decoded struct {
	Client  native.Client
	Records []ingest.Record
}

// decode opens payload and reads all its records.
func decode(t *testing.T, payload []byte) decoded {
	t.Helper()

	p, err := native.Open(t.Context(), bytes.NewReader(payload))
	require.NoError(t, err)

	got := decoded{Client: p.Client()}

	for r, err := range p.Records(t.Context()) {
		require.NoError(t, err)

		got.Records = append(got.Records, r)
	}

	return got
}

// requireGolden requires that got equals the golden file at path, or rewrites it with -update.
func requireGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o600)) //nolint:gosec // G703: a golden file of testdata
	}

	want, err := os.ReadFile(path) //nolint:gosec // G304: a golden file of testdata
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "%s differs, run the tests with -update to rewrite it", path)
}

// Given the payloads of format verve.ingest.v1 in testdata, when they are decoded, then they yield
// the records of their golden files: samples, workouts with their events and routes, and
// deletions, at their index across samples, workouts and deletions whatever the order of the
// members, the invalid ones with their reason among the others.
func TestRecords_PayloadsOfTestdata_MatchGoldenFiles(t *testing.T) {
	t.Parallel()

	payloads, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, payloads)

	for _, path := range payloads {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()

			payload, err := os.ReadFile(path) //nolint:gosec // G304: a payload of testdata
			require.NoError(t, err)

			got, err := json.MarshalIndent(decode(t, payload), "", "  ")
			require.NoError(t, err)

			requireGolden(t, strings.TrimSuffix(path, ".json")+".golden", append(got, '\n'))
		})
	}
}

// Given a payload, when its records are read twice, then both reads yield the same records, so
// that a resumed import finds each record at the same position.
func TestRecords_ReadTwice_YieldTheSameRecords(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("testdata", "full.json"))
	require.NoError(t, err)

	p, err := native.Open(t.Context(), bytes.NewReader(payload))
	require.NoError(t, err)

	read := func() []ingest.Record {
		var records []ingest.Record

		for r, err := range p.Records(t.Context()) {
			require.NoError(t, err)

			records = append(records, r)
		}

		return records
	}

	first := read()
	require.NotEmpty(t, first)
	require.Equal(t, first, read())
}

// Given the example payload of FR-7 of Ingestion pipeline, when it is decoded, then it yields its
// three samples, its workout and its deletion, none of them invalid, so that the spec and the
// decoder agree.
func TestRecords_ExamplePayloadOfTheSpec_DecodesWithoutError(t *testing.T) {
	t.Parallel()

	spec, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "features", "ingestion-pipeline.md"))
	require.NoError(t, err)

	_, after, found := strings.Cut(string(spec), "Example payload:")
	require.True(t, found, "the spec has no example payload")

	_, block, found := strings.Cut(after, "```json")
	require.True(t, found, "the example payload is not a json block")

	example, _, found := strings.Cut(block, "```")
	require.True(t, found, "the json block of the example payload does not end")

	got := decode(t, []byte(example))
	require.Equal(t, native.Client{Name: "Verve for iOS", Version: "1.0.0", DeviceModel: "iPhone17,1", OSVersion: "27.1"}, got.Client)
	require.Len(t, got.Records, 5)

	for i, r := range got.Records {
		require.Equal(t, int64(i), r.Position)
		require.Nil(t, r.Invalid, "record %d is invalid: %+v", i, r.Invalid)
	}

	require.NotNil(t, got.Records[0].Quantity)
	require.NotNil(t, got.Records[1].Quantity)
	require.NotNil(t, got.Records[2].Category)
	require.NotNil(t, got.Records[3].Workout)
	require.Len(t, got.Records[3].Workout.Events, 2)
	require.Len(t, got.Records[3].Workout.Route, 2)
	require.Equal(t, &ingest.Deletion{UUID: "5A4B3C2D-1E0F-4A9B-8C7D-6E5F4A3B2C1D"}, got.Records[4].Deletion)
}

// Given a body that is not JSON, or not a payload of format verve.ingest.v1, when it is opened,
// then decoding fails as a whole with ErrInvalidPayload.
func TestOpen_BodyThatIsNotAPayload_Fails(t *testing.T) {
	t.Parallel()

	const client = `"client": {"name": "Verve for iOS", "version": "1.0.0"}`

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ``},
		{name: "not JSON", body: `format=verve.ingest.v1`},
		{name: "truncated", body: `{"format": "verve.ingest.v1", ` + client + `, "samples": [{"type": "HKQuantityTypeIdentifierStepCount"`},
		{name: "invalid JSON inside a record", body: `{"format": "verve.ingest.v1", ` + client + `, "samples": [{"type": nope}]}`},
		{name: "data after the payload", body: `{"format": "verve.ingest.v1", ` + client + `} {}`},
		{name: "not an object", body: `["verve.ingest.v1"]`},
		{name: "format missing", body: `{` + client + `}`},
		{name: "other format", body: `{"format": "verve.ingest.v2", ` + client + `}`},
		{name: "format not a string", body: `{"format": 1, ` + client + `}`},
		{name: "client missing", body: `{"format": "verve.ingest.v1"}`},
		{name: "client not an object", body: `{"format": "verve.ingest.v1", "client": "Verve for iOS"}`},
		{name: "client name missing", body: `{"format": "verve.ingest.v1", "client": {"version": "1.0.0"}}`},
		{name: "client version not a string", body: `{"format": "verve.ingest.v1", "client": {"name": "Verve for iOS", "version": 1}}`},
		{name: "samples not an array", body: `{"format": "verve.ingest.v1", ` + client + `, "samples": {}}`},
		{name: "workouts not an array", body: `{"format": "verve.ingest.v1", ` + client + `, "workouts": "none"}`},
		{name: "deletions not an array", body: `{"format": "verve.ingest.v1", ` + client + `, "deletions": 3}`},
		{name: "samples twice", body: `{"format": "verve.ingest.v1", ` + client + `, "samples": [], "samples": []}`},
		{name: "client twice", body: `{"format": "verve.ingest.v1", ` + client + `, ` + client + `}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := native.Open(t.Context(), strings.NewReader(tt.body))
			require.ErrorIs(t, err, native.ErrInvalidPayload)
		})
	}
}

// Given a payload whose optional members are left out or null, when it is decoded, then it yields
// no records.
func TestRecords_PayloadWithoutRecords_YieldsNone(t *testing.T) {
	t.Parallel()

	got := decode(t, []byte(`{"format": "verve.ingest.v1", "client": {"name": "Verve for iOS", "version": "1.0.0"}, "samples": null, "workouts": []}`))
	require.Equal(t, native.Client{Name: "Verve for iOS", Version: "1.0.0"}, got.Client)
	require.Empty(t, got.Records)
}

// readCounter is a payload that counts the bytes read from it.
type readCounter struct {
	payload []byte
	read    atomic.Int64
}

func (r *readCounter) ReadAt(p []byte, off int64) (int, error) {
	n, err := bytes.NewReader(r.payload).ReadAt(p, off)
	r.read.Add(int64(n))

	return n, err
}

// Given a large payload, when its records are read, then the first record comes after reading a
// small part of the payload, so that decoding never holds the whole payload in memory.
func TestRecords_LargePayload_StreamsRecordByRecord(t *testing.T) {
	t.Parallel()

	const sample = `{"uuid": "6F1C2B9E-3D4A-4E0B-9C59-1A2B3C4D5E6F", "type": "HKQuantityTypeIdentifierStepCount", ` +
		`"start": "2026-11-10T08:00:00+01:00", "end": "2026-11-10T08:10:00+01:00", "value": 1240, "unit": "count", ` +
		`"source": {"name": "Apple Watch", "device": {"model": "Watch"}}}`

	const count = 20_000

	var body strings.Builder

	body.WriteString(`{"format": "verve.ingest.v1", "client": {"name": "Verve for iOS", "version": "1.0.0"}, "samples": [`)

	for i := range count {
		if i > 0 {
			body.WriteString(",")
		}

		body.WriteString(sample)
	}

	body.WriteString(`]}`)

	payload := &readCounter{payload: []byte(body.String())}

	p, err := native.Open(t.Context(), payload)
	require.NoError(t, err)

	opened := payload.read.Load()
	n := 0

	for r, err := range p.Records(t.Context()) {
		require.NoError(t, err)
		require.Equal(t, int64(n), r.Position)

		if n == 0 {
			require.Less(t, payload.read.Load()-opened, int64(len(payload.payload)/100),
				"the first record needs most of the payload")
		}

		n++
	}

	require.Equal(t, count, n)
}

// Given a payload whose reads fail once it is opened, when its records are read, then the
// iterator yields the error.
func TestRecords_ReadFailure_YieldsTheError(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("testdata", "full.json"))
	require.NoError(t, err)

	r := &failingReader{payload: payload}

	p, err := native.Open(t.Context(), r)
	require.NoError(t, err)

	r.fail.Store(true)

	var got error

	for _, err := range p.Records(t.Context()) {
		if err != nil {
			got = err

			break
		}
	}

	require.ErrorIs(t, got, errRead)
}

// errRead is the error of a failingReader.
var errRead = errors.New("disk read failed")

// failingReader is a payload whose reads fail once fail is set.
type failingReader struct {
	payload []byte
	fail    atomic.Bool
}

func (r *failingReader) ReadAt(p []byte, off int64) (int, error) {
	if r.fail.Load() {
		return 0, errRead
	}

	return bytes.NewReader(r.payload).ReadAt(p, off)
}

// Given a cancelled context, when a payload is opened or its records are read, then they stop with
// the error of the context.
func TestOpenAndRecords_CancelledContext_Stop(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("testdata", "full.json"))
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = native.Open(cancelled, bytes.NewReader(payload))
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, native.ErrInvalidPayload)

	p, err := native.Open(t.Context(), bytes.NewReader(payload))
	require.NoError(t, err)

	var got error

	for _, err := range p.Records(cancelled) {
		got = err

		break
	}

	require.ErrorIs(t, got, context.Canceled)
}
