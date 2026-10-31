package synthexport_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/synthexport"
)

const (
	exportXMLName = "apple_health_export/export.xml"
	hrvType       = "HKQuantityTypeIdentifierHeartRateVariabilitySDNN"
)

// testOptions describes a small export, so that a test runs in milliseconds.
func testOptions() synthexport.Options {
	return synthexport.Options{
		Seed:    1,
		End:     time.Date(2026, time.January, 5, 9, 0, 0, 0, time.FixedZone("", 3600)),
		Days:    8,
		Density: 1,
	}
}

// generate returns the zip written for opts.
func generate(t *testing.T, opts synthexport.Options) []byte {
	t.Helper()

	var archive bytes.Buffer
	require.NoError(t, synthexport.Write(t.Context(), &archive, opts))

	return archive.Bytes()
}

// openZip reads the central directory of archive.
func openZip(t *testing.T, archive []byte) *zip.Reader {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)

	return zr
}

// readEntry returns the content of one entry of archive.
func readEntry(t *testing.T, archive []byte, name string) []byte {
	t.Helper()

	f, err := openZip(t, archive).Open(name)
	require.NoError(t, err)

	defer func() { require.NoError(t, f.Close()) }()

	content, err := io.ReadAll(f)
	require.NoError(t, err)

	return content
}

// element is a start tag of an XML document.
type element struct {
	path  string // names from the root, joined by slashes
	attrs map[string]string
}

// readElements decodes content with the strict decoder of encoding/xml, which fails on a document
// that is not well formed.
func readElements(t *testing.T, content []byte) []element {
	t.Helper()

	var (
		dec      = xml.NewDecoder(bytes.NewReader(content))
		stack    []string
		elements []element
	)

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return elements
		}

		require.NoError(t, err)

		switch tok := tok.(type) {
		case xml.StartElement:
			stack = append(stack, tok.Name.Local)

			attrs := make(map[string]string, len(tok.Attr))
			for _, a := range tok.Attr {
				attrs[a.Name.Local] = a.Value
			}

			elements = append(elements, element{path: strings.Join(stack, "/"), attrs: attrs})
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

// shapeOf returns the distinct element paths with their attribute names, which values do not change.
func shapeOf(elements []element) []string {
	seen := map[string]bool{}

	for _, e := range elements {
		names := make([]string, 0, len(e.attrs))
		for name := range e.attrs {
			names = append(names, name)
		}

		sort.Strings(names)
		seen[fmt.Sprintf("%s %v", e.path, names)] = true
	}

	shape := make([]string, 0, len(seen))
	for s := range seen {
		shape = append(shape, s)
	}

	sort.Strings(shape)

	return shape
}

// Given the same options, when Write runs twice, then the bytes are identical.
func TestWrite_SameOptions_WritesIdenticalBytes(t *testing.T) {
	t.Parallel()

	first := generate(t, testOptions())

	require.NotEmpty(t, first)
	require.Equal(t, first, generate(t, testOptions()))
}

// Given another seed, then the values differ and the structure stays.
func TestWrite_AnotherSeed_ChangesValuesAndKeepsStructure(t *testing.T) {
	t.Parallel()

	other := testOptions()
	other.Seed = 2

	first := readEntry(t, generate(t, testOptions()), exportXMLName)
	second := readEntry(t, generate(t, other), exportXMLName)

	require.NotEqual(t, first, second)
	require.Equal(t, shapeOf(readElements(t, first)), shapeOf(readElements(t, second)))
}

// Given an export, then the zip holds, in this order, export.xml, the routes, the electrocardiograms
// and the clinical document, deflated, with data descriptors.
func TestWrite_Zip_HasTheLayoutOfARealExport(t *testing.T) {
	t.Parallel()

	groups := []*regexp.Regexp{
		regexp.MustCompile(`^apple_health_export/export\.xml$`),
		regexp.MustCompile(`^apple_health_export/workout-routes/route_\d{4}-\d{2}-\d{2}_\d{1,2}\.\d{2}[ap]m\.gpx$`),
		regexp.MustCompile(`^apple_health_export/electrocardiograms/ecg_\d{4}-\d{2}-\d{2}(_\d+)?\.csv$`),
		regexp.MustCompile(`^apple_health_export/export_cda\.xml$`),
	}

	const dataDescriptorFlag = 0x8

	var order []int

	for _, f := range openZip(t, generate(t, testOptions())).File {
		group := slices.IndexFunc(groups, func(r *regexp.Regexp) bool { return r.MatchString(f.Name) })
		require.NotEqual(t, -1, group, "unexpected entry %s", f.Name)
		require.Equal(t, zip.Deflate, f.Method, f.Name)
		require.NotZero(t, f.Flags&dataDescriptorFlag, f.Name)

		order = append(order, group)
	}

	require.Equal(t, []int{0, 1, 2, 3}, slices.Compact(order))
}

// Given export.xml, then it starts with the prolog and an inline DTD holding the export version.
func TestWrite_ExportXML_StartsWithThePrologAndTheDTD(t *testing.T) {
	t.Parallel()

	content := string(readEntry(t, generate(t, testOptions()), exportXMLName))

	require.True(t, strings.HasPrefix(content, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE HealthData [
<!-- HealthKit Export Version: 17 -->
`), "the document starts with %.80q", content)
	require.Contains(t, content, "]>\n<HealthData locale=\"")
}

// Given export.xml, then a strict decoder reads it, and the top level comes in the order of a real
// export, the last group being the heart rate variability records.
func TestWrite_ExportXML_TopLevelComesInTheOrderOfARealExport(t *testing.T) {
	t.Parallel()

	var (
		names         []string
		afterSummary  []string
		summaryPassed bool
	)

	for _, e := range readElements(t, readEntry(t, generate(t, testOptions()), exportXMLName)) {
		name, isTopLevel := strings.CutPrefix(e.path, "HealthData/")
		if !isTopLevel || strings.Contains(name, "/") {
			continue
		}

		names = append(names, name)

		if summaryPassed && name == "Record" {
			afterSummary = append(afterSummary, e.attrs["type"])
		}

		summaryPassed = summaryPassed || name == "ActivitySummary"
	}

	require.Equal(t,
		[]string{"ExportDate", "Me", "Record", "Correlation", "Workout", "ActivitySummary", "Record"},
		slices.Compact(names))
	require.Equal(t, []string{hrvType}, slices.Compact(afterSummary))
}

// Given Days or Density under 1, then Write fails before writing anything.
func TestWrite_InvalidOptions_FailsBeforeWritingAnything(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		change func(*synthexport.Options)
		want   string
	}{
		"no days":          {func(o *synthexport.Options) { o.Days = 0 }, "days must be at least 1"},
		"negative days":    {func(o *synthexport.Options) { o.Days = -1 }, "days must be at least 1"},
		"no density":       {func(o *synthexport.Options) { o.Density = 0 }, "density must be at least 1"},
		"negative density": {func(o *synthexport.Options) { o.Density = -1 }, "density must be at least 1"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			opts := testOptions()
			tt.change(&opts)

			var archive bytes.Buffer

			require.ErrorContains(t, synthexport.Write(t.Context(), &archive, opts), tt.want)
			require.Zero(t, archive.Len())
		})
	}
}
