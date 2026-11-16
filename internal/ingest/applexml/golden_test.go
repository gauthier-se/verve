package applexml_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/ingest/applexml"
	"github.com/gauthier-se/verve/internal/synthexport"
)

// update rewrites the golden files, whose diff then shows every change of the reading.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// line is what a golden line holds of a record: the kinds it lacks are left out.
type line struct {
	Position    int64
	Quantity    *ingest.QuantitySample `json:",omitempty"`
	Category    *ingest.CategorySample `json:",omitempty"`
	Workout     *ingest.Workout        `json:",omitempty"`
	Invalid     *ingest.Invalid        `json:",omitempty"`
	Unsupported *ingest.Unsupported    `json:",omitempty"`
	Warning     string                 `json:",omitempty"`
}

// openExport opens the export of the zip at path, whose file is closed at the end of the test.
func openExport(t testing.TB, path string) *applexml.Export {
	t.Helper()

	f, err := os.Open(path) //nolint:gosec // G304: a file of the temporary directory of the test
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, f.Close()) })

	e, err := applexml.Open(t.Context(), f)
	require.NoError(t, err)

	return e
}

// Given the fixture of #138 written as a zip by synthexport.Write, when its records are read, then
// they equal a golden file in testdata, one normalized record per line, with the routes of its two
// runs, and the strength sessions have none. -update rewrites it.
func TestRecords_FixtureZip_MatchesTheGoldenFile(t *testing.T) {
	t.Parallel()

	e := openExport(t, writeExport(t, t.TempDir(), synthexport.Fixture()))

	var (
		got    bytes.Buffer
		routed []string
	)

	enc := json.NewEncoder(&got)
	enc.SetEscapeHTML(false)

	for r, err := range e.Records(t.Context()) {
		require.NoError(t, err)
		require.NoError(t, enc.Encode(line{
			Position: r.Position, Quantity: r.Quantity, Category: r.Category, Workout: r.Workout,
			Invalid: r.Invalid, Unsupported: r.Unsupported, Warning: r.Warning,
		}))

		if r.Workout != nil && r.Workout.Route != nil {
			routed = append(routed, r.Workout.ActivityType+" "+r.Workout.Start)
		}
	}

	require.Equal(t, []string{
		"HKWorkoutActivityTypeRunning 2025-12-30T07:00:00+01:00",
		"HKWorkoutActivityTypeWalking 2026-01-03T07:28:00+01:00",
	}, routed, "the strength sessions and the copy of Strava have no route")

	path := filepath.Join("testdata", "export.golden")

	if *update {
		require.NoError(t, os.WriteFile(path, got.Bytes(), 0o600)) //nolint:gosec // G703: a golden file of testdata
	}

	want, err := os.ReadFile(path) //nolint:gosec // G304: a golden file of testdata
	require.NoError(t, err)
	require.Equal(t, string(want), got.String(), "%s differs, run the tests with -update to rewrite it", path)
}

// Given a context cancelled during the read, then it stops with the error of the context.
func TestRecords_CancelledContext_StopsWithItsError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var records int

	for _, err := range applexml.Records(ctx, bytes.NewReader([]byte(document(heartRate, steps, sleep)))) {
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 1, records, "the read stops at the next element")

			return
		}

		records++

		cancel()
	}

	t.Fatal("the read ended without the error of the context")
}

// largeExport writes a generated export of tens of megabytes to a file of dir, and returns the
// path of its zip.
func largeExport(t testing.TB, dir string) string {
	t.Helper()

	opts := synthexport.Fixture()
	opts.Days = 30
	opts.Density = 12

	return writeExport(t, dir, opts)
}

// writeExport writes the generated export of opts to a file of dir, and returns the path of its
// zip.
func writeExport(t testing.TB, dir string, opts synthexport.Options) string {
	t.Helper()

	path := filepath.Join(dir, "export.zip")

	f, err := os.Create(path) //nolint:gosec // G304: a file of the temporary directory of the test
	require.NoError(t, err)
	require.NoError(t, synthexport.Write(t.Context(), f, opts))
	require.NoError(t, f.Close())

	return path
}

// openExportXML opens the export.xml entry of the zip at path, closed at the end of the test, and
// returns it with its uncompressed size.
func openExportXML(t testing.TB, path string) (io.Reader, int64) {
	t.Helper()

	zr, err := zip.OpenReader(path)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, zr.Close()) })

	for _, entry := range zr.File {
		if filepath.Base(entry.Name) != "export.xml" {
			continue
		}

		r, err := entry.Open()
		require.NoError(t, err)

		t.Cleanup(func() { require.NoError(t, r.Close()) })

		return r, int64(entry.UncompressedSize64) //nolint:gosec // G115: the size of a generated entry
	}

	t.Fatal("the export has no export.xml")

	return nil, 0
}

// heapWatcher measures the live heap while the records of an export are read, every 512 KiB of
// their positions.
type heapWatcher struct {
	next int64
	peak uint64
}

// at measures the live heap when the record read is at position.
func (w *heapWatcher) at(position int64) {
	const checkEvery = 512 << 10

	if position < w.next {
		return
	}

	w.next = position + checkEvery

	var stats runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&stats)
	w.peak = max(w.peak, stats.HeapAlloc)
}

// Given a generated export of several tens of megabytes as a zip, then the live heap stays under
// 16 MiB during the read, routes included. Not parallel, since other tests would count in the heap.
func TestRecords_LargeExport_KeepsTheLiveHeapBounded(t *testing.T) {
	const (
		ceiling = 16 << 20
		minSize = 20 << 20
	)

	e := openExport(t, largeExport(t, t.TempDir()))
	require.Greater(t, e.Size(), int64(minSize))

	var (
		watcher        heapWatcher
		records, route int
		last           int64
	)

	for r, err := range e.Records(t.Context()) {
		require.NoError(t, err)

		watcher.at(r.Position)

		records++
		last = r.Position

		if r.Workout != nil {
			route += len(r.Workout.Route)
		}
	}

	require.Greater(t, last, e.Size(), "the whole document is read, then the electrocardiograms")
	require.Positive(t, records)
	require.Positive(t, route, "the routes are read")
	require.NotZero(t, watcher.peak)
	require.Less(t, watcher.peak, uint64(ceiling))
}

// BenchmarkRecords reads a generated export of tens of megabytes held in memory, and reports its
// throughput.
func BenchmarkRecords(b *testing.B) {
	r, size := openExportXML(b, largeExport(b, b.TempDir()))

	doc, err := io.ReadAll(r)
	require.NoError(b, err)

	b.SetBytes(size)

	for b.Loop() {
		for _, err := range applexml.Records(b.Context(), bytes.NewReader(doc)) {
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
