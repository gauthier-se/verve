package applexml_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
)

// entry is a file of a zip written by a test.
type entry struct {
	name, body string
	// stored writes the entry without compression.
	stored bool
}

// zipOf returns the bytes of a zip of entries, in central directory order. Every entry has a data
// descriptor, as zip.Writer.Create writes it: its local header holds no size.
func zipOf(t *testing.T, entries ...entry) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		method := zip.Deflate
		if e.stored {
			method = zip.Store
		}

		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: method})
		require.NoError(t, err)

		_, err = io.WriteString(w, e.body)
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())

	return buf.Bytes()
}

// fileOf writes data to a file of a temporary directory, and returns it opened, closed at the end
// of the test.
func fileOf(t *testing.T, data []byte) *os.File {
	t.Helper()

	path := filepath.Join(t.TempDir(), "payload")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	f, err := os.Open(path) //nolint:gosec // G304: a file of the temporary directory of the test
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	return f
}

// open opens the export of data.
func open(t *testing.T, data []byte) (*applexml.Export, error) {
	t.Helper()

	return applexml.Open(t.Context(), fileOf(t, data))
}

// recordsOf reads the records of e until the end or the first error.
func recordsOf(t *testing.T, e *applexml.Export) ([]ingest.Record, error) {
	t.Helper()

	var records []ingest.Record

	for record, err := range e.Records(t.Context()) {
		if err != nil {
			return records, err
		}

		records = append(records, record)
	}

	return records, nil
}

// Given a file that starts with a zip local file header, then it is read as a zip. Given one whose
// first non-blank byte is <, then as a bare export.xml. Given any other, then opening it fails with
// applexml.ErrNotAnExport, whose message is the one of the report.
func TestOpen_ZipOrBareExportXMLOrNeither(t *testing.T) {
	t.Parallel()

	doc := document(heartRate, steps)
	want := readAll(t, doc)

	t.Run("zip", func(t *testing.T) {
		t.Parallel()

		e, err := open(t, zipOf(t, entry{name: "apple_health_export/export.xml", body: doc}))
		require.NoError(t, err)

		records, err := recordsOf(t, e)
		require.NoError(t, err)
		require.Equal(t, want, records, "positions are offsets in the uncompressed export.xml")
	})

	for name, blank := range map[string]string{"bare export.xml": "", "bare export.xml after blanks": " \t\r\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e, err := open(t, []byte(blank+doc))
			require.NoError(t, err)

			records, err := recordsOf(t, e)
			require.NoError(t, err)
			require.Len(t, records, len(want))

			for i, r := range records {
				require.Equal(t, want[i].Position+int64(len(blank)), r.Position)
				require.Equal(t, want[i].Quantity, r.Quantity)
			}
		})
	}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty file", data: nil},
		{name: "blanks only", data: []byte(" \n\t\r\n")},
		{name: "JSON document", data: []byte(`{"data": {"metrics": []}}`)},
		{name: "text before the XML", data: []byte("export\n" + doc)},
		{name: "byte order mark", data: append([]byte("\xef\xbb\xbf"), doc...)},
		{name: "empty zip", data: zipOf(t)[:4]},
		{name: "gzip", data: []byte("\x1f\x8b\x08\x00")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := open(t, tt.data)
			require.ErrorIs(t, err, applexml.ErrNotAnExport)
			require.EqualError(t, err, "the file is not an Apple Health export")
		})
	}
}

// Given a zip, then the export is the first entry of the central directory named export.xml, at
// the root or one folder deep whatever its name. Given a zip without it, or with it two folders
// deep only, then opening fails with applexml.ErrNotAnExport.
func TestOpen_FindsExportXMLAtTheRootOrOneFolderDeep(t *testing.T) {
	t.Parallel()

	first, second := document(heartRate), document(steps)

	found := []struct {
		name    string
		entries []entry
	}{
		{name: "at the root", entries: []entry{{name: "export.xml", body: first}}},
		{name: "in apple_health_export", entries: []entry{{name: "apple_health_export/export.xml", body: first}}},
		{name: "in a folder of any name", entries: []entry{{name: "Exportación de Salud/export.xml", body: first}}},
		{name: "first of two", entries: []entry{
			{name: "a/export_cda.xml", body: second},
			{name: "a/export.xml", body: first},
			{name: "b/export.xml", body: second},
		}},
		{name: "after a deeper one", entries: []entry{
			{name: "a/b/export.xml", body: second},
			{name: "a/export.xml", body: first},
		}},
	}

	for _, tt := range found {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, err := open(t, zipOf(t, tt.entries...))
			require.NoError(t, err)

			records, err := recordsOf(t, e)
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "HKQuantityTypeIdentifierHeartRate", records[0].Quantity.Type)
		})
	}

	missing := []struct {
		name    string
		entries []entry
	}{
		{name: "no entry", entries: nil},
		{name: "other entries only", entries: []entry{
			{name: "apple_health_export/export_cda.xml", body: first},
			{name: "apple_health_export/workout-routes/route.gpx", body: "<gpx/>"},
		}},
		{name: "two folders deep only", entries: []entry{{name: "a/apple_health_export/export.xml", body: first}}},
		{name: "a folder named export.xml", entries: []entry{{name: "export.xml/", body: ""}}},
		{name: "another name", entries: []entry{{name: "apple_health_export/export.xml.bak", body: first}}},
	}

	for _, tt := range missing {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := open(t, zipOf(t, tt.entries...))
			require.ErrorIs(t, err, applexml.ErrNotAnExport)
		})
	}
}

