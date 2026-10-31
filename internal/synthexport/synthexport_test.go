package synthexport_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // for Europe/Paris, on a machine without a time zone database
	"unicode"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/synthexport"
)

const (
	exportXMLName = "apple_health_export/export.xml"
	hrvType       = "HKQuantityTypeIdentifierHeartRateVariabilitySDNN"
	stepType      = "HKQuantityTypeIdentifierStepCount"
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

// Given a cancelled context, then Write stops with its error.
func TestWrite_CancelledContext_StopsWithItsError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, synthexport.Write(ctx, io.Discard, testOptions()), context.Canceled)
}

// failingWriter refuses every write.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// Given a writer that fails, then Write returns its error.
func TestWrite_FailingWriter_ReturnsItsError(t *testing.T) {
	t.Parallel()

	errFull := errors.New("disk full")

	require.ErrorIs(t, synthexport.Write(t.Context(), failingWriter{errFull}, testOptions()), errFull)
}

// exportElements returns the elements of export.xml for opts.
func exportElements(t *testing.T, opts synthexport.Options) []element {
	t.Helper()

	return readElements(t, readEntry(t, generate(t, opts), exportXMLName))
}

// withPath keeps the elements at one path.
func withPath(elements []element, path string) []element {
	var kept []element

	for _, e := range elements {
		if e.path == path {
			kept = append(kept, e)
		}
	}

	return kept
}

// Given the records, then each type is one contiguous block, not sorted by date across sources.
func TestWrite_Records_EachTypeIsOneBlockNotSortedAcrossSources(t *testing.T) {
	t.Parallel()

	var (
		records = withPath(exportElements(t, testOptions()), "HealthData/Record")
		types   []string
		steps   []element
	)

	for _, r := range records {
		types = append(types, r.attrs["type"])

		if r.attrs["type"] == stepType {
			steps = append(steps, r)
		}
	}

	blocks := slices.Compact(slices.Clone(types))
	slices.Sort(types)
	require.Len(t, blocks, len(slices.Compact(types)))

	goesBack := false
	for i := 1; i < len(steps); i++ {
		goesBack = goesBack || steps[i].attrs["startDate"] < steps[i-1].attrs["startDate"]
	}

	require.True(t, goesBack, "the block of step counts is sorted by date")
}

