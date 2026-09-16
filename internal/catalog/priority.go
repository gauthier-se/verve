package catalog

import "strings"

// SourceManual is the reserved Source of a Manual entry — a Measurement the
// Account typed rather than a Connector imported (ADR 0022). It deliberately does
// **not** appear in sourcePriority: priority elects a winning Source per day (ADR
// 0034), so ranking a hand-typed value first would make one entry the winner of every
// day it happened to fall on and hide the device readings around it. A Manual entry displaces
// imported data through the Manual overlay instead — day by day, in the query
// engine's source predicate.
const SourceManual = "Manual"

// sourcePriority resolves read-time Source overlap for a Metric (ADR 0003): an
// ordered list of case-insensitive substrings matched against Source names
// (substrings, since real names are device-specific like "Gauthier's Apple Watch").
//
// The substring match is not laziness, it is load-bearing: Apple exports device
// names containing a **non-breaking space** (U+00A0, bytes C2 A0), so the real stored
// Source is "Apple Watch de Gauthier" and an exact comparison against a
// hand-typed "Apple Watch …" silently matches nothing. Matching lowercase substrings
// like "watch" sidesteps the whole class of whitespace and localization surprises in
// vendor-supplied names.
// Only Metrics prone to harmful overlap need an entry; the rest fall back to
// alphabetical order (ResolveSource).
// A Source ending in "Health Kit" is Google Health's copy of a HealthKit row: the
// same measurement, one aggregator further from the device that took it. It ranks
// behind anything recorded natively and ahead of nothing. "Apple Health Health Kit"
// is the HealthKit aggregate, whatever device produced it, and "Phone Health Kit"
// is explicitly the phone, so the aggregate is matched by its own pattern first
// rather than left to tie and break alphabetically. Names come from a reference
// Takeout; a Source matching none of these still ranks last, as it always did.
var sourcePriority = map[string][]string{
	// Watch and iPhone both count steps when worn together, double-counting the
	// total; prefer the Watch, which is worn more continuously.
	"steps":                    {"watch", "iphone", "apple health", "health kit"},
	"distance_walking_running": {"watch", "iphone"},
	// Google reports one undifferentiated distance, so its Sources are ranked here
	// rather than under the per-activity Metrics Apple splits into.
	"distance":        {"watch", "iphone", "apple health", "health kit"},
	"flights_climbed": {"watch", "iphone"},
	// Heart rate had no entry until a second Source existed, which meant every
	// Source tied and the winner was decided by the alphabet: "Apple Health Health
	// Kit" sorts before "Apple Watch …", so a mirror covering months could take a
	// window covering years. Day-grain resolution (ADR 0034) stops that from
	// blanking anything; this decides the days both of them cover.
	"heart_rate":         {"watch", "apple health", "health kit"},
	"resting_heart_rate": {"watch", "apple health", "health kit"},
	// Sleep resolves per Night rather than per range (ADR 0027), so this ordering
	// breaks a tie between two Sources that both staged the *same* night: the Watch
	// is the one actually on the wrist. The nights it missed still come from the
	// iPhone, which is why the two are not ranked over the whole window.
	"sleep": {"watch", "iphone"},

	// --- Energy ---
	// The Watch measures an all-day figure. Yazio, Nike Run Club and Strava each write
	// back the energy of *one workout* that the Watch has already counted inside that
	// figure, so they are not rival measurements of the same quantity — they are a
	// subset of it, and electing one replaces a day with a run. On the reference
	// Account these overlap on 777 days, with daily means of Watch 1043, Strava 747,
	// Nike 562, Yazio 521, iPhone 410: a factor of 2.5 that was being settled by
	// sort.Strings, and settled correctly only because Apple names its devices with a
	// leading "A".
	"active_energy":       {"watch", "iphone", "apple health", "health kit"},
	"basal_energy":        {"watch", "iphone", "apple health", "health kit"},
	"total_energy_burned": {"watch", "iphone", "apple health", "health kit"},
	"apple_exercise_time": {"watch", "iphone"},
	"apple_stand_time":    {"watch", "iphone"},

	// --- Body composition ---
	// The scale is the instrument and its name is an open set, so the wildcard holds
	// the position of "some scale we have not heard of" and the food logs that mirror
	// its reading rank behind it. Yazio and FatSecret overlap the scale on 350+ days
	// of the reference Account and won every one of them on the alphabet.
	"body_mass":           {SourceWildcard, "yazio", "fatsecret", "health kit"},
	"body_mass_index":     {SourceWildcard, "yazio", "fatsecret", "health kit"},
	"body_fat_percentage": {SourceWildcard, "yazio", "fatsecret", "health kit"},
	"lean_body_mass":      {SourceWildcard, "yazio", "fatsecret", "health kit"},

	// --- Watch-measured, mirrored elsewhere ---
	"vo2_max":           {"watch", "apple health", "health kit"},
	"oxygen_saturation": {"watch", "iphone", "apple health", "health kit"},

	// Two devices playing audio at *different* times is genuinely additive, and
	// electing one discards the other's hours. Merging complementary Sources is where
	// ROADMAP has it; until then the Watch is the better of two wrong answers, and
	// this entry is where that note lives rather than in a commit message.
	"headphone_audio_exposure": {"watch", "iphone"},
}

