package web

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// An Annotation's marker is placed on a category the server named (ADR 0030), and
// nothing about that placement fails loudly when it goes wrong: Recharts matches a
// reference element's x against the axis categories by equality, so a date computed
// on this side that disagrees with the server's boundary rules by one day draws
// nothing at all, with no error anywhere. These read the SPA as text, like the sleep
// and workout contracts next door, so they run in `make ci` with no front-end
// toolchain.
const (
	annotationsTSPath = "../../web/src/lib/annotations.ts"
	panelChartTSXPath = "../../web/src/components/panel-chart.tsx"
	chartTSPath       = "../../web/src/lib/chart.ts"
)

var (
	// A hex literal anywhere in the drawing code, in either notation.
	hexColourRe = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	// A CSS colour keyword or function that is not a custom property: rgb(), hsl()
	// with literal numbers, or a bare named colour on a paint attribute.
	namedColourRe = regexp.MustCompile(
		`(?:fill|stroke|background|color)\s*[:=]\s*"(?:[a-z]+|rgba?\([^"]*\)|hsla?\(\s*\d[^"]*\))"`)
)

// dateArithmetic is the vocabulary of snapping a date to a bucket. None of it
// belongs on the client: `internal/timeaxis` owns bucket boundaries, and its Go and
// SQL sides are pinned to each other by test. A third implementation, in a language
// where nothing compares it to the other two, is exactly what this forbids.
var dateArithmetic = []string{
	"startOfWeek", "startOfMonth", "startOfDay",
	"endOfWeek", "endOfMonth", "endOfDay",
	"addDays", "addWeeks", "addMonths", "subDays", "subWeeks", "subMonths",
	"getDay(", "setDate(", "new Date(",
}

// TestAnnotationPlacementDoesNoDateArithmetic: the projection reads the server's
// folded buckets and compares them as strings (YYYY-MM-DD is chronological when
// compared lexically). It must never derive one.
func TestAnnotationPlacementDoesNoDateArithmetic(t *testing.T) {
	for _, path := range []string{annotationsTSPath, panelChartTSXPath} {
		src := readFileText(t, path)
		for _, banned := range dateArithmetic {
			if strings.Contains(src, banned) {
				t.Errorf("%s uses %q: a bucket is the server's to compute (ADR 0030)", path, banned)
			}
		}
	}
}

// TestAnnotationMarkersUseTheServersBucket: the fields a marker is drawn from are
// the folded ones the API sends, not the note's real dates. Placing a marker at
// `starts_on` would be correct at the day bucket and silently wrong at every other.
func TestAnnotationMarkersUseTheServersBucket(t *testing.T) {
	src := readFileText(t, annotationsTSPath)
	for _, field := range []string{"a.bucket", "a.end_bucket"} {
		if !strings.Contains(src, field) {
			t.Errorf("%s does not read %s: the marker must sit on the folded bucket", annotationsTSPath, field)
		}
	}
	for _, field := range []string{"starts_on", "ends_on"} {
		if strings.Contains(src, field) {
			t.Errorf("%s reads %s: those are the tooltip's dates, not a chart position", annotationsTSPath, field)
		}
	}
}

// TestAnnotationOverlayDrawsBehindTheMarks: an Annotation is context, not a series.
// Recharts paints in child order, so the overlay has to be emitted before the marks
// or a band would sit on top of the very curve it is there to explain.
func TestAnnotationOverlayDrawsBehindTheMarks(t *testing.T) {
	src := readFileText(t, panelChartTSXPath)
	overlay := strings.Index(src, "{annotationOverlay(overlay)}")
	marks := strings.Index(src, "{list.map((s, i) =>")
	if overlay < 0 || marks < 0 {
		t.Fatalf("%s: overlay=%d marks=%d, one of the two has been renamed", panelChartTSXPath, overlay, marks)
	}
	if overlay > marks {
		t.Errorf("%s draws its Annotations after the marks: context must sit behind the data", panelChartTSXPath)
	}
}

