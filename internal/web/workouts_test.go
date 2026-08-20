package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
)

// The workout screens span two languages, like the sleep stages next door. The
// Catalog owns the Activity display table because the group is a server-side
// filter; the SPA owns only the icons, and the map's tile layer. Nothing at
// either end fails when they drift: an Activity group with no icon renders a
// hole, and a tile layer that stops being conditional starts making outbound
// requests that nobody notices from the inside. Both files are read as text, so
// this runs in `make ci` with no front-end toolchain.
const (
	activitiesTSPath  = "../../web/src/lib/activities.ts"
	routeMapTSXPath   = "../../web/src/components/route-map.tsx"
	sessionDetailPath = "../../web/src/components/session-detail.tsx"
)

var (
	// BY_GROUP: { cardio: Heart, … }
	byGroupRe = regexp.MustCompile(`(?s)BY_GROUP: Record<Activity\["group"\], LucideIcon> = \{(.*?)\n\}`)
	// EXPLICIT: { running: Footprints, … }
	explicitRe = regexp.MustCompile(`(?s)EXPLICIT: Record<string, LucideIcon> = \{(.*?)\n\}`)
	// ACTIVITY_GROUPS = [{ value: "cardio", … }]
	groupListRe = regexp.MustCompile(`(?s)ACTIVITY_GROUPS: \{ value: Activity\["group"\]; label: string \}\[\] = \[(.*?)\n\]`)
	valueRe     = regexp.MustCompile(`value: "(\w+)"`)
)

// TestActivityGroupsHaveIcons: every group the Catalog can put on a Session must
// have a fallback icon and a filter button. A group added server-side without
// either renders a blank cell and an unreachable filter.
func TestActivityGroupsHaveIcons(t *testing.T) {
	src := readFileText(t, activitiesTSPath)
	icons := tsKeyRe.FindAllStringSubmatch(section(t, src, byGroupRe, "BY_GROUP"), -1)
	buttons := valueRe.FindAllStringSubmatch(section(t, src, groupListRe, "ACTIVITY_GROUPS"), -1)

	have := map[string]bool{}
	for _, m := range icons {
		have[m[1]] = true
	}
	shown := map[string]bool{}
	for _, m := range buttons {
		shown[m[1]] = true
	}

	for _, g := range catalog.Groups() {
		if !have[string(g)] {
			t.Errorf("Activity group %q has no icon in %s", g, activitiesTSPath)
		}
		if !shown[string(g)] {
			t.Errorf("Activity group %q has no filter button in %s", g, activitiesTSPath)
		}
	}
	for slug := range have {
		if !catalog.IsGroup(slug) {
			t.Errorf("%s has an icon for %q, which is not an Activity group", activitiesTSPath, slug)
		}
	}
}

// TestExplicitActivityIconsAreRealSlugs: an icon keyed on a slug the Connector
// can never produce is dead code that looks like coverage. The fallback hides the
// typo, so only this test can find it.
func TestExplicitActivityIconsAreRealSlugs(t *testing.T) {
	src := readFileText(t, activitiesTSPath)
	for _, m := range tsKeyRe.FindAllStringSubmatch(section(t, src, explicitRe, "EXPLICIT"), -1) {
		slug := m[1]
		if got := catalog.LookupActivity(slug); got.Label == "" || got.Slug != slug {
			t.Errorf("icon slug %q is not resolvable as an Activity", slug)
		}
		// LookupActivity never fails, so check the curated table itself: an
		// unknown slug falls back to Other, which is what a typo looks like.
		if catalog.LookupActivity(slug).Group == catalog.GroupOther && slug != "other" {
			t.Errorf("icon slug %q is not in the Catalog's curated Activity table (typo?)", slug)
		}
	}
}

// TestMapDrawsNoTilesWithoutConfiguration is the guard on the promise that Verve
// makes no outbound request unless its owner asked for one (ADR 0028). A default
// basemap is a one-line "improvement" that tells a third party where its owner
// runs, and nothing in the running app would look wrong.
func TestMapDrawsNoTilesWithoutConfiguration(t *testing.T) {
	src := readFileText(t, routeMapTSXPath)

	if n := strings.Count(src, "L.tileLayer"); n != 1 {
		t.Fatalf("L.tileLayer appears %d times in %s, want exactly 1 (inside the guard)", n, routeMapTSXPath)
	}
	guard := regexp.MustCompile(`if \(tiles\) \{\s*\n\s*L\.tileLayer\(tiles`)
	if !guard.MatchString(src) {
		t.Errorf("%s must add its tile layer only inside `if (tiles)`; a basemap must never be the default", routeMapTSXPath)
	}

	// A URL baked into the client is the other way this promise breaks.
	for _, host := range []string{"tile.openstreetmap", "https://", "http://"} {
		if strings.Contains(src, host) {
			t.Errorf("%s contains a hardcoded %q: the tile source is configuration, not code", routeMapTSXPath, host)
		}
	}
}

// TestPaceReadingComesFromTheServer: which of pace or speed a workout shows is a
// property of its Activity, carried on the payload. A component that decided by
// naming activities would silently disagree with the Catalog's table.
func TestPaceReadingComesFromTheServer(t *testing.T) {
	src := readFileText(t, sessionDetailPath)
	if !strings.Contains(src, `session.activity.reading === "pace"`) {
		t.Error("the detail page must read the pace/speed choice off the Activity")
	}
	for _, slug := range []string{"running", "cycling", "swimming"} {
		if strings.Contains(src, `"`+slug+`"`) {
			t.Errorf("the detail page names the Activity %q: the reading belongs to the Catalog's table", slug)
		}
	}
}