// Given dates of another season, then they still have the format of a real export and the offset of
// the export, which is not the offset of their own date.
func TestWrite_Dates_CarryTheOffsetOfTheExportInEverySeason(t *testing.T) {
	t.Parallel()

	paris, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)

	opts := testOptions()
	opts.End = time.Date(2026, time.October, 31, 9, 0, 0, 0, paris) // the clocks went back on October 25
	opts.Days = 10

	format := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \+0100$`)
	inSummerTime := false

	for _, e := range exportElements(t, opts) {
		for name, value := range e.attrs {
			isDate := name == "creationDate" || name == "startDate" || name == "endDate" || name == "date" ||
				e.path == "HealthData/ExportDate"
			if !isDate {
				continue
			}

			require.Regexp(t, format, value, "%s of %s", name, e.path)

			inSummerTime = inSummerTime || strings.HasPrefix(value, "2026-10-21 ")
		}
	}

	require.True(t, inSummerTime, "no date before the clocks went back")
}

// Given the records, then they cover the shapes of a real export.
func TestWrite_Records_CoverTheShapesOfTheFormat(t *testing.T) {
	t.Parallel()

	instant := func(r element) bool { return r.attrs["startDate"] == r.attrs["endDate"] }

	shapes := map[string]func(r element) bool{
		"instant quantity": func(r element) bool {
			return r.attrs["type"] == "HKQuantityTypeIdentifierHeartRate" && r.attrs["unit"] == "count/min" && instant(r)
		},
		"interval quantity": func(r element) bool {
			return r.attrs["type"] == stepType && r.attrs["unit"] == "count" && !instant(r)
		},
		"category without unit": func(r element) bool {
			_, hasUnit := r.attrs["unit"]
			return r.attrs["type"] == "HKCategoryTypeIdentifierSleepAnalysis" && !hasUnit &&
				strings.HasPrefix(r.attrs["value"], "HKCategoryValueSleepAnalysis")
		},
		"percentage as a fraction": func(r element) bool {
			value, err := strconv.ParseFloat(r.attrs["value"], 64)
			return r.attrs["unit"] == "%" && err == nil && value > 0 && value <= 1
		},
		"value in scientific notation": func(r element) bool {
			return regexp.MustCompile(`^\d(\.\d+)?e-\d+$`).MatchString(r.attrs["value"])
		},
		"zero": func(r element) bool { return r.attrs["value"] == "0" },
		"unit with a non-ASCII character": func(r element) bool {
			return r.attrs["unit"] == "kcal/hr·kg"
		},
		"source with a non-ASCII character": func(r element) bool {
			return strings.ContainsFunc(r.attrs["sourceName"], func(c rune) bool { return c > unicode.MaxASCII })
		},
		"without sourceVersion": func(r element) bool {
			_, hasVersion := r.attrs["sourceVersion"]
			return !hasVersion
		},
		"type outside the prefixes of the catalog": func(r element) bool {
			return r.attrs["type"] == "HKDataTypeSleepDurationGoal" && r.attrs["unit"] == "hr"
		},
	}

	records := withPath(exportElements(t, testOptions()), "HealthData/Record")

	for name, matches := range shapes {
		require.True(t, slices.ContainsFunc(records, matches), name)
	}
}

// Given the step counts, then two sources overlap in time, which source priority has to resolve.
func TestWrite_StepCounts_OfTwoSourcesOverlapInTime(t *testing.T) {
	t.Parallel()

	steps := withPath(exportElements(t, testOptions()), "HealthData/Record")
	steps = slices.DeleteFunc(steps, func(r element) bool { return r.attrs["type"] != stepType })

	overlaps := slices.ContainsFunc(steps, func(a element) bool {
		return slices.ContainsFunc(steps, func(b element) bool {
			return a.attrs["sourceName"] != b.attrs["sourceName"] &&
				a.attrs["startDate"] < b.attrs["endDate"] && b.attrs["startDate"] < a.attrs["endDate"]
		})
	})

	require.True(t, overlaps)
}

// Given the device attributes, then they have the five shapes of the format, and they are escaped.
func TestWrite_Devices_HaveTheFiveShapesOfTheFormat(t *testing.T) {
	t.Parallel()

	var (
		content = readEntry(t, generate(t, testOptions()), exportXMLName)
		device  = regexp.MustCompile(`^<<HKDevice: 0x[0-9a-f]+>, (.*)>$`)
		shapes  = map[string]bool{}
	)

	for _, e := range readElements(t, content) {
		value, ok := e.attrs["device"]
		if !ok {
			continue
		}

		match := device.FindStringSubmatch(value)
		require.NotNil(t, match, value)

		var names []string

		for field := range strings.SplitSeq(match[1], ", ") {
			name, _, found := strings.Cut(field, ":")
			require.True(t, found, value)

			names = append(names, name)
		}

		shapes[strings.Join(names, ",")] = true
	}

	require.ElementsMatch(t, []string{
		"name,manufacturer,model,hardware,software,creation date",
		"name,bluetoothIdentifier,manufacturer,model,hardware,firmware,creation date",
		"name,manufacturer,model,localIdentifier,creation date",
		"name,localIdentifier,creation date",
		"name,manufacturer,localIdentifier,creation date",
	}, slices.Collect(maps.Keys(shapes)))
	require.Contains(t, string(content), `device="&lt;&lt;HKDevice: 0x`)
}

// Given the metadata, then it covers the shapes of a real export.
func TestWrite_Metadata_CoversTheShapesOfTheFormat(t *testing.T) {
	t.Parallel()

	shapes := map[string]func(key, value string) bool{
		"HealthKit key": func(key, _ string) bool { return key == "HKMetadataKeyHeartRateMotionContext" },
		"key written by an app": func(key, _ string) bool {
			return !strings.HasPrefix(key, "HK")
		},
		"non-ASCII value": func(_, value string) bool {
			return strings.ContainsFunc(value, func(c rune) bool { return c > unicode.MaxASCII })
		},
		"value carrying a unit": func(_, value string) bool {
			return regexp.MustCompile(`^\d+(\.\d+)? (kPa|count/min)$`).MatchString(value)
		},
		"time zone": func(key, value string) bool { return key == "HKTimeZone" && value == "Europe/Paris" },
	}

	entries := withPath(exportElements(t, testOptions()), "HealthData/Record/MetadataEntry")

	for name, matches := range shapes {
		found := slices.ContainsFunc(entries, func(e element) bool { return matches(e.attrs["key"], e.attrs["value"]) })
		require.True(t, found, name)
	}
}

// Given a heart rate variability record, then it lists its beats, whose time has a comma before its
// hundredths of a second.
func TestWrite_HeartRateVariability_ListsBeatsWithACommaInTheirTime(t *testing.T) {
	t.Parallel()

	const list = "HealthData/Record/HeartRateVariabilityMetadataList"

	var (
		format = regexp.MustCompile(`^\d{2}:\d{2}:\d{2},\d{2}$`)
		record element
		lists  int
		beats  int
	)

	for _, e := range exportElements(t, testOptions()) {
		switch e.path {
		case "HealthData/Record":
			record = e
		case list:
			lists++

			require.Equal(t, hrvType, record.attrs["type"])
		case list + "/InstantaneousBeatsPerMinute":
			beats++

			require.Regexp(t, format, e.attrs["time"])
			require.Regexp(t, `^\d{2,3}$`, e.attrs["bpm"])
		}
	}

	require.Equal(t, testOptions().Days, lists)
	require.Greater(t, beats, lists)
}
