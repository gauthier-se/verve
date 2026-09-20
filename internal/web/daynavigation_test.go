package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A day bucket leads to a Day; a week or month bucket leads nowhere (ADR 0043).
//
// That one condition is the whole safety property of the feature. Without it,
// "clicking a bucket opens it" becomes "clicking a month opens a month page",
// which is the week-and-month page the ADR refuses, or worse, a month that opens
// the first of its days: a guess presented as a destination.
//
// Nothing fails when it drifts. A chart handed an unconditional callback navigates
// perfectly well, to the wrong place, and the only symptom is a reader who clicked
// on August and landed on the 1st. So the rule is read as text, like the palette
// and Stage contracts next door: what must hold spans a hook and four components,
// and a runtime test of any one of them would not see it.

const (
	dayHookPath   = "../../web/src/hooks/use-day.ts"
	ledgerTSPath  = "../../web/src/components/ledger-detail-table.tsx"
	spaComponents = "../../web/src/components"
)

// TestDayNavigationIsGatedOnDayGrain: the gate lives in one place and every chart
// goes through it.
func TestDayNavigationIsGatedOnDayGrain(t *testing.T) {
	hook := readFileText(t, dayHookPath)
	if !strings.Contains(hook, `bucket === "day"`) {
		t.Errorf("%s: useDayNavigation must return its callback only at day grain", dayHookPath)
	}
	// Undefined rather than a no-op, because a caller shows the difference in its
	// cursor: an affordance that lies is worse than none.
	if !strings.Contains(hook, "? go : undefined") {
		t.Errorf("%s: a non-day grain must yield undefined, not a callback that does nothing", dayHookPath)
	}

	// Every chart that can select a bucket takes its callback from the hook. A
	// component passing its own would be the gate written twice.
	for _, path := range []string{
		filepath.Join(spaComponents, "panel-card.tsx"),
		filepath.Join(spaComponents, "metric-page.tsx"),
	} {
		src := readFileText(t, path)
		if !strings.Contains(src, "onSelectBucket=") {
			t.Errorf("%s: no chart selection wired; has the prop been renamed?", path)
			continue
		}
		if !strings.Contains(src, "useDayNavigation(") {
			t.Errorf("%s passes onSelectBucket without useDayNavigation: the grain gate is bypassed", path)
		}
	}

	// The band is the third chart and reads the grain off its own payload.
	history := readFileText(t, filepath.Join(spaComponents, "history-page.tsx"))
	if !strings.Contains(history, "useDayNavigation(band.bucket)") {
		t.Errorf("%s: the history band must gate on the band's own grain", filepath.Join(spaComponents, "history-page.tsx"))
	}

	// The Ledger row is a link rather than a click handler, so it carries the same
	// condition in its own words.
	ledger := readFileText(t, ledgerTSPath)
	if !strings.Contains(ledger, `bucket !== "day"`) {
		t.Errorf("%s: a week or month row must render plain, not as a link to one of its days", ledgerTSPath)
	}
}

// TestOnlyTheGatedCallersLinkToADay: a new screen linking to /days/$date is not
// wrong, but it has to answer the grain question, so it has to be added here.
func TestOnlyTheGatedCallersLinkToADay(t *testing.T) {
	// The Day page navigates within itself (previous, next, the picker, Today) and
	// the Ledger row links out; everything else goes through the hook.
	allowed := map[string]bool{
		"day-page.tsx":            true,
		"ledger-detail-table.tsx": true,
	}

	entries, err := os.ReadDir(spaComponents)
	if err != nil {
		t.Fatalf("read %s: %v", spaComponents, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tsx") {
			continue
		}
		src := readFileText(t, filepath.Join(spaComponents, e.Name()))
		if strings.Contains(src, `"/days/$date"`) && !allowed[e.Name()] {
			t.Errorf("%s links to a Day directly: route it through useDayNavigation, or add it here with the grain it guarantees", e.Name())
		}
	}
}
