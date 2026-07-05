// Package catalog defines Verve's Catalog: the closed but extensible set of
// canonical Metrics the system understands (see CONTEXT.md and ADR 0002). Each
// Metric has a neutral, source-independent slug (heart_rate, never
// HKQuantityTypeIdentifierHeartRate), one canonical unit, and an aggregation
// rule that decides how points collapse into a time bucket.
//
// The Catalog is source-independent: it names *what* Verve stores, not *how*
// any Source spells it. A Connector owns the mapping from its own vocabulary to
// these slugs (see internal/connector/applehealth). Per ADR 0011 the Catalog is
// seeded broadly — nearly every scalar Apple Health type — so an import
// captures almost everything even though the v1 UI graphs only a subset.
package catalog

// Aggregation is a Metric's rule for collapsing many points into one time
// bucket. The rule, not the user, decides how a series aggregates.
type Aggregation string

const (
	// Sum totals the points in a bucket (steps, calories, nutrients).
	Sum Aggregation = "sum"
	// Average means the bucket value is the mean, typically shown with a
	// min/max band (heart rate, speed).
	Average Aggregation = "average"
	// Latest takes the most recent point in the bucket (body mass, height).
	Latest Aggregation = "latest"
	// DurationByState sums time spent per categorical state (sleep). No scalar
	// Metric in this slice uses it; it is part of the canonical shape.
	DurationByState Aggregation = "duration_by_state"
)

// Nature distinguishes Metrics produced by a Connector from those computed on
// read. Only Imported Metrics exist in this slice.
type Nature string

const (
	// Imported Metrics are produced by a Connector from a Source.
	Imported Nature = "imported"
	// Derived Metrics are defined by a formula over other Metrics (v2).
	Derived Nature = "derived"
)

// Metric is one canonical entry in the Catalog.
type Metric struct {
	// Slug is the stable, neutral identifier persisted with every Measurement.
	Slug string
	// Unit is the single canonical unit; Connectors normalize to it on import.
	Unit string
	// Aggregation is how points collapse into a time bucket.
	Aggregation Aggregation
	// Nature is Imported or Derived.
	Nature Nature
}

// metrics declares the Catalog as data. Canonical units follow Apple Health's
// choices for the seeded types (already sensible), so import is an identity
// conversion for an Apple source while other Sources still normalize via
// internal/units.
var metrics = buildMetrics()

// Lookup returns the Metric for slug and whether it exists in the Catalog.
func Lookup(slug string) (Metric, bool) {
	m, ok := metrics[slug]
	return m, ok
}

// All returns every Catalog Metric keyed by slug. The returned map must not be
// mutated by callers.
func All() map[string]Metric {
	return metrics
}