// TestAnnotationOverlayWearsTheRecessedTone: a note that wore a chart colour would
// read as a fifth series on a Panel that already carries four (ADR 0020).
//
// The tone is now named once in lib/chart.ts and referenced by the chart, so this
// follows the indirection rather than reading a literal: the chart must bind
// ANNOTATION to AXIS, and AXIS must be the muted text tone. Checking both ends keeps
// the contract honest — binding to a constant that had drifted to a chart colour
// would otherwise pass.
func TestAnnotationOverlayWearsTheRecessedTone(t *testing.T) {
	src := readFileText(t, panelChartTSXPath)
	if !strings.Contains(src, "const ANNOTATION = AXIS;") {
		t.Errorf("%s: the Annotation tone must be the muted one the Baseline uses (AXIS)", panelChartTSXPath)
	}
	if !strings.Contains(src, "const BASELINE = AXIS;") {
		t.Errorf("%s: the Baseline tone must be the muted one too (ADR 0015)", panelChartTSXPath)
	}
	for _, chart := range []string{"--chart-1", "--chart-positive", "--chart-negative"} {
		if strings.Contains(src, "ANNOTATION = \"hsl(var("+chart) {
			t.Errorf("%s colours its Annotations with %s: they are context, not a series", panelChartTSXPath, chart)
		}
	}

	tokens := readFileText(t, chartTSPath)
	if !strings.Contains(tokens, `export const AXIS = token("muted-foreground");`) {
		t.Errorf("%s: AXIS must be the muted text tone — it is what the Annotation and Baseline wear", chartTSPath)
	}
}

// TestSpaColoursAreTokensOnly: Verve ships nine Palettes in two modes (ADR 0024,
// ADR 0026), so a literal colour anywhere in the SPA is a bug in seventeen of the
// eighteen combinations — and one that shows up only in the palette nobody thought
// to open. Every colour must be a token: a Tailwind class bound to a custom
// property, or `hsl(var(--x))` where a library needs a string.
//
// This scans the whole source tree rather than the chart modules alone, because the
// rule is not about charts. The stylesheet is the one file allowed to hold literals:
// it is where the Palettes are defined.
func TestSpaColoursAreTokensOnly(t *testing.T) {
	root := filepath.Clean("../../web/src")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (filepath.Ext(path) != ".ts" && filepath.Ext(path) != ".tsx") {
			return nil
		}
		src := readFileText(t, path)
		for _, m := range hexColourRe.FindAllString(src, -1) {
			t.Errorf("%s carries the literal colour %s: use a token (ADR 0024)", path, m)
		}
		for _, m := range namedColourRe.FindAllString(src, -1) {
			// `stroke="none"` and `fill="none"` are absences, not colours.
			if strings.Contains(m, `"none"`) {
				continue
			}
			t.Errorf("%s carries the literal colour %s: use a token (ADR 0024)", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// TestAnnotationWritesInvalidateOnlyTheNotes: a note changes no aggregate, so a
// write must invalidate the annotations query and nothing else. Widening it to the
// Series would refetch four Panels' worth of buckets because someone typed
// "holiday", making the cheapest write in the app the most expensive one, and it
// would look like nothing at all had gone wrong.
func TestAnnotationWritesInvalidateOnlyTheNotes(t *testing.T) {
	const hookPath = "../../web/src/hooks/use-annotations.ts"
	src := readFileText(t, hookPath)

	if !strings.Contains(src, "invalidateQueries({ queryKey: ANNOTATIONS_KEY })") {
		t.Errorf("%s: a write must invalidate ANNOTATIONS_KEY", hookPath)
	}
	// One invalidation helper, shared by all three verbs: three hand-written calls
	// are three chances for one of them to drift.
	if n := strings.Count(src, "invalidateQueries"); n != 1 {
		t.Errorf("%s calls invalidateQueries %d times, want exactly 1 shared helper", hookPath, n)
	}
	for _, other := range []string{`"series"`, `"ledger"`, `"dashboards"`, `"plan"`} {
		if strings.Contains(src, other) {
			t.Errorf("%s names the %s query: a note leaves every aggregate untouched", hookPath, other)
		}
	}
	// Every verb must go through the helper rather than skipping the refresh: a note
	// written but not shown reads as a note not saved.
	for _, verb := range []string{"useCreateAnnotation", "useUpdateAnnotation", "useDeleteAnnotation"} {
		if !strings.Contains(src, verb) {
			t.Errorf("%s does not export %s", hookPath, verb)
		}
	}
	if n := strings.Count(src, "onSuccess: invalidate"); n != 3 {
		t.Errorf("%s wires onSuccess %d times, want one per verb (3)", hookPath, n)
	}
}
