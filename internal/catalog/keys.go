package catalog

import "strings"

// uncuratedPrefixes are the prefixes that the key of an uncurated type drops.
var uncuratedPrefixes = []string{"HKQuantityTypeIdentifier", "HKCategoryTypeIdentifier"}

// UncuratedKey returns the key of an uncurated type of identifier, by the rule of FR-2 of
// docs/features/data-storage-and-aggregates.md, before the suffix that a key already taken gets.
func UncuratedKey(identifier string) string {
	for _, prefix := range uncuratedPrefixes {
		if rest, ok := strings.CutPrefix(identifier, prefix); ok {
			identifier = rest

			break
		}
	}

	var b strings.Builder

	// split is set once a word ends, so that the next letter or digit starts another one.
	split := false

	for i := range len(identifier) {
		c := identifier[i]
		if !isLower(c) && !isUpper(c) && !isDigit(c) {
			split = true

			continue
		}

		if i > 0 && isUpper(c) {
			prev := identifier[i-1]
			endsRun := isUpper(prev) && i+1 < len(identifier) && isLower(identifier[i+1])

			if isLower(prev) || isDigit(prev) || endsRun {
				split = true
			}
		}

		if split && b.Len() > 0 {
			b.WriteByte('_')
		}

		split = false

		if isUpper(c) {
			c += 'a' - 'A'
		}

		b.WriteByte(c)
	}

	if b.Len() == 0 {
		return "uncurated"
	}

	return b.String()
}

// isUpper, isLower and isDigit test for ASCII only: any other character separates words.
func isUpper(c byte) bool { return 'A' <= c && c <= 'Z' }

func isLower(c byte) bool { return 'a' <= c && c <= 'z' }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
