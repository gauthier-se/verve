package googlehealth

// The mapping is data, not logic (ADR 0009). Every unit below is read from the
// README Google ships beside the file it describes, never inferred from the
// magnitude of the numbers: the difference between "weight grams" and a bare
// "weight" is the difference between 93.4 kg and 93,400.

// csvMetric maps one Physical Activity family to a Catalog Metric. Column is the
// header cell carrying the value and Unit what that column is in, converted to the
// Metric's canonical unit at import through internal/units.
type csvMetric struct {
	Metric string
	Column string
	Unit   string
}

// familyToMetric maps a Physical Activity family, a filename with its date shard
// removed, to the Catalog. A family absent from this table is kept in the Unmapped
// bin rather than dropped (ADR 0002).
var familyToMetric = map[string]csvMetric{
	"active_energy_burned": {"active_energy", "Kilocalories", "kcal"},
	"steps":                {"steps", "steps", "count"},
	"distance":             {"distance", "distance", "m"},
	"heart_rate":           {"heart_rate", "beats per minute", "count/min"},
	"height":               {"height", "height millimeters", "mm"},
	"weight":               {"body_mass", "weight grams", "g"},
	"body_fat":             {"body_fat_percentage", "body fat percentage", "%"},
	"calories":             {"total_energy_burned", "calories", "kcal"},

	// Google publishes resting heart rate and VO2 max twice, once per reading and once
	// as a daily figure. Both are mapped and neither is special-cased: where the two
	// carry the same reading, the Content key collapses them (ADR 0006), and where
	// they do not, daily_vo2_max is computed by Google, vo2_max measured by the
	// device, they are two Sources of one Metric, which Source priority is for.
	"resting_heart_rate":       {"resting_heart_rate", "beats per minute", "count/min"},
	"daily_resting_heart_rate": {"resting_heart_rate", "beats per minute", "count/min"},
	"vo2_max":                  {"vo2_max", "vo2 max value", "mL/min·kg"},
	"daily_vo2_max":            {"vo2_max", "daily vo2 max value", "mL/min·kg"},
}

// MappedSlugs returns the Catalog Metrics this Connector claims. It exists for the
// Catalog's coverage test, which asks the registered Connectors rather than one of
// them: the invariant is that the Catalog has no orphan, not that any single source
// covers it.
func MappedSlugs() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range familyToMetric {
		if !seen[m.Metric] {
			seen[m.Metric] = true
			out = append(out, m.Metric)
		}
	}
	return out
}
