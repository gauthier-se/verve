package catalog

import "strings"

// sourcePriority resolves read-time Source overlap for a Metric (ADR 0003): an
// ordered list of case-insensitive substrings matched against Source names
// (substrings, since real names are device-specific like "Gauthier's Apple Watch").
// Only Metrics prone to harmful overlap need an entry; the rest fall back to
// alphabetical order (ResolveSource).
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
