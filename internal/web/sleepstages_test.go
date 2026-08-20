package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
)

// seriesColors is the length of the Palette's categorical ramp, which every Palette
// is verified against for four-way separation (ADR 0026). A Stage pointing past it
// would render transparent.
const seriesColors = 4

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

	for stage := range order {
		if !labels[stage] {
			t.Errorf("SLEEP_STAGES lists %q with no STAGE_LABEL", stage)
		}
		slot, ok := colors[stage]
		if !ok {
			t.Errorf("SLEEP_STAGES lists %q with no STAGE_COLOR_INDEX", stage)
			continue
		}
		if slot < 0 || slot >= seriesColors {
			t.Errorf("Stage %q takes colour slot %d, outside the %d-colour ramp", stage, slot, seriesColors)
		}
	}
}
