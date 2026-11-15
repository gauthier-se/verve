package applexml_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
	"github.com/gauthier-se/verve/internal/store"
)

// head is the start of a document of the tests: the prolog, an inline DTD of the format and the
// root element.
const head = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE HealthData [
<!-- HealthKit Export Version: 17 -->
<!ELEMENT HealthData (ExportDate,Me,(Record|Correlation|Workout|ActivitySummary|ClinicalRecord|Audiogram|VisionPrescription)*)>
<!ATTLIST HealthData
  locale CDATA #REQUIRED
>
]>
<HealthData locale="en_FR">
`

// document returns a document of the elements of body, one per line, under the root element.
func document(body ...string) string {
	var b strings.Builder

	b.WriteString(head)

	for _, element := range body {
		b.WriteString(" " + element + "\n")
	}

	b.WriteString("</HealthData>\n")

	return b.String()
}

// read reads the records of doc until the end or the first error.
func read(t *testing.T, doc string) ([]ingest.Record, error) {
	t.Helper()

	return readFrom(t, strings.NewReader(doc))
}

// readFrom reads the records of r until the end or the first error.
func readFrom(t *testing.T, r io.Reader) ([]ingest.Record, error) {
	t.Helper()

	var records []ingest.Record

	for record, err := range applexml.Records(t.Context(), r) {
		if err != nil {
			return records, err
		}

		records = append(records, record)
	}

	return records, nil
}

// readAll reads every record of doc, which must read without error.
func readAll(t *testing.T, doc string) []ingest.Record {
	t.Helper()

	records, err := read(t, doc)
	require.NoError(t, err)

	return records
}

// offset returns the byte offset of the n-th occurrence, from 0, of tag in doc.
func offset(t *testing.T, doc, tag string, n int) int64 {
	t.Helper()

	at := -1

	for range n + 1 {
		i := strings.Index(doc[at+1:], tag)
		require.GreaterOrEqual(t, i, 0, "%s occurs fewer than %d times", tag, n+1)

		at += 1 + i
	}

	return int64(at)
}

const (
	heartRate = `<Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Apple Watch de Alex" sourceVersion="26.0" unit="count/min" creationDate="2025-12-30 07:01:00 +0100" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:00:00 +0100" value="62"/>`
	steps     = `<Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone de Alex" unit="count" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:05:00 +0100" value="120"/>`
	sleep     = `<Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Apple Watch de Alex" startDate="2025-12-30 01:00:00 +0100" endDate="2025-12-30 01:30:00 +0100" value="HKCategoryValueSleepAnalysisAsleepCore"/>`
)

// Given a record, then its position is the byte offset of its start tag in the document, and the
// positions of a document increase strictly and are the same on every read.
func TestRecords_PositionIsTheOffsetOfTheStartTag(t *testing.T) {
	t.Parallel()

	doc := document(heartRate, steps, sleep)

	first := readAll(t, doc)
	require.Len(t, first, 3)

	want := []int64{offset(t, doc, "<Record", 0), offset(t, doc, "<Record", 1), offset(t, doc, "<Record", 2)}
	require.Equal(t, want, []int64{first[0].Position, first[1].Position, first[2].Position})
	require.Less(t, first[0].Position, first[1].Position)
	require.Less(t, first[1].Position, first[2].Position)

	require.Equal(t, first, readAll(t, doc), "a second read yields the same records")
}

