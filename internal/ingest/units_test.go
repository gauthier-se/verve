package ingest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// curated returns a curated type whose canonical unit is unit.
func curated(unit string) store.MetricType {
	return store.MetricType{Key: "test", Identifier: "HKQuantityTypeIdentifierTest", Unit: unit, Curated: true}
}

// uncurated returns an uncurated type whose unit, the one of its first record, is unit.
func uncurated(unit string) store.MetricType {
	return store.MetricType{Key: "test", Identifier: "HKQuantityTypeIdentifierTest", Unit: unit}
}

// Given a value and a unit listed by the Units table, when it is converted to the canonical unit
// of its type, then the value is exact to the precision of a float64.
func TestConvertValue_ListedUnit_ConvertsToTheCanonicalUnit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     float64
		unit      string
		canonical string
		want      float64
	}{
		{name: "count", value: 1240, unit: "count", canonical: "count", want: 1240},

		{name: "kg", value: 72.4, unit: "kg", canonical: "kg", want: 72.4},
		{name: "g", value: 500, unit: "g", canonical: "kg", want: 0.5},
		{name: "mg", value: 250, unit: "mg", canonical: "kg", want: 0.00025},
		{name: "mcg", value: 400, unit: "mcg", canonical: "kg", want: 0.0000004},
		{name: "lb", value: 1, unit: "lb", canonical: "kg", want: 0.45359237},
		{name: "oz", value: 16, unit: "oz", canonical: "kg", want: 0.45359237},
		{name: "st", value: 1, unit: "st", canonical: "kg", want: 6.35029318},

		{name: "m", value: 42, unit: "m", canonical: "m", want: 42},
		{name: "km", value: 5.21, unit: "km", canonical: "m", want: 5210},
		{name: "cm", value: 180, unit: "cm", canonical: "m", want: 1.8},
		{name: "mm", value: 12, unit: "mm", canonical: "m", want: 0.012},
		{name: "in", value: 1, unit: "in", canonical: "m", want: 0.0254},
		{name: "ft", value: 1, unit: "ft", canonical: "m", want: 0.3048},
		{name: "yd", value: 1, unit: "yd", canonical: "m", want: 0.9144},
		{name: "mi", value: 1, unit: "mi", canonical: "m", want: 1609.344},

		{name: "L", value: 1.5, unit: "L", canonical: "L", want: 1.5},
		{name: "mL", value: 250, unit: "mL", canonical: "L", want: 0.25},
		{name: "fl_oz_us", value: 1, unit: "fl_oz_us", canonical: "L", want: 0.0295735295625},
		{name: "fl_oz_imp", value: 1, unit: "fl_oz_imp", canonical: "L", want: 0.0284130625},
		{name: "cup_us", value: 1, unit: "cup_us", canonical: "L", want: 0.2365882365},
		{name: "cup_imp", value: 1, unit: "cup_imp", canonical: "L", want: 0.284130625},
		{name: "pt_us", value: 1, unit: "pt_us", canonical: "L", want: 0.473176473},
		{name: "pt_imp", value: 1, unit: "pt_imp", canonical: "L", want: 0.56826125},

		{name: "kcal", value: 362, unit: "kcal", canonical: "kcal", want: 362},
		{name: "Cal", value: 100, unit: "Cal", canonical: "kcal", want: 100},
		{name: "cal", value: 1000, unit: "cal", canonical: "kcal", want: 1},
		{name: "kJ", value: 418.4, unit: "kJ", canonical: "kcal", want: 100},
		{name: "J", value: 4184, unit: "J", canonical: "kcal", want: 1},

		{name: "duration s", value: 1800, unit: "s", canonical: "s", want: 1800},
		{name: "duration ms", value: 250, unit: "ms", canonical: "s", want: 0.25},
		{name: "duration min", value: 30, unit: "min", canonical: "s", want: 1800},
		{name: "duration hr", value: 1.5, unit: "hr", canonical: "s", want: 5400},
		{name: "duration d", value: 1, unit: "d", canonical: "s", want: 86400},

		{name: "short duration s", value: 0.045, unit: "s", canonical: "ms", want: 45},
		{name: "short duration ms", value: 45, unit: "ms", canonical: "ms", want: 45},
		{name: "short duration min", value: 1, unit: "min", canonical: "ms", want: 60000},
		{name: "short duration hr", value: 1, unit: "hr", canonical: "ms", want: 3600000},
		{name: "short duration d", value: 1, unit: "d", canonical: "ms", want: 86400000},

		{name: "count/min", value: 62, unit: "count/min", canonical: "count/min", want: 62},
		{name: "count/s", value: 1.5, unit: "count/s", canonical: "count/min", want: 90},

		{name: "m/s", value: 2.9, unit: "m/s", canonical: "m/s", want: 2.9},
		{name: "km/hr", value: 36, unit: "km/hr", canonical: "m/s", want: 10},
		{name: "mi/hr", value: 1, unit: "mi/hr", canonical: "m/s", want: 0.44704},

		{name: "W", value: 250, unit: "W", canonical: "W", want: 250},
		{name: "kW", value: 1.2, unit: "kW", canonical: "W", want: 1200},

		{name: "degC", value: 36.6, unit: "degC", canonical: "degC", want: 36.6},
		{name: "degF", value: 98.6, unit: "degF", canonical: "degC", want: 37},
		{name: "K", value: 310.15, unit: "K", canonical: "degC", want: 37},

		{name: "% as a fraction", value: 0.97, unit: "%", canonical: "fraction", want: 0.97},

		{name: "mg/dL", value: 99, unit: "mg/dL", canonical: "mg/dL", want: 99},
		{name: "mmol/L of glucose", value: 5.5, unit: "mmol<180.15588000005408>/L", canonical: "mg/dL", want: 99.085734000029744},

		{name: "mmHg", value: 120, unit: "mmHg", canonical: "mmHg", want: 120},
		{name: "kPa", value: 15.9986864898, unit: "kPa", canonical: "mmHg", want: 120},

		{name: "VO2max", value: 45.2, unit: "mL/min·kg", canonical: "mL/(kg.min)", want: 45.2},
		{name: "physical effort", value: 9.8, unit: "kcal/hr·kg", canonical: "kcal/(kg.h)", want: 9.8},
		{name: "effort score", value: 7, unit: "appleEffortScore", canonical: "appleEffortScore", want: 7},
		{name: "sound level", value: 72, unit: "dBASPL", canonical: "dBASPL", want: 72},
		{name: "air flow", value: 420, unit: "L/min", canonical: "L/min", want: 420},

		{name: "S", value: 0.000002, unit: "S", canonical: "S", want: 0.000002},
		{name: "mS", value: 3, unit: "mS", canonical: "S", want: 0.003},
		{name: "mcS", value: 2.5, unit: "mcS", canonical: "S", want: 0.0000025},

		{name: "IU", value: 4, unit: "IU", canonical: "IU", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ingest.ConvertValue(tt.value, tt.unit, curated(tt.canonical))
			require.NoError(t, err)
			require.InEpsilon(t, tt.want, got, 1e-15)
		})
	}
}