// Given a zip with data descriptors, then the size of the export is the uncompressed size of its
// central directory entry, and for a bare export.xml the size of the file.
func TestOpen_SizeIsTheUncompressedSizeOfExportXML(t *testing.T) {
	t.Parallel()

	doc := document(heartRate, steps, sleep)

	t.Run("zip", func(t *testing.T) {
		t.Parallel()

		data := zipOf(t,
			entry{name: "apple_health_export/export_cda.xml", body: strings.Repeat("x", 1000)},
			entry{name: "apple_health_export/export.xml", body: doc},
		)
		require.Equal(t, uint16(0x8), uint16(data[6])&0x8, "the local header defers its sizes to a data descriptor")

		e, err := open(t, data)
		require.NoError(t, err)
		require.Equal(t, int64(len(doc)), e.Size())
	})

	t.Run("bare export.xml", func(t *testing.T) {
		t.Parallel()

		e, err := open(t, []byte("\n"+doc))
		require.NoError(t, err)
		require.Equal(t, int64(len(doc)+1), e.Size())
	})
}

// corrupt returns data with size bytes overwritten by 0xff from at.
func corrupt(data []byte, at, size int) []byte {
	out := bytes.Clone(data)

	for i := range size {
		out[at+i] = 0xff
	}

	return out
}

// dataOffset returns the offset in data of the content of the zip entry name.
func dataOffset(t *testing.T, data []byte, name string) int {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	for _, f := range zr.File {
		if f.Name == name {
			offset, err := f.DataOffset()
			require.NoError(t, err)

			return int(offset)
		}
	}

	t.Fatalf("the zip has no entry %s", name)

	return 0
}

// Given a corrupted entry or a corrupted central directory, then the read ends with
// applexml.ErrMalformed.
func TestOpen_CorruptedZipIsMalformed(t *testing.T) {
	t.Parallel()

	const name = "apple_health_export/export.xml"

	doc := document(heartRate, steps, sleep, heartRate, steps, sleep)

	t.Run("corrupted compressed data", func(t *testing.T) {
		t.Parallel()

		data := zipOf(t, entry{name: name, body: doc})
		at := dataOffset(t, data, name)

		e, err := open(t, corrupt(data, at+20, 16))
		require.NoError(t, err)

		_, err = recordsOf(t, e)
		require.ErrorIs(t, err, applexml.ErrMalformed)
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		t.Parallel()

		data := zipOf(t, entry{name: name, body: doc, stored: true})
		at := dataOffset(t, data, name) + strings.Index(doc, `value="62"`) + len(`value="`)
		data[at] = '7'

		e, err := open(t, data)
		require.NoError(t, err)

		records, err := recordsOf(t, e)
		require.ErrorIs(t, err, applexml.ErrMalformed)
		require.NotEmpty(t, records, "the records before the end of the entry come first")
	})

	t.Run("corrupted central directory", func(t *testing.T) {
		t.Parallel()

		data := zipOf(t, entry{name: name, body: doc})
		directory := bytes.LastIndex(data, []byte("PK\x01\x02"))

		_, err := open(t, corrupt(data, directory, 4))
		require.ErrorIs(t, err, applexml.ErrMalformed)
	})

	t.Run("truncated zip", func(t *testing.T) {
		t.Parallel()

		data := zipOf(t, entry{name: name, body: doc})

		_, err := open(t, data[:len(data)/2])
		require.ErrorIs(t, err, applexml.ErrMalformed)
	})
}

// Given a file that fails to read once opened, then the read ends with its error, which is not
// applexml.ErrMalformed, since a read error of the disk is retried (FR-5 of XML import).
func TestOpen_ReadErrorIsNotMalformed(t *testing.T) {
	t.Parallel()

	doc := document(heartRate, steps)

	for name, data := range map[string][]byte{
		"zip":             zipOf(t, entry{name: "apple_health_export/export.xml", body: doc}),
		"bare export.xml": []byte(doc),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := fileOf(t, data)

			e, err := applexml.Open(t.Context(), f)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			_, err = recordsOf(t, e)
			require.ErrorIs(t, err, os.ErrClosed)
			require.NotErrorIs(t, err, applexml.ErrMalformed)
		})
	}
}