// Given a Record with a unit attribute, then it is a quantity sample with its value and unit as
// received, a value in scientific notation included. Given one without unit, then it is a category
// sample whose value is its value attribute.
func TestRecords_QuantityAndCategorySamples(t *testing.T) {
	t.Parallel()

	energy := `<Record type="HKQuantityTypeIdentifierBasalEnergyBurned" sourceName="Apple Watch de Alex" unit="kcal/hr·kg" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:01:00 +0100" value="1.25e-02"/>`
	doc := document(heartRate, energy, sleep)

	require.Equal(t, []ingest.Record{
		{Position: offset(t, doc, "<Record", 0), Quantity: &ingest.QuantitySample{
			Type:   "HKQuantityTypeIdentifierHeartRate",
			Start:  "2025-12-30T07:00:00+01:00",
			End:    "2025-12-30T07:00:00+01:00",
			Value:  62,
			Unit:   "count/min",
			Source: store.SourceInfo{Name: "Apple Watch de Alex"},
		}},
		{Position: offset(t, doc, "<Record", 1), Quantity: &ingest.QuantitySample{
			Type:   "HKQuantityTypeIdentifierBasalEnergyBurned",
			Start:  "2025-12-30T07:00:00+01:00",
			End:    "2025-12-30T07:01:00+01:00",
			Value:  0.0125,
			Unit:   "kcal/hr·kg",
			Source: store.SourceInfo{Name: "Apple Watch de Alex"},
		}},
		{Position: offset(t, doc, "<Record", 2), Category: &ingest.CategorySample{
			Type:   "HKCategoryTypeIdentifierSleepAnalysis",
			Start:  "2025-12-30T01:00:00+01:00",
			End:    "2025-12-30T01:30:00+01:00",
			Value:  "HKCategoryValueSleepAnalysisAsleepCore",
			Source: store.SourceInfo{Name: "Apple Watch de Alex"},
		}},
	}, readAll(t, doc))
}

// Given a quantity whose value is not a number, or a category without value, then it is an invalid
// record of its type, with a reason that names the attribute and never the value.
func TestRecords_InvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record string
		typ    string
		reason string
	}{
		{
			name:   "quantity with a word as value",
			record: `<Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Watch" unit="count/min" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:00:00 +0100" value="sixty"/>`,
			typ:    "HKQuantityTypeIdentifierHeartRate",
			reason: "value is not a number",
		},
		{
			name:   "quantity with an infinite value",
			record: `<Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Watch" unit="count/min" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:00:00 +0100" value="Inf"/>`,
			typ:    "HKQuantityTypeIdentifierHeartRate",
			reason: "value is not a number",
		},
		{
			name:   "quantity without value",
			record: `<Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Watch" unit="count/min" startDate="2025-12-30 07:00:00 +0100" endDate="2025-12-30 07:00:00 +0100"/>`,
			typ:    "HKQuantityTypeIdentifierHeartRate",
			reason: "value is missing",
		},
		{
			name:   "category without value",
			record: `<Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2025-12-30 01:00:00 +0100" endDate="2025-12-30 01:30:00 +0100"/>`,
			typ:    "HKCategoryTypeIdentifierSleepAnalysis",
			reason: "value is missing",
		},
		{
			name:   "record without type",
			record: `<Record sourceName="Watch" unit="count" startDate="2025-12-30 01:00:00 +0100" endDate="2025-12-30 01:30:00 +0100" value="1"/>`,
			reason: "type is missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := document(tt.record)

			records := readAll(t, doc)
			require.Equal(t, []ingest.Record{{
				Position: offset(t, doc, "<Record", 0),
				Invalid:  &ingest.Invalid{Of: ingest.SampleRecord, Type: tt.typ, Reason: tt.reason},
			}}, records)
			require.NotContains(t, records[0].Invalid.Reason, "sixty")
		})
	}
}

// recordAt returns a heart rate record whose startDate is start and endDate is end.
func recordAt(start, end string) string {
	return `<Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Watch" unit="count/min" startDate="` +
		start + `" endDate="` + end + `" value="62"/>`
}

// Given a date yyyy-MM-dd HH:mm:ss Z, then the record holds it as RFC 3339 with the same instant
// and offset. Given each tolerated variant, then it is read the same way.
func TestRecords_DatesAndTheirTolerances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		date string
		want string
	}{
		{name: "format of the export", date: "2025-12-30 07:00:00 +0100", want: "2025-12-30T07:00:00+01:00"},
		{name: "negative offset", date: "2025-07-14 22:30:05 -0430", want: "2025-07-14T22:30:05-04:30"},
		{name: "offset with a colon", date: "2025-12-30 07:00:00 +01:00", want: "2025-12-30T07:00:00+01:00"},
		{name: "offset Z", date: "2025-12-30 06:00:00 Z", want: "2025-12-30T06:00:00Z"},
		{name: "fractional seconds", date: "2025-12-30 07:00:00.25 +0100", want: "2025-12-30T07:00:00.25+01:00"},
		{name: "one-digit hour", date: "2025-12-30 7:00:00 +0100", want: "2025-12-30T07:00:00+01:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			records := readAll(t, document(recordAt(tt.date, tt.date)))
			require.Len(t, records, 1)
			require.NotNil(t, records[0].Quantity)
			require.Equal(t, tt.want, records[0].Quantity.Start)
			require.Equal(t, tt.want, records[0].Quantity.End)
		})
	}
}