// unitStrings gives, for each canonical unit of the Units table of docs/architecture.md, a unit
// string of its row.
var unitStrings = map[string]string{
	"count": "count", "kg": "lb", "m": "km", "L": "mL", "kcal": "kJ", "s": "min", "ms": "s",
	"count/min": "count/s", "m/s": "km/hr", "W": "kW", "degC": "degF", "fraction": "%",
	"mg/dL": "mmol<180.15588000005408>/L", "mmHg": "kPa", "mL/(kg.min)": "mL/min·kg",
	"kcal/(kg.h)": "kcal/hr·kg", "appleEffortScore": "appleEffortScore", "dBASPL": "dBASPL",
	"L/min": "L/min", "S": "mS", "IU": "IU",
}

// Given the shipped catalog, when a value of each of its types is converted from a unit string of
// its row, then it converts: every canonical unit of catalog.yaml is in the units table.
func TestConvertValue_ShippedCatalog_EveryCanonicalUnitIsInTheTable(t *testing.T) {
	t.Parallel()

	c, err := catalog.Shipped()
	require.NoError(t, err)

	for _, typ := range c.Types() {
		unit, ok := unitStrings[typ.Unit]
		require.True(t, ok, "%s: unit %q is not in the Units table", typ.Key, typ.Unit)

		_, err := ingest.ConvertValue(1, unit, curated(typ.Unit))
		require.NoError(t, err, typ.Key)
	}
}

