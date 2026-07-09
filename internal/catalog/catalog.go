// Package catalog defines Verve's Catalog: the closed but extensible set of
// canonical Metrics (CONTEXT.md, ADR 0002), each with a neutral slug (heart_rate,
// never HKQuantityTypeIdentifierHeartRate) and one canonical unit. An imported
// Metric carries an aggregation rule; a derived Metric carries a Formula computed
// per bucket on read (ADR 0014). A Connector owns the mapping to these slugs;
// seeded broadly (ADR 0011).
package catalog

// Aggregation is a Metric's rule for collapsing many points into one time bucket.
type Aggregation string

const (
	Sum             Aggregation = "sum"               // total (steps, calories, nutrients)
	Average         Aggregation = "average"           // mean, with a min/max band (heart rate)
	Latest          Aggregation = "latest"            // most recent point (body mass, height)
	DurationByState Aggregation = "duration_by_state" // time per state (sleep); unused in this slice
)

// Nature distinguishes Metrics produced by a Connector from those computed on read.
type Nature string

const (
	Imported Nature = "imported" // produced by a Connector, carries its own rule
	Derived  Nature = "derived"  // defined by a Formula, computed on read (ADR 0014)
)

// Metric is one canonical Catalog entry. Imported and Derived are mutually
// exclusive shapes: an Imported Metric carries an Aggregation and no Formula, a
// Derived Metric a Formula and no Aggregation (ADR 0014).
type Metric struct {
	Slug        string      // stable neutral identifier, persisted with every Measurement
	Unit        string      // single canonical unit (for derived, what the Formula produces)
	Aggregation Aggregation // how imported points collapse; empty for derived
	Nature      Nature      // Imported or Derived
	Formula     *Formula    // derived only; nil for imported (ADR 0014)
	Signed      bool        // derived value meaningfully negative → diverging render
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

	derived := derivedMetrics()
	m := make(map[string]Metric, len(rows)+len(derived))
	for _, r := range rows {
		m[r.slug] = Metric{Slug: r.slug, Unit: r.unit, Aggregation: r.agg, Nature: Imported}
	}
	for _, d := range derived {
		m[d.Slug] = d
	}
	return m
}

// derivedMetrics declares the seed derived Metrics as data (ADR 0014): each a
// Formula and unit, no aggregation rule. formula_test validates them at build time.
func derivedMetrics() []Metric {
	return []Metric{
		// total_energy_expenditure = active_energy + basal_energy (kcal).
		{
			Slug:   "total_energy_expenditure",
			Unit:   "kcal",
			Nature: Derived,
			Formula: &Formula{
				Scale: 1,
				Numerator: []Term{
					{Metric: "active_energy", Coefficient: 1},
					{Metric: "basal_energy", Coefficient: 1},
				},
			},
		},
		// calorie_balance = dietary_energy − active_energy − basal_energy
		// (kcal, signed: negative on a deficit).
		{
			Slug:   "calorie_balance",
			Unit:   "kcal",
			Nature: Derived,
			Signed: true,
			Formula: &Formula{
				Scale: 1,
				Numerator: []Term{
					{Metric: "dietary_energy", Coefficient: 1},
					{Metric: "active_energy", Coefficient: -1},
					{Metric: "basal_energy", Coefficient: -1},
				},
			},
		},
		// protein_per_kg = dietary_protein / body_mass (g/kg).
		{
			Slug:   "protein_per_kg",
			Unit:   "g/kg",
			Nature: Derived,
			Formula: &Formula{
				Scale:       1,
				Numerator:   []Term{{Metric: "dietary_protein", Coefficient: 1}},
				Denominator: []Term{{Metric: "body_mass", Coefficient: 1}},
			},
		},
		// protein_energy_share = 100 · 4·dietary_protein / dietary_energy (%),
		// the Atwater factor 4 kcal/g turning grams into their energy share.
		{
			Slug:   "protein_energy_share",
			Unit:   "%",
			Nature: Derived,
			Formula: &Formula{
				Scale:       100,
				Numerator:   []Term{{Metric: "dietary_protein", Coefficient: 4}},
				Denominator: []Term{{Metric: "dietary_energy", Coefficient: 1}},
			},
		},
		// carb_energy_share = 100 · 4·dietary_carbohydrates / dietary_energy (%).
		{
			Slug:   "carb_energy_share",
			Unit:   "%",
			Nature: Derived,
			Formula: &Formula{
				Scale:       100,
				Numerator:   []Term{{Metric: "dietary_carbohydrates", Coefficient: 4}},
				Denominator: []Term{{Metric: "dietary_energy", Coefficient: 1}},
			},
		},
		// fat_energy_share = 100 · 9·dietary_fat_total / dietary_energy (%),
		// the Atwater factor 9 kcal/g for fat.
		{
			Slug:   "fat_energy_share",
			Unit:   "%",
			Nature: Derived,
			Formula: &Formula{
				Scale:       100,
				Numerator:   []Term{{Metric: "dietary_fat_total", Coefficient: 9}},
				Denominator: []Term{{Metric: "dietary_energy", Coefficient: 1}},
			},
		},
	}
}