func buildMetrics() map[string]Metric {
	// Compact declaration: {slug, unit, aggregation}. Every entry is Imported.
	rows := []struct {
		slug string
		unit string
		agg  Aggregation
	}{
		// --- Energy ---
		{"active_energy", "kcal", Sum},
		{"basal_energy", "kcal", Sum},

		// --- Body ---
		{"body_mass", "kg", Latest},
		{"body_mass_index", "count", Latest},
		{"body_fat_percentage", "%", Latest},
		{"lean_body_mass", "kg", Latest},
		{"height", "cm", Latest},

		// --- Activity ---
		{"steps", "count", Sum},
		{"distance_walking_running", "km", Sum},
		{"distance_cycling", "km", Sum},
		{"flights_climbed", "count", Sum},
		{"physical_effort", "kcal/hr·kg", Average},
		{"apple_exercise_time", "min", Sum},
		{"apple_stand_time", "min", Sum},
		{"time_in_daylight", "min", Sum},
		{"walking_speed", "km/hr", Average},
		{"walking_step_length", "cm", Average},
		{"walking_double_support_percentage", "%", Average},
		{"walking_asymmetry_percentage", "%", Average},
		{"walking_steadiness", "%", Average},
		{"stair_ascent_speed", "m/s", Average},
		{"stair_descent_speed", "m/s", Average},
		{"six_minute_walk_test_distance", "m", Latest},
		{"running_speed", "km/hr", Average},
		{"running_power", "W", Average},
		{"running_stride_length", "m", Average},
		{"running_ground_contact_time", "ms", Average},
		{"running_vertical_oscillation", "cm", Average},

		// --- Heart & circulation ---
		{"heart_rate", "count/min", Average},
		{"resting_heart_rate", "count/min", Average},
		{"walking_heart_rate_average", "count/min", Average},
		{"heart_rate_variability_sdnn", "ms", Average},
		{"heart_rate_recovery_one_minute", "count/min", Average},
		{"vo2_max", "mL/min·kg", Average},

		// --- Respiratory & vitals ---
		{"respiratory_rate", "count/min", Average},
		{"oxygen_saturation", "%", Average},
		{"apple_sleeping_breathing_disturbances", "count", Average},
		{"apple_sleeping_wrist_temperature", "degC", Average},

		// --- Audio exposure ---
		{"headphone_audio_exposure", "dBASPL", Average},
		{"environmental_audio_exposure", "dBASPL", Average},
		{"environmental_sound_reduction", "dBASPL", Average},

		// --- Nutrition: energy & macros ---
		{"dietary_energy", "kcal", Sum},
		{"dietary_protein", "g", Sum},
		{"dietary_carbohydrates", "g", Sum},
		{"dietary_fat_total", "g", Sum},
		{"dietary_fat_saturated", "g", Sum},
		{"dietary_fat_monounsaturated", "g", Sum},
		{"dietary_fat_polyunsaturated", "g", Sum},
		{"dietary_fiber", "g", Sum},
		{"dietary_sugar", "g", Sum},
		{"dietary_cholesterol", "mg", Sum},
		{"dietary_water", "mL", Sum},

		// --- Nutrition: minerals ---
		{"dietary_sodium", "mg", Sum},
		{"dietary_potassium", "mg", Sum},
		{"dietary_calcium", "mg", Sum},
		{"dietary_iron", "mg", Sum},
		{"dietary_magnesium", "mg", Sum},
		{"dietary_phosphorus", "mg", Sum},
		{"dietary_zinc", "mg", Sum},
		{"dietary_copper", "mg", Sum},
		{"dietary_manganese", "mg", Sum},
		{"dietary_selenium", "mcg", Sum},
		{"dietary_iodine", "mcg", Sum},
		{"dietary_chloride", "mg", Sum},
		{"dietary_chromium", "mcg", Sum},
		{"dietary_molybdenum", "mcg", Sum},

		// --- Nutrition: vitamins ---
		{"dietary_vitamin_a", "mcg", Sum},
		{"dietary_vitamin_c", "mg", Sum},
		{"dietary_vitamin_d", "mcg", Sum},
		{"dietary_vitamin_e", "mg", Sum},
		{"dietary_vitamin_k", "mcg", Sum},
		{"dietary_thiamin", "mg", Sum},
		{"dietary_riboflavin", "mg", Sum},
		{"dietary_niacin", "mg", Sum},
		{"dietary_vitamin_b6", "mg", Sum},
		{"dietary_vitamin_b12", "mcg", Sum},
		{"dietary_folate", "mcg", Sum},
		{"dietary_pantothenic_acid", "mg", Sum},
		{"dietary_biotin", "mcg", Sum},

		// --- Clinical & extended vitals ---
		// Not present in the reference export but common Apple scalar types, so
		// an import from a device or app that records them is captured too
		// (ADR 0011). Canonical units follow Apple's documented choices.
		{"blood_pressure_systolic", "mmHg", Average},
		{"blood_pressure_diastolic", "mmHg", Average},
		{"blood_glucose", "mg/dL", Average},
		{"body_temperature", "degC", Average},
		{"basal_body_temperature", "degC", Average},
		{"peripheral_perfusion_index", "%", Average},
		{"electrodermal_activity", "mcS", Average},
		{"blood_alcohol_content", "%", Average},
		{"forced_vital_capacity", "L", Average},
		{"forced_expiratory_volume_1", "L", Average},
		{"peak_expiratory_flow_rate", "L/min", Average},

		// --- Extended activity & body ---
		{"distance_swimming", "m", Sum},
		{"swimming_stroke_count", "count", Sum},
		{"distance_downhill_snow_sports", "km", Sum},
		{"distance_wheelchair", "km", Sum},
		{"push_count", "count", Sum},
		{"uv_exposure", "count", Average},
		{"waist_circumference", "cm", Latest},

		// --- Nutrition (extended) ---
		{"dietary_caffeine", "mg", Sum},

		// --- Events & symptoms (scalar counts) ---
		{"inhaler_usage", "count", Sum},
		{"number_of_times_fallen", "count", Sum},

		// --- Miscellaneous ---
		{"number_of_alcoholic_beverages", "count", Sum},
	}

	m := make(map[string]Metric, len(rows))
	for _, r := range rows {
		m[r.slug] = Metric{Slug: r.slug, Unit: r.unit, Aggregation: r.agg, Nature: Imported}
	}
	return m
}
