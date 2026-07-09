package applehealth

import "strings"

// Declarative mapping for the non-scalar families — States and Sessions — data,
// not logic (ADR 0009).

// categoryStateKinds maps an Apple category type Verve models as a State to its
// neutral kind. Absent types fall through to the Unmapped bin (ADR 0002).
var categoryStateKinds = map[string]string{
	"HKCategoryTypeIdentifierSleepAnalysis":  "sleep",
	"HKCategoryTypeIdentifierAppleStandHour": "stand",
}

// stateValues maps an Apple category value to its neutral phase slug; an absent
// value is derived by normalizeStateValue, not dropped (ADR 0002).
var stateValues = map[string]string{
	"HKCategoryValueSleepAnalysisInBed":             "in_bed",
	"HKCategoryValueSleepAnalysisAsleepUnspecified": "asleep",
	"HKCategoryValueSleepAnalysisAsleepCore":        "asleep_core",
	"HKCategoryValueSleepAnalysisAsleepDeep":        "asleep_deep",
	"HKCategoryValueSleepAnalysisAsleepREM":         "asleep_rem",
	"HKCategoryValueSleepAnalysisAwake":             "awake",
	"HKCategoryValueAppleStandHourStood":            "stood",
	"HKCategoryValueAppleStandHourIdle":             "idle",
}

// stateKind reports the neutral kind for an Apple category type and whether it
// is a State Verve ingests in this slice.
func stateKind(appleType string) (string, bool) {
	kind, ok := categoryStateKinds[appleType]
	return kind, ok
}

// normalizeStateValue returns the neutral phase slug: the curated table, or a
// derived slug (prefix trimmed, snake-cased) so an unknown value is kept.
func normalizeStateValue(appleValue string) string {
	if slug, ok := stateValues[appleValue]; ok {
		return slug
	}
	return snakeFromCamel(trimKnownPrefix(appleValue, "HKCategoryValue"))
}

// normalizeActivityType derives the neutral activity slug by trimming the
// HKWorkoutActivityType prefix and snake-casing, covering all of Apple's ~80
// activities without a table.
func normalizeActivityType(appleType string) string {
	return snakeFromCamel(trimKnownPrefix(appleType, "HKWorkoutActivityType"))
}

// trimKnownPrefix strips prefix from s if present, else returns s unchanged.
func trimKnownPrefix(s, prefix string) string {
	if t, ok := strings.CutPrefix(s, prefix); ok {
		return t
	}
	return s
}

// snakeFromCamel lowercases UpperCamelCase into snake_case (MixedCardio →
// mixed_cardio); runs of capitals split per letter, fine for this vocabulary.
func snakeFromCamel(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
