package synthexport_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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
	runningType   = "HKWorkoutActivityTypeRunning"
	strengthType  = "HKWorkoutActivityTypeTraditionalStrengthTraining"
	workoutPath   = "HealthData/Workout"
	dateLayout    = "2006-01-02 15:04:05 -0700"
)

// update rewrites the golden tree, whose diff then shows every change of the format.
var update = flag.Bool("update", false, "rewrite the golden tree in testdata")

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
	text  string // the text right after the start tag
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
		case xml.CharData:
			if len(elements) > 0 && elements[len(elements)-1].path == strings.Join(stack, "/") {
				elements[len(elements)-1].text = string(tok)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

// structureOf returns every element, in order, with its attribute names, which values do not change.
func structureOf(elements []element) []string {
	structure := make([]string, 0, len(elements))

	for _, e := range elements {
		structure = append(structure, fmt.Sprintf("%s %v", e.path, slices.Sorted(maps.Keys(e.attrs))))
	}

	return structure
}

// entryNames returns the names of the entries of archive, in order.
func entryNames(t *testing.T, archive []byte) []string {
	t.Helper()

	var names []string
	for _, f := range openZip(t, archive).File {
		names = append(names, f.Name)
	}

	return names
}

// Given the same options, when Write runs twice, then the bytes are identical.
func TestWrite_SameOptions_WritesIdenticalBytes(t *testing.T) {
	t.Parallel()

	first := generate(t, synthexport.Fixture())

	require.NotEmpty(t, first)
	require.Equal(t, first, generate(t, synthexport.Fixture()))
}

// Given another seed, then the values differ and the structure stays: the same entries, holding the
// same elements in the same order.
func TestWrite_AnotherSeed_ChangesValuesAndKeepsStructure(t *testing.T) {
	t.Parallel()

	other := synthexport.Fixture()
	other.Seed = 2

	first, second := generate(t, synthexport.Fixture()), generate(t, other)

	require.NotEqual(t, readEntry(t, first, exportXMLName), readEntry(t, second, exportXMLName))
	require.Equal(t, entryNames(t, first), entryNames(t, second))

	for _, name := range entryNames(t, first) {
		if strings.HasSuffix(name, ".csv") {
			continue
		}

		require.Equal(t,
			structureOf(readElements(t, readEntry(t, first, name))),
			structureOf(readElements(t, readEntry(t, second, name))), name)
	}
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

	for _, f := range openZip(t, generate(t, synthexport.Fixture())).File {
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

	content := string(readEntry(t, generate(t, synthexport.Fixture()), exportXMLName))

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

	for _, e := range readElements(t, readEntry(t, generate(t, synthexport.Fixture()), exportXMLName)) {
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

	tests := []struct {
		name   string
		change func(*synthexport.Options)
		want   string
	}{
		{"no days", func(o *synthexport.Options) { o.Days = 0 }, "days must be at least 1"},
		{"negative days", func(o *synthexport.Options) { o.Days = -1 }, "days must be at least 1"},
		{"no density", func(o *synthexport.Options) { o.Density = 0 }, "density must be at least 1"},
		{"negative density", func(o *synthexport.Options) { o.Density = -1 }, "density must be at least 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := synthexport.Fixture()
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

	require.ErrorIs(t, synthexport.Write(ctx, io.Discard, synthexport.Fixture()), context.Canceled)
}

// failingWriter refuses every write.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// Given a writer that fails, then Write returns its error.
func TestWrite_FailingWriter_ReturnsItsError(t *testing.T) {
	t.Parallel()

	errFull := errors.New("disk full")

	require.ErrorIs(t, synthexport.Write(t.Context(), failingWriter{errFull}, synthexport.Fixture()), errFull)
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
		records = withPath(exportElements(t, synthexport.Fixture()), "HealthData/Record")
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

	opts := synthexport.Fixture()
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

	records := withPath(exportElements(t, synthexport.Fixture()), "HealthData/Record")

	for name, matches := range shapes {
		require.True(t, slices.ContainsFunc(records, matches), name)
	}
}

// Given the step counts, then two sources overlap in time, which source priority has to resolve.
func TestWrite_StepCounts_OfTwoSourcesOverlapInTime(t *testing.T) {
	t.Parallel()

	steps := withPath(exportElements(t, synthexport.Fixture()), "HealthData/Record")
	steps = slices.DeleteFunc(steps, func(r element) bool { return r.attrs["type"] != stepType })

	overlaps := slices.ContainsFunc(steps, func(a element) bool {
		return slices.ContainsFunc(steps, func(b element) bool {
			return a.attrs["sourceName"] != b.attrs["sourceName"] &&
				a.attrs["startDate"] < b.attrs["endDate"] && b.attrs["startDate"] < a.attrs["endDate"]
		})
	})

	require.True(t, overlaps)
}

// Given the density of a watch, then the quantities of one source do not overlap, as in a real export.
func TestWrite_Quantities_OfOneSourceDoNotOverlapAtTheDensityOfAWatch(t *testing.T) {
	t.Parallel()

	opts := synthexport.Fixture()
	opts.Days, opts.Density = 1, 30

	previousEnd := map[string]string{}

	for _, r := range withPath(exportElements(t, opts), "HealthData/Record") {
		if !strings.HasPrefix(r.attrs["type"], "HKQuantityTypeIdentifier") {
			continue
		}

		key := r.attrs["type"] + " " + r.attrs["sourceName"]
		require.GreaterOrEqual(t, r.attrs["startDate"], previousEnd[key], key)

		previousEnd[key] = r.attrs["endDate"]
	}
}

// Given the device attributes, then they have the five shapes of the format, and they are escaped.
func TestWrite_Devices_HaveTheFiveShapesOfTheFormat(t *testing.T) {
	t.Parallel()

	var (
		content = readEntry(t, generate(t, synthexport.Fixture()), exportXMLName)
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
			return regexp.MustCompile(`^\d+\.\d{3} kPa$`).MatchString(value)
		},
		"time zone": func(key, value string) bool { return key == "HKTimeZone" && value == "Europe/Paris" },
	}

	entries := withPath(exportElements(t, synthexport.Fixture()), "HealthData/Record/MetadataEntry")

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

	for _, e := range exportElements(t, synthexport.Fixture()) {
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

	require.Equal(t, synthexport.Fixture().Days, lists)
	require.Greater(t, beats, lists)
}

// attrsOf identifies an element by all its attributes.
func attrsOf(e element) string { return fmt.Sprint(e.attrs) }

// Given the correlations, then there are food and blood pressure ones, whose records also exist at
// top level with the same attributes.
func TestWrite_Correlations_HoldRecordsThatAlsoExistAtTopLevel(t *testing.T) {
	t.Parallel()

	var (
		elements = exportElements(t, synthexport.Fixture())
		inner    = withPath(elements, "HealthData/Correlation/Record")
		topLevel = map[string]bool{}
		types    = map[string]bool{}
		within   = map[string]bool{}
	)

	for _, r := range withPath(elements, "HealthData/Record") {
		topLevel[attrsOf(r)] = true
	}

	for _, c := range withPath(elements, "HealthData/Correlation") {
		types[c.attrs["type"]] = true
	}

	for _, r := range inner {
		require.True(t, topLevel[attrsOf(r)], "not at top level: %v", r.attrs)

		within[r.attrs["type"]] = true
	}

	require.ElementsMatch(t,
		[]string{"HKCorrelationTypeIdentifierFood", "HKCorrelationTypeIdentifierBloodPressure"},
		slices.Collect(maps.Keys(types)))
	require.Subset(t, slices.Collect(maps.Keys(within)), []string{
		"HKQuantityTypeIdentifierDietaryEnergyConsumed",
		"HKQuantityTypeIdentifierDietaryProtein",
		"HKQuantityTypeIdentifierBloodPressureSystolic",
		"HKQuantityTypeIdentifierBloodPressureDiastolic",
	})
}

// Given the food correlations, then they carry metadata as their records do, and their free text
// needs the escaping of a real export.
func TestWrite_Correlations_CarryMetadataWithFreeText(t *testing.T) {
	t.Parallel()

	content := readEntry(t, generate(t, synthexport.Fixture()), exportXMLName)
	elements := readElements(t, content)

	var keys []string
	for _, m := range withPath(elements, "HealthData/Correlation/MetadataEntry") {
		keys = append(keys, m.attrs["key"])
	}

	require.Subset(t, keys, []string{"HKExternalUUID", "HKFoodType", "Repas"})
	require.NotEmpty(t, withPath(elements, "HealthData/Correlation/Record/MetadataEntry"))

	require.Contains(t, string(content), `value="Salade d'été"`)
	require.Contains(t, string(content), `value="Yaourt &amp; fruits"`)
}

// workoutNode is a Workout element with everything below it.
type workoutNode struct {
	element
	below []element
}

// readWorkouts groups the elements of export.xml by workout.
func readWorkouts(elements []element) []workoutNode {
	var workouts []workoutNode

	for _, e := range elements {
		switch {
		case e.path == workoutPath:
			workouts = append(workouts, workoutNode{element: e})
		case strings.HasPrefix(e.path, workoutPath+"/"):
			last := &workouts[len(workouts)-1]
			last.below = append(last.below, e)
		}
	}

	return workouts
}

// children returns the names of the direct children of a workout, once per group.
func (w workoutNode) children() []string {
	var names []string

	for _, e := range w.below {
		if name := strings.TrimPrefix(e.path, workoutPath+"/"); !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}

	return slices.Compact(names)
}

// routePath returns the file reference of a workout, if it has one.
func (w workoutNode) routePath() string {
	for _, e := range withPath(w.below, workoutPath+"/WorkoutRoute/FileReference") {
		return e.attrs["path"]
	}

	return ""
}

// findWorkout returns the first workout that matches.
func findWorkout(t *testing.T, workouts []workoutNode, matches func(workoutNode) bool) workoutNode {
	t.Helper()

	i := slices.IndexFunc(workouts, matches)
	require.NotEqual(t, -1, i, "no such workout")

	return workouts[i]
}

// parseDate reads a date of export.xml.
func parseDate(t *testing.T, value string) time.Time {
	t.Helper()

	date, err := time.Parse(dateLayout, value)
	require.NoError(t, err)

	return date
}

// Given the workouts, then a run with a route, a strength session without route and a third-party
// copy hold the children of a real export, in its order.
func TestWrite_Workouts_HoldTheChildrenOfARealExport(t *testing.T) {
	t.Parallel()

	workouts := readWorkouts(exportElements(t, synthexport.Fixture()))

	tests := []struct {
		name    string
		matches func(workoutNode) bool
		want    []string
	}{
		{
			"run with a route",
			func(w workoutNode) bool { return w.attrs["workoutActivityType"] == runningType && w.routePath() != "" },
			[]string{"MetadataEntry", "WorkoutEvent", "WorkoutStatistics", "WorkoutZoneGroup", "WorkoutRoute", "Record"},
		},
		{
			"strength session",
			func(w workoutNode) bool { return w.attrs["workoutActivityType"] == strengthType && w.routePath() == "" },
			[]string{"MetadataEntry", "WorkoutStatistics", "WorkoutZoneGroup"},
		},
		{
			"third-party copy",
			func(w workoutNode) bool { return w.attrs["sourceName"] == "Strava" },
			[]string{"MetadataEntry"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, findWorkout(t, workouts, tt.matches).children())
		})
	}
}

// Given the workouts, then none has totals in its attributes, as in a recent export: they come from
// the statistics.
func TestWrite_Workouts_HoldNoTotalsInTheirAttributes(t *testing.T) {
	t.Parallel()

	workouts := readWorkouts(exportElements(t, synthexport.Fixture()))
	require.NotEmpty(t, workouts)

	for _, w := range workouts {
		require.NotContains(t, w.attrs, "totalDistance")
		require.NotContains(t, w.attrs, "totalEnergyBurned")
		require.Equal(t, "min", w.attrs["durationUnit"])
	}
}

// Given the Strava copy of a run, then it is a run from another source that overlaps the original by
// more than 80 percent of the shorter one, which makes it a duplicate.
func TestWrite_StravaCopy_DuplicatesARun(t *testing.T) {
	t.Parallel()

	workouts := readWorkouts(exportElements(t, synthexport.Fixture()))

	strava := findWorkout(t, workouts, func(w workoutNode) bool { return w.attrs["sourceName"] == "Strava" })
	require.Equal(t, runningType, strava.attrs["workoutActivityType"])

	original := findWorkout(t, workouts, func(w workoutNode) bool {
		return w.attrs["sourceName"] != "Strava" && w.attrs["startDate"][:10] == strava.attrs["startDate"][:10]
	})
	require.Equal(t, runningType, original.attrs["workoutActivityType"])

	var (
		copyStart, copyEnd = parseDate(t, strava.attrs["startDate"]), parseDate(t, strava.attrs["endDate"])
		start, end         = parseDate(t, original.attrs["startDate"]), parseDate(t, original.attrs["endDate"])
		overlap            = time.Duration(math.Min(float64(copyEnd.Sub(start)), float64(end.Sub(copyStart))))
		shorter            = min(copyEnd.Sub(copyStart), end.Sub(start))
	)

	require.Greater(t, overlap.Seconds(), 0.8*shorter.Seconds())

	var keys []string
	for _, m := range withPath(strava.below, workoutPath+"/MetadataEntry") {
		keys = append(keys, m.attrs["key"]+"="+m.attrs["value"])
	}

	require.Condition(t, func() bool {
		return slices.ContainsFunc(keys, func(k string) bool {
			return regexp.MustCompile(`^HKExternalUUID=strava://activities/\d+$`).MatchString(k)
		})
	}, "no external identifier in %v", keys)
}

// Given the children of a workout, then they have the attributes of a real export.
func TestWrite_WorkoutChildren_HaveTheAttributesOfTheFormat(t *testing.T) {
	t.Parallel()

	workouts := readWorkouts(exportElements(t, synthexport.Fixture()))
	run := findWorkout(t, workouts, func(w workoutNode) bool { return w.routePath() != "" })

	statistics := withPath(run.below, workoutPath+"/WorkoutStatistics")
	require.True(t, slices.ContainsFunc(statistics, func(s element) bool {
		_, hasAverage := s.attrs["average"]
		return s.attrs["unit"] == "kcal" && s.attrs["sum"] != "" && !hasAverage
	}), "no sum")
	require.True(t, slices.ContainsFunc(statistics, func(s element) bool {
		_, hasSum := s.attrs["sum"]
		return s.attrs["unit"] == "count/min" && !hasSum &&
			s.attrs["average"] != "" && s.attrs["minimum"] != "" && s.attrs["maximum"] != ""
	}), "no average")

	events := withPath(run.below, workoutPath+"/WorkoutEvent")
	require.True(t, slices.ContainsFunc(events, func(e element) bool {
		return e.attrs["type"] == "HKWorkoutEventTypeSegment" && e.attrs["duration"] != "" && e.attrs["durationUnit"] == "min"
	}), "no segment")
	require.True(t, slices.ContainsFunc(events, func(e element) bool {
		_, hasDuration := e.attrs["duration"]
		return e.attrs["type"] == "HKWorkoutEventTypePause" && !hasDuration
	}), "no pause")

	group := withPath(run.below, workoutPath+"/WorkoutZoneGroup")
	require.Len(t, group, 1)
	require.Equal(t, map[string]string{
		"type": "HKQuantityTypeIdentifierHeartRate", "source": "system", "unit": "count/min",
	}, group[0].attrs)

	zones := withPath(run.below, workoutPath+"/WorkoutZoneGroup/WorkoutZone")
	require.Len(t, zones, 5)
	require.NotContains(t, zones[0].attrs, "minimum")
	require.Contains(t, zones[0].attrs, "maximum")
	require.Contains(t, zones[2].attrs, "minimum")
	require.Contains(t, zones[2].attrs, "maximum")
	require.Contains(t, zones[4].attrs, "minimum")
	require.NotContains(t, zones[4].attrs, "maximum")
}

// Given the records inside workouts, then they also exist at top level with the same attributes.
func TestWrite_WorkoutRecords_AlsoExistAtTopLevel(t *testing.T) {
	t.Parallel()

	elements := exportElements(t, synthexport.Fixture())
	inner := withPath(elements, workoutPath+"/Record")
	require.NotEmpty(t, inner)

	topLevel := map[string]bool{}
	for _, r := range withPath(elements, "HealthData/Record") {
		topLevel[attrsOf(r)] = true
	}

	for _, r := range inner {
		require.True(t, topLevel[attrsOf(r)], "not at top level: %v", r.attrs)
	}
}

// Given the routes, then every file reference resolves to one GPX entry and every GPX entry is
// referenced.
func TestWrite_Routes_AndTheirReferencesMatch(t *testing.T) {
	t.Parallel()

	archive := generate(t, synthexport.Fixture())

	var references, entries []string

	for _, w := range readWorkouts(readElements(t, readEntry(t, archive, exportXMLName))) {
		if path := w.routePath(); path != "" {
			references = append(references, "apple_health_export"+path)
		}
	}

	for _, f := range openZip(t, archive).File {
		if strings.HasSuffix(f.Name, ".gpx") {
			entries = append(entries, f.Name)
		}
	}

	require.NotEmpty(t, references)
	require.ElementsMatch(t, entries, references)
}

// Given a route, then its points are in time order inside their workout, carry the fields of a real
// export, and stay around latitude 0 and longitude 0.
func TestWrite_Routes_HavePointsInOrderInsideTheirWorkout(t *testing.T) {
	t.Parallel()

	const (
		point  = "gpx/trk/trkseg/trkpt"
		radius = 0.1
	)

	archive := generate(t, synthexport.Fixture())
	routes := 0

	for _, w := range readWorkouts(readElements(t, readEntry(t, archive, exportXMLName))) {
		if w.routePath() == "" {
			continue
		}

		routes++

		content := readEntry(t, archive, "apple_health_export"+w.routePath())
		gpx := readElements(t, content)
		points := withPath(gpx, point)

		require.Contains(t, string(content), `creator="Apple Health Export"`)
		require.Regexp(t, `^Route \d{4}-\d{2}-\d{2} \d{1,2}:\d{2}[ap]m$`, withPath(gpx, "gpx/trk/name")[0].text)
		require.Len(t, withPath(gpx, "gpx/metadata/time"), 1)
		require.Len(t, withPath(gpx, "gpx/trk/trkseg"), 1)
		require.Greater(t, len(points), 2)

		for _, field := range []string{"ele", "time", "extensions/speed", "extensions/course", "extensions/hAcc", "extensions/vAcc"} {
			require.Len(t, withPath(gpx, point+"/"+field), len(points), field)
		}

		for _, p := range points {
			lat, err := strconv.ParseFloat(p.attrs["lat"], 64)
			require.NoError(t, err)

			lon, err := strconv.ParseFloat(p.attrs["lon"], 64)
			require.NoError(t, err)

			require.Less(t, math.Abs(lat), radius)
			require.Less(t, math.Abs(lon), radius)
		}

		previous := parseDate(t, w.attrs["startDate"]).Add(-time.Second)

		for _, stamp := range withPath(gpx, point+"/time") {
			at, err := time.Parse(time.RFC3339, stamp.text)
			require.NoError(t, err)
			require.True(t, at.After(previous), "%s comes before the point it follows", stamp.text)

			previous = at
		}

		require.False(t, previous.After(parseDate(t, w.attrs["endDate"])))
	}

	require.NotZero(t, routes)
}

// Given the fixture options, then the content of the zip equals the unpacked tree in testdata, which
// -update rewrites. Entries are compared rather than the zip, whose bytes depend on compress/flate.
func TestWrite_Fixture_EqualsTheGoldenTree(t *testing.T) {
	t.Parallel()

	archive := generate(t, synthexport.Fixture())

	got := map[string][]byte{}
	for _, f := range openZip(t, archive).File {
		got[f.Name] = readEntry(t, archive, f.Name)
	}

	if *update {
		require.NoError(t, os.RemoveAll(filepath.Join("testdata", "apple_health_export")))

		for name, content := range got {
			path := filepath.Join("testdata", filepath.FromSlash(name))
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, os.WriteFile(path, content, 0o600))
		}
	}

	want := map[string][]byte{}
	golden := os.DirFS("testdata")

	require.NoError(t, fs.WalkDir(golden, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		want[name], err = fs.ReadFile(golden, name)

		return err
	}))

	require.ElementsMatch(t, slices.Collect(maps.Keys(want)), slices.Collect(maps.Keys(got)))

	for name, content := range got {
		require.True(t, bytes.Equal(want[name], content), "%s differs from testdata: review it, then run with -update", name)
	}
}

// heapWatcher measures the live heap while an export is written to it.
type heapWatcher struct {
	written, next int
	peak          uint64
}

func (w *heapWatcher) Write(p []byte) (int, error) {
	const checkEvery = 512 << 10

	w.written += len(p)

	if w.written >= w.next {
		w.next = w.written + checkEvery

		var stats runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&stats)
		w.peak = max(w.peak, stats.HeapAlloc)
	}

	return len(p), nil
}

// Given an export of several tens of megabytes, then the live heap stays under 16 MiB during
// generation. Not parallel, since other tests would count in the heap.
func TestWrite_LargeExport_KeepsTheLiveHeapBounded(t *testing.T) {
	const (
		ceiling        = 16 << 20
		compressedSize = 1 << 20 // more than 20 MB before compression
	)

	opts := synthexport.Fixture()
	opts.Days = 30
	opts.Density = 12

	var watcher heapWatcher

	require.NoError(t, synthexport.Write(t.Context(), &watcher, opts))
	require.Greater(t, watcher.written, compressedSize)
	require.NotZero(t, watcher.peak)
	require.Less(t, watcher.peak, uint64(ceiling))
}