// Given any other date in startDate or endDate, then the record is invalid, with a reason naming
// the attribute.
func TestRecords_OtherDatesMakeTheRecordInvalid(t *testing.T) {
	t.Parallel()

	const valid = "2025-12-30 07:00:00 +0100"

	tests := []struct {
		name       string
		start, end string
		reason     string
	}{
		{name: "RFC 3339 start", start: "2025-12-30T07:00:00+01:00", end: valid, reason: "startDate is not a date"},
		{name: "day first start", start: "30/12/2025 07:00:00 +0100", end: valid, reason: "startDate is not a date"},
		{name: "start without offset", start: "2025-12-30 07:00:00", end: valid, reason: "startDate is not a date"},
		{name: "empty start", start: "", end: valid, reason: "startDate is not a date"},
		{name: "end without seconds", start: valid, end: "2025-12-30 07:00 +0100", reason: "endDate is not a date"},
		{name: "end out of range", start: valid, end: "2025-13-30 07:00:00 +0100", reason: "endDate is not a date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, []ingest.Record{{
				Position: int64(len(head)) + 1,
				Invalid: &ingest.Invalid{
					Of: ingest.SampleRecord, Type: "HKQuantityTypeIdentifierHeartRate", Reason: tt.reason,
				},
			}}, readAll(t, document(recordAt(tt.start, tt.end))))
		})
	}

	t.Run("missing dates", func(t *testing.T) {
		t.Parallel()

		records := readAll(t, document(`<Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Watch" unit="count/min" value="62"/>`))
		require.Len(t, records, 1)
		require.Equal(t, &ingest.Invalid{
			Of: ingest.SampleRecord, Type: "HKQuantityTypeIdentifierHeartRate", Reason: "startDate is missing",
		}, records[0].Invalid)
	})
}

// Given a truncated document or malformed XML outside the DTD, then the read ends with an error
// that errors.Is reports as applexml.ErrMalformed, after the records that came before it.
func TestRecords_TruncatedOrMalformedDocument(t *testing.T) {
	t.Parallel()

	whole := document(heartRate, steps)
	inSecond := offset(t, whole, "<Record", 1) + 40

	tests := []struct {
		name string
		doc  string
	}{
		{name: "truncated in a record", doc: whole[:inSecond]},
		{name: "truncated before the end of the root", doc: strings.TrimSuffix(document(heartRate), "</HealthData>\n")},
		{name: "mismatched end tag", doc: document(heartRate, `<Record type="X" sourceName="Watch" startDate="" endDate="" value="1"></Correlation>`)},
		{name: "attribute without quotes", doc: document(heartRate, `<Record type=X/>`)},
		{name: "unknown entity", doc: document(heartRate, `<Record type="&nope;"/>`)},
		{name: "second root element", doc: document(heartRate) + "<HealthData/>\n"},
		{name: "text after the root element", doc: document(heartRate) + "records\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			records, err := read(t, tt.doc)
			require.ErrorIs(t, err, applexml.ErrMalformed)
			require.Len(t, records, 1, "the records before the error come first")
			require.Equal(t, "HKQuantityTypeIdentifierHeartRate", records[0].Quantity.Type)
		})
	}

	t.Run("empty document", func(t *testing.T) {
		t.Parallel()

		records, err := read(t, "")
		require.ErrorIs(t, err, applexml.ErrMalformed)
		require.Empty(t, records)
	})
}

// failingReader yields its data, then fails with err.
type failingReader struct {
	data string
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}

	n := copy(p, r.data)
	r.data = r.data[n:]

	return n, nil
}

// Given a reader that fails, then the read ends with its error, which is not
// applexml.ErrMalformed, since a read error of the disk is retried (FR-5 of XML import).
func TestRecords_ReadErrorIsNotMalformed(t *testing.T) {
	t.Parallel()

	errDisk := errors.New("disk failed")
	whole := document(heartRate, steps)

	records, err := readFrom(t, &failingReader{data: whole[:offset(t, whole, "<Record", 1)+40], err: errDisk})
	require.ErrorIs(t, err, errDisk)
	require.NotErrorIs(t, err, applexml.ErrMalformed)
	require.Len(t, records, 1)
}
