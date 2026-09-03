package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The Stage vocabulary spans two languages: the Apple Connector decides which Stage
// slugs can ever reach the database, and the SPA decides how they are ordered,
// labelled and coloured. Nothing at either end fails when they drift — a Stage the
// client has never heard of simply vanishes from the stack, which is the failure
// mode a chart cannot show you. Both files are read as text, like the palette
// contract next door, so this runs in `make ci` with no front-end toolchain.
const (
	familiesPath = "../connector/applehealth/families.go"
	sleepTSPath  = "../../web/src/lib/sleep.ts"
)

var (
	// The Connector's sleep category values: "HKCategoryValueSleepAnalysisAsleepREM": "asleep_rem".
	appleSleepValueRe = regexp.MustCompile(`"HKCategoryValueSleepAnalysis\w+":\s*"(\w+)"`)
	// The SPA's stack order, labels and colour slots.
	stageOrderRe = regexp.MustCompile(`(?s)SLEEP_STAGES = \[(.*?)\] as const`)
	stageEntryRe = regexp.MustCompile(`"(\w+)"`)
	stageLabelRe = regexp.MustCompile(`(?s)STAGE_LABEL: Record<string, string> = \{(.*?)\n\}`)
	stageColorRe = regexp.MustCompile(`(?s)STAGE_COLOR_INDEX: Record<string, number> = \{(.*?)\n\}`)
	tsKeyRe      = regexp.MustCompile(`(?m)^\s*(\w+):`)
	tsColorRe    = regexp.MustCompile(`(?m)^\s*(\w+):\s*(\d+),`)
	// The two ramps in chart.ts, so the bound below is read off the ramp itself
	// rather than restated here where it can quietly stop being true.
	seriesRampRe   = regexp.MustCompile(`(?s)SERIES_COLORS = \[(.*?)\];`)
	categoryRampRe = regexp.MustCompile(`(?s)CATEGORY_COLORS = \[\.\.\.SERIES_COLORS,(.*?)\];`)
	rampTokenRe    = regexp.MustCompile(`token\("chart-\d+"\)`)
)

// categoryRampLength is how many slots CATEGORY_COLORS actually has. A Stage pointing
// past it would render transparent, which is the whole reason for the bound; reading
// it out of chart.ts rather than restating it keeps the two from drifting the way this
// file exists to prevent.
//
// It is the longer ramp on purpose. SERIES_COLORS is four because a Panel holds four
// Metrics (ADR 0020) and every Palette is verified for four-way separation across
// exactly those (ADR 0026); Stages are not Series and were never bound by that count.
func categoryRampLength(t *testing.T) int {
	t.Helper()
	chart := readFileText(t, chartTSPath)
	series := seriesRampRe.FindStringSubmatch(chart)
	category := categoryRampRe.FindStringSubmatch(chart)
	if series == nil || category == nil {
		t.Fatalf("%s: could not find SERIES_COLORS and CATEGORY_COLORS; the ramps or the parser moved", chartTSPath)
	}
	n := len(rampTokenRe.FindAllString(series[1], -1)) + len(rampTokenRe.FindAllString(category[1], -1))
	if n < 4 {
		t.Fatalf("%s: parsed a %d-slot category ramp, which cannot be right", chartTSPath, n)
	}
	return n
}

func readFileText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// section pulls one brace-delimited TypeScript literal's body out of a file.
func section(t *testing.T, src string, re *regexp.Regexp, what string) string {
	t.Helper()
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("%s not found in %s — has it been renamed?", what, sleepTSPath)
	}
	return m[1]
}

// TestSleepStagesCoverTheConnector: every Stage the Apple Connector can store has a
// place in the stack, a label, and a colour (ADR 0027).
func TestSleepStagesCoverTheConnector(t *testing.T) {
	families := readFileText(t, familiesPath)
	ts := readFileText(t, sleepTSPath)

	order := map[string]bool{}
	for _, m := range stageEntryRe.FindAllStringSubmatch(section(t, ts, stageOrderRe, "SLEEP_STAGES"), -1) {
		order[m[1]] = true
	}
	if len(order) == 0 {
		t.Fatal("SLEEP_STAGES is empty")
	}

	labels := map[string]bool{}
	for _, m := range tsKeyRe.FindAllStringSubmatch(section(t, ts, stageLabelRe, "STAGE_LABEL"), -1) {
		labels[m[1]] = true
	}
	colors := map[string]int{}
	for _, m := range tsColorRe.FindAllStringSubmatch(section(t, ts, stageColorRe, "STAGE_COLOR_INDEX"), -1) {
		i, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("colour slot for %q is not a number: %v", m[1], err)
		}
		colors[m[1]] = i
	}

	stored := appleSleepValueRe.FindAllStringSubmatch(families, -1)
	if len(stored) == 0 {
		t.Fatalf("no sleep category values found in %s", familiesPath)
	}
	for _, m := range stored {
		stage := m[1]
		if !order[stage] {
			t.Errorf("the Connector can store Stage %q but SLEEP_STAGES does not list it: it would be missing from the stack", stage)
		}
		if !labels[stage] {
			t.Errorf("Stage %q has no STAGE_LABEL", stage)
		}
		if _, ok := colors[stage]; !ok {
			t.Errorf("Stage %q has no STAGE_COLOR_INDEX", stage)
		}
	}

	ramp := categoryRampLength(t)
	for stage := range order {
		if !labels[stage] {
			t.Errorf("SLEEP_STAGES lists %q with no STAGE_LABEL", stage)
		}
		slot, ok := colors[stage]
		if !ok {
			t.Errorf("SLEEP_STAGES lists %q with no STAGE_COLOR_INDEX", stage)
			continue
		}
		if slot < 0 || slot >= ramp {
			t.Errorf("Stage %q takes colour slot %d, outside the %d-colour ramp", stage, slot, ramp)
		}
	}
}

// TestAwakeIsDrawnRecessed locks the one Stage that is deliberately not a colour in
// the ramp.
//
// Awake minutes are stacked so a broken night looks broken, and are never counted as
// sleep (ADR 0027). Giving them a categorical colour would say the opposite — that
// awake is another kind of sleep — and would put the most saturated treatment on the
// card's least important segment. So `awake` keeps its slot in the contract above
// (every Stage must have one) and is painted in the recessed tone instead. Widening
// the ramp does not change that: the reason was never that colours were scarce.
func TestAwakeIsDrawnRecessed(t *testing.T) {
	ts := readFileText(t, sleepTSPath)
	if !strings.Contains(ts, `RECESSIVE_STAGES: readonly string[] = ["awake"]`) {
		t.Errorf("%s: awake must be listed as a recessive Stage (ADR 0027)", sleepTSPath)
	}
	if !strings.Contains(ts, "if (RECESSIVE_STAGES.includes(stage)) return RECESSED;") {
		t.Errorf("%s: stageColor must paint a recessive Stage in the recessed tone", sleepTSPath)
	}
	// Every consumer must go through stageColor rather than indexing the ramp
	// directly, or the rule would hold on the chart and not in the tooltip.
	chart := readFileText(t, panelChartTSXPath)
	if strings.Contains(chart, "CATEGORY_COLORS[STAGE_COLOR_INDEX[") || strings.Contains(chart, "SERIES_COLORS[STAGE_COLOR_INDEX[") {
		t.Errorf("%s indexes the ramp by Stage directly: use stageColor, which knows about awake", panelChartTSXPath)
	}
}