// Given a value of a curated type and a unit of its canonical unit, when it is converted, then the
// value stays as received, without rounding.
func TestConvertValue_CanonicalUnit_KeepsTheValue(t *testing.T) {
	t.Parallel()

	got, err := ingest.ConvertValue(0.1, "kg", curated("kg"))
	require.NoError(t, err)
	require.Equal(t, 0.1, got) //nolint:testifylint // the value is exact, not close
}

// Given a unit that the table does not list, or of another dimension than the canonical unit, when
// it is converted, then the record is invalid, with a reason naming the unit.
func TestConvertValue_UnlistedOrOtherDimension_IsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		unit string
		typ  store.MetricType
		want string
	}{
		{name: "unlisted unit", unit: "bpm", typ: curated("count/min"), want: `unit "bpm" is not a unit of count/min`},
		{name: "unit in another case", unit: "KG", typ: curated("kg"), want: `unit "KG" is not a unit of kg`},
		{name: "canonical unit that is not a unit string", unit: "fraction", typ: curated("fraction"), want: `unit "fraction" is not a unit of fraction`},
		{name: "energy for a mass", unit: "kcal", typ: curated("kg"), want: `unit "kcal" is not a unit of kg`},
		{name: "count for a rate", unit: "count", typ: curated("count/min"), want: `unit "count" is not a unit of count/min`},
		{name: "time for a rate", unit: "s", typ: curated("count/min"), want: `unit "s" is not a unit of count/min`},
		{name: "empty unit", unit: "", typ: curated("kg"), want: `unit "" is not a unit of kg`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ingest.ConvertValue(1, tt.unit, tt.typ)
			require.EqualError(t, err, tt.want)
		})
	}
}

// Given an uncurated type, when a record is converted, then its first unit is kept: a record of
// that unit is stored as received, and a record of another unit converts to it when both units
// are in the table and share a dimension, and is invalid otherwise.
func TestConvertValue_UncuratedType_KeepsItsFirstUnit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   float64
		unit    string
		typ     store.MetricType
		want    float64
		wantErr string
	}{
		{name: "same listed unit", value: 418.4, unit: "kJ", typ: uncurated("kJ"), want: 418.4},
		{name: "same unit that is not canonical", value: 0.97, unit: "%", typ: uncurated("%"), want: 0.97},
		{name: "same unlisted unit", value: 3, unit: "furlong", typ: uncurated("furlong"), want: 3},
		{name: "other unit of the same dimension", value: 100, unit: "kcal", typ: uncurated("kJ"), want: 418.4},
		{name: "other unit of a temperature", value: 37, unit: "degC", typ: uncurated("degF"), want: 98.6},
		{name: "other dimension", value: 1, unit: "count", typ: uncurated("kJ"), wantErr: `unit "count" is not a unit of kJ`},
		{name: "unlisted unit", value: 1, unit: "furlong", typ: uncurated("kJ"), wantErr: `unit "furlong" is not a unit of kJ`},
		{name: "listed unit to an unlisted one", value: 1, unit: "m", typ: uncurated("furlong"), wantErr: `unit "m" is not a unit of furlong`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ingest.ConvertValue(tt.value, tt.unit, tt.typ)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.InEpsilon(t, tt.want, got, 1e-15)
		})
	}
}
