package catalog

import (
	"strings"
	"testing"
)

func TestResolveSourcePrefersConfiguredPriority(t *testing.T) {
	// steps prefers the Watch over the iPhone to avoid double-counting.
	got, ok := ResolveSource("steps", []string{"Gauthier's iPhone", "Gauthier's Apple Watch"})
	if !ok || got != "Gauthier's Apple Watch" {
		t.Fatalf("ResolveSource(steps) = %q, %v; want the Apple Watch", got, ok)
	}
}

func TestResolveSourceUnmatchedRankAfterMatched(t *testing.T) {
	// A Source matching no priority pattern loses to one that matches, even if
	// it sorts earlier alphabetically.
	got, ok := ResolveSource("steps", []string{"AAA Fitness App", "My Apple Watch"})
	if !ok || got != "My Apple Watch" {
		t.Fatalf("ResolveSource = %q, %v; want the matched Watch source", got, ok)
	}
}

func TestResolveSourceNoPriorityFallsBackAlphabetical(t *testing.T) {
	// This test named heart_rate until ADR 0034 gave heart_rate an entry, at which
	// point it passed because "Apple Watch" matched "watch" at rank 0 — the alphabet
	// it claimed to be testing was no longer involved. The guard below is the fix:
	// the slug is asserted to be unranked, so the day someone ranks it the test says
	// so instead of quietly testing something else.
	const slug = "respiratory_rate"
	if SourcePriority(slug) != nil {
		t.Fatalf("%s now has a priority list; this test no longer exercises the fallback", slug)
	}
	got, ok := ResolveSource(slug, []string{"Polar H10", "Apple Watch"})
	if !ok || got != "Apple Watch" {
		t.Fatalf("ResolveSource(%s) = %q, %v; want alphabetical first", slug, got, ok)
	}
}

// TestWildcardRanksBetweenNamedPatterns: on a body mass the scale wins, whatever it
// is called, and between two food logs the list's own order decides.
func TestWildcardRanksBetweenNamedPatterns(t *testing.T) {
	for _, scale := range []string{"Zepp Life", "Withings", "Renpho", "AAA Scale"} {
		got, _ := ResolveSource("body_mass", []string{"Yazio", scale})
		if got != scale {
			t.Errorf("ResolveSource(body_mass, [Yazio, %s]) = %q; want the scale", scale, got)
		}
	}
	// "AAA Scale" above is the case the alphabet would have got right anyway; the two
	// below are the ones it got wrong, since Y and F both sort before Z.
	if got, _ := ResolveSource("body_mass", []string{"Yazio", "Zepp Life"}); got != "Zepp Life" {
		t.Errorf("Yazio beat the scale: %q", got)
	}
	if got, _ := ResolveSource("body_mass", []string{"FatSecret", "Zepp Life"}); got != "Zepp Life" {
		t.Errorf("FatSecret beat the scale: %q", got)
	}
	// Among named Sources the list order holds, wildcard or not.
	if got, _ := ResolveSource("body_mass", []string{"FatSecret", "Yazio"}); got != "Yazio" {
		t.Errorf("ResolveSource(body_mass, [FatSecret, Yazio]) = %q; want Yazio", got)
	}
}

// TestWildcardDoesNotSwallowALaterPattern is the implementation trap: the wildcard
// matches every name, so resolving it inside the scan would rank Yazio at the
// wildcard's position and undo its own demotion.
func TestWildcardDoesNotSwallowALaterPattern(t *testing.T) {
	if got, _ := ResolveSource("body_mass", []string{"Yazio", "Some Scale"}); got != "Some Scale" {
		t.Errorf("wildcard swallowed a named pattern: %q won", got)
	}
}

// TestWildcardAbsentIsUnchanged: every list that existed before the wildcard must rank
// exactly as it did, so the change cannot have moved an existing Account's curves.
func TestWildcardAbsentIsUnchanged(t *testing.T) {
	cases := []struct {
		slug      string
		available []string
		want      string
	}{
		{"steps", []string{"Gauthier's iPhone", "Gauthier's Apple Watch"}, "Gauthier's Apple Watch"},
		{"steps", []string{"AAA Fitness App", "My Apple Watch"}, "My Apple Watch"},
		{"distance_walking_running", []string{"iPhone", "Apple Watch"}, "Apple Watch"},
		{"heart_rate", []string{"Apple Health Health Kit", "Apple Watch de G"}, "Apple Watch de G"},
		{"flights_climbed", []string{"iPhone", "Apple Watch"}, "Apple Watch"},
	}
	for _, c := range cases {
		if got, _ := ResolveSource(c.slug, c.available); got != c.want {
			t.Errorf("ResolveSource(%s, %v) = %q, want %q", c.slug, c.available, got, c.want)
		}
	}
}

