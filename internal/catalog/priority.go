package catalog

import "strings"

// Source priority resolves the read-time overlap between Sources that report
// the same Metric (ADR 0003). Verve keeps every Measurement from every Source
// (non-destructive); when a graph needs one series it must pick a single Source
// so it never double-counts — e.g. steps recorded by both the Watch and the
// iPhone worn together.
//
// A priority is an ordered list of case-insensitive substrings matched against
// a Source's name. Substrings (not exact names) because real Source strings are
// device-specific — "Gauthier's Apple Watch", not a clean "Apple Watch" — so
// "watch" matches the family without hard-coding every device name.
//
// Only Metrics prone to harmful overlap need an entry; everything else resolves
// deterministically by falling back to alphabetical order (see ResolveSource).
var sourcePriority = map[string][]string{
	// Watch and iPhone both count steps when worn together, double-counting the
	// total; prefer the Watch, which is worn more continuously.
	"steps":                    {"watch", "iphone"},
	"distance_walking_running": {"watch", "iphone"},
	"flights_climbed":          {"watch", "iphone"},
}

// SourcePriority returns the configured ordered priority patterns for a Metric,
// or nil if the Metric has no explicit priority (then Sources resolve
// alphabetically). The returned slice must not be mutated.
func SourcePriority(slug string) []string {
	return sourcePriority[slug]
}

// ResolveSource picks the single winning Source for a Metric from the Sources
// that actually have data in the queried range (available). It returns the
// winner and true, or "" and false when available is empty.
//
// Sources are ranked by the index of the first priority pattern their name
// contains (case-insensitive); a Source matching no pattern ranks after every
// matched one. Ties — including the common case of a Metric with no configured
// priority, where every Source is unranked — break alphabetically, so the choice
// is always deterministic.
//
// This resolves the whole range to one Source. Per-bucket resolution and
// merging complementary Sources are deferred refinements (ADR 0003).
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
