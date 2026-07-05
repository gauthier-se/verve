package applehealth

import "strings"

// This file holds the Apple Health Connector's declarative mapping for the
// non-scalar families — States (sleep, stand hours) and Sessions (workouts).
// Like typeToMetric it is data, not logic (ADR 0009): the code only knows "how
// to read the source", the maps say "what it means".

// categoryStateKinds maps an Apple HKCategoryType* that Verve models as a State
// to its neutral kind. Category types absent here (LowHeartRateEvent, audio
// exposure events…) are Events — out of scope for this slice — and fall through
// to the Unmapped bin, kept for a later slice, never discarded (ADR 0002).
var categoryStateKinds = map[string]string{
	"HKCategoryTypeIdentifierSleepAnalysis":  "sleep",
	"HKCategoryTypeIdentifierAppleStandHour": "stand",
}

// stateValues maps an Apple HKCategoryValue* to its neutral phase slug. A value
// absent here but under a mapped kind still becomes a State: normalizeStateValue
// derives a slug from it rather than dropping the row (ADR 0002).
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

// normalizeStateValue returns the neutral phase slug for an Apple category
// value. Known values use the curated table; an unknown value under a mapped
// kind is derived (prefix trimmed, snake-cased) so it is kept, never dropped.
func normalizeStateValue(appleValue string) string {
	if slug, ok := stateValues[appleValue]; ok {
		return slug
	}
	return snakeFromCamel(trimKnownPrefix(appleValue, "HKCategoryValue"))
}

// normalizeActivityType returns the neutral activity slug for an Apple workout
// activity type, derived by trimming the HKWorkoutActivityType prefix and
// snake-casing (e.g. TraditionalStrengthTraining → traditional_strength_training).
// Derivation rather than a table keeps every one of Apple's ~80 activities
// covered without maintenance.
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

// snakeFromCamel lowercases an UpperCamelCase identifier into snake_case,
// inserting an underscore before each interior capital (Running → running,
// MixedCardio → mixed_cardio). Runs of capitals are treated one letter at a
// time, which is fine for the workout/category vocabulary Verve derives from.
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