// TestEveryOverlappingMetricIsRanked is the check the table lacked, and the reason it
// drifted: nothing could notice it drifting. Each pair below is two Source names that
// really do appear together on the reference exports, and for each the two must not
// tie — a tie means sort.Strings is deciding a real overlap.
func TestEveryOverlappingMetricIsRanked(t *testing.T) {
	const (
		watch  = "Apple\u00a0Watch de Gauthier" // Apple's real name, non-breaking space and all
		phone  = "iPhone de Gauthier"
		scale  = "Zepp Life"
		yazio  = "Yazio"
		google = "Apple Health Health Kit"
	)
	cases := []struct{ slug, a, b string }{
		{"steps", watch, phone},
		{"distance_walking_running", watch, phone},
		{"flights_climbed", watch, phone},
		{"heart_rate", watch, google},
		{"active_energy", watch, yazio},
		{"active_energy", watch, phone},
		{"active_energy", watch, "Nike Run Club"},
		{"active_energy", watch, "Strava"},
		{"basal_energy", watch, phone},
		{"apple_exercise_time", watch, phone},
		{"apple_stand_time", watch, phone},
		{"body_mass", scale, yazio},
		{"body_mass", scale, "FatSecret"},
		{"body_mass_index", scale, yazio},
		{"body_fat_percentage", scale, yazio},
		{"lean_body_mass", scale, yazio},
		{"dietary_energy", yazio, "FatSecret"},
		{"dietary_protein", yazio, "FatSecret"},
		{"dietary_vitamin_b12", yazio, "FatSecret"},
		{"vo2_max", watch, "Google Health"},
		{"oxygen_saturation", watch, "Oxygène sanguin"},
		{"headphone_audio_exposure", watch, phone},
	}
	for _, c := range cases {
		winner, ok := ResolveSource(c.slug, []string{c.a, c.b})
		if !ok {
			t.Fatalf("ResolveSource(%s) found nothing", c.slug)
		}
		// A tie is what we are hunting: reverse the input order and see whether the
		// answer moves for any reason other than the ranking. It cannot, because
		// ResolveSource is order-independent — so instead assert the ranking itself
		// separates them.
		reversed, _ := ResolveSource(c.slug, []string{c.b, c.a})
		if winner != reversed {
			t.Errorf("ResolveSource(%s) is order-dependent: %q vs %q", c.slug, winner, reversed)
		}
		if rankOf(c.slug, c.a) == rankOf(c.slug, c.b) {
			t.Errorf("%s: %q and %q tie at rank %d — the alphabet is deciding a real overlap",
				c.slug, c.a, c.b, rankOf(c.slug, c.a))
		}
	}
}

// rankOf mirrors ResolveSource's ranking for the assertions above: the test needs to
// see a tie, which the winner alone cannot show.
func rankOf(slug, source string) int {
	patterns := SourcePriority(slug)
	lower := strings.ToLower(source)
	wildcard := len(patterns)
	for i, p := range patterns {
		if p == SourceWildcard {
			wildcard = i
			continue
		}
		if strings.Contains(lower, p) {
			return i
		}
	}
	return wildcard
}

// TestEveryDietaryMetricIsRanked: the list is built from the Catalog rather than typed,
// so a nutrient added later is covered automatically. This is the assertion that the
// build actually does it.
func TestEveryDietaryMetricIsRanked(t *testing.T) {
	n := 0
	for slug := range All() {
		if !strings.HasPrefix(slug, "dietary_") {
			continue
		}
		n++
		if SourcePriority(slug) == nil {
			t.Errorf("%s has no priority list", slug)
		}
	}
	if n < 10 {
		t.Fatalf("only %d dietary Metrics found; the prefix or the Catalog changed", n)
	}
}

func TestResolveSourceEmpty(t *testing.T) {
	if got, ok := ResolveSource("steps", nil); ok || got != "" {
		t.Fatalf("ResolveSource(empty) = %q, %v; want \"\", false", got, ok)
	}
}
