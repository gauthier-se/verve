package catalog

import "strings"

// SourceManual is the reserved Source of a Manual entry — a Measurement the
// Account typed rather than a Connector imported (ADR 0022). It deliberately does
// **not** appear in sourcePriority: priority elects one winning Source for the whole
// range, so ranking a hand-typed value first would make one entry the winner of the
// entire window and hide every device reading around it. A Manual entry displaces
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
}

// SourcePriority returns the configured ordered priority patterns for a Metric,
// or nil if the Metric has no explicit priority (then Sources resolve
// alphabetically). The returned slice must not be mutated.
func SourcePriority(slug string) []string {
	return sourcePriority[slug]
}

// ResolveSource picks the winning Source for a Metric from those with data
// (available), or "" and false when empty. Sources rank by the first priority
// pattern their name contains; unmatched rank last; ties break alphabetically.
// Whole-range only — per-bucket resolution is deferred (ADR 0003).
func ResolveSource(slug string, available []string) (string, bool) {
	if len(available) == 0 {
		return "", false
	}

	patterns := sourcePriority[slug]
	rank := func(source string) int {
		lower := strings.ToLower(source)
		for i, p := range patterns {
			if strings.Contains(lower, p) {
				return i
			}
		}
		return len(patterns) // unmatched Sources sort after every matched one
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