// Every dietary_* Metric takes the same ranking, built rather than typed so that a
// nutrient added to the Catalog later cannot be silently left out — which is how this
// table fell behind in the first place.
//
// Unlike every other group here, neither Source is closer to the food than the other:
// Yazio and FatSecret are both food logs and the choice between them is genuinely
// arbitrary. It is ranked anyway, because arbitrary-and-stated beats arbitrary-and-
// alphabetical, and because day-grain election (ADR 0034) already leaves the loser
// every day the winner did not record.
func init() {
	for slug := range metrics {
		if strings.HasPrefix(slug, "dietary_") {
			sourcePriority[slug] = []string{"yazio", "fatsecret", "health kit"}
		}
	}
}

// SourcePriority returns the configured ordered priority patterns for a Metric,
// or nil if the Metric has no explicit priority (then Sources resolve
// alphabetically). The returned slice must not be mutated.
func SourcePriority(slug string) []string {
	return sourcePriority[slug]
}

// SourceWildcard is the position, inside a priority list, of every Source the list
// does not name.
//
// Without it a list can only *promote*: an unmatched Source ranks after every matched
// one, so there is no way to say "this named Source ranks **behind** whatever else
// turns up". That is exactly what a body mass needs. The instrument is a scale and
// scale names are an open set — Zepp Life, Withings, Renpho, a Garmin Index — so
// enumerating them is the losing half of ADR 0011. What *is* closed and knowable is
// the short list of apps that hold a copy: a food log never weighed anything.
//
// So `{"*", "yazio", "fatsecret"}` reads "any scale, then Yazio, then FatSecret", and
// a list with no wildcard behaves exactly as it always has.
const SourceWildcard = "*"

// ResolveSource picks the winning Source for a Metric from those with data
// (available), or "" and false when empty. Sources rank by the first priority
// pattern their name contains; a Source matching none takes the list's
// SourceWildcard position, or last when the list has none; ties break alphabetically.
//
// The election this feeds runs per **day** (ADR 0034), so "the winning Source" means
// the winner among those that recorded something that day — which is why a list may
// safely name a Source that covers only part of a history.
func ResolveSource(slug string, available []string) (string, bool) {
	if len(available) == 0 {
		return "", false
	}

	patterns := sourcePriority[slug]
	rank := func(source string) int {
		lower := strings.ToLower(source)
		// The wildcard is resolved after the named patterns, never inside the loop: it
		// matches everything, so matching it first would swallow a Source that a later
		// pattern names — the demotion would demote its own targets.
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

	winner := available[0]
	winnerRank := rank(winner)
	for _, s := range available[1:] {
		r := rank(s)
		if r < winnerRank || (r == winnerRank && s < winner) {
			winner, winnerRank = s, r
		}
	}
	return winner, true
}
