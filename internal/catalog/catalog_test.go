package catalog_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
)

// small is a valid catalog with one type of each kind and one unsupported identifier of each form.
const small = `
types:
  - key: heart_rate
    identifier: HKQuantityTypeIdentifierHeartRate
    kind: quantity
    unit: count/min
    aggregation: discrete
    group: heart
  - key: stand_hour
    identifier: HKCategoryTypeIdentifierAppleStandHour
    kind: category
    unit: count
    aggregation: discrete
    group: activity
    values:
      - {key: stood, identifier: HKCategoryValueAppleStandHourStood}
      - {key: idle, identifier: HKCategoryValueAppleStandHourIdle}
unsupported:
  identifiers:
    - HKDataTypeIdentifierAudiogram
  prefixes:
    - HKClinicalTypeIdentifier
`

// parseSmall parses the small catalog.
func parseSmall(t *testing.T) *catalog.Catalog {
	t.Helper()

	c, err := catalog.Parse([]byte(small))
	require.NoError(t, err)

	return c
}

// Given a known identifier, when Lookup runs, then it returns its type.
func TestLookup_KnownIdentifier_ReturnsItsType(t *testing.T) {
	t.Parallel()

	c := parseSmall(t)

	tests := []struct {
		identifier string
		want       catalog.Type
	}{
		{
			identifier: "HKQuantityTypeIdentifierHeartRate",
			want: catalog.Type{
				Key:         "heart_rate",
				Identifier:  "HKQuantityTypeIdentifierHeartRate",
				Kind:        catalog.KindQuantity,
				Unit:        "count/min",
				Aggregation: catalog.AggregationDiscrete,
				Group:       catalog.GroupHeart,
			},
		},
		{
			identifier: "HKCategoryTypeIdentifierAppleStandHour",
			want: catalog.Type{
				Key:         "stand_hour",
				Identifier:  "HKCategoryTypeIdentifierAppleStandHour",
				Kind:        catalog.KindCategory,
				Unit:        "count",
				Aggregation: catalog.AggregationDiscrete,
				Group:       catalog.GroupActivity,
				Values: []catalog.Value{
					{Key: "stood", Identifier: "HKCategoryValueAppleStandHourStood"},
					{Key: "idle", Identifier: "HKCategoryValueAppleStandHourIdle"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			t.Parallel()

			typ, status := c.Lookup(tt.identifier)

			require.Equal(t, catalog.Known, status)
			require.Equal(t, tt.want, typ)
		})
	}
}

// Given an unsupported identifier, listed or starting with an unsupported prefix, when Lookup runs,
// then it reports it as unsupported.
func TestLookup_UnsupportedIdentifier_ReportsUnsupported(t *testing.T) {
	t.Parallel()

	c := parseSmall(t)

	for _, identifier := range []string{
		"HKDataTypeIdentifierAudiogram",
		"HKClinicalTypeIdentifierLabResultRecord",
	} {
		t.Run(identifier, func(t *testing.T) {
			t.Parallel()

			typ, status := c.Lookup(identifier)

			require.Equal(t, catalog.Unsupported, status)
			require.Zero(t, typ)
		})
	}
}

// Given any other identifier, when Lookup runs, then it reports it as unknown.
func TestLookup_OtherIdentifier_ReportsUnknown(t *testing.T) {
	t.Parallel()

	c := parseSmall(t)

	for _, identifier := range []string{
		"HKQuantityTypeIdentifierStepCount",
		"HKDataTypeSleepDurationGoal",
		"heart_rate",
		"",
	} {
		t.Run(identifier, func(t *testing.T) {
			t.Parallel()

			typ, status := c.Lookup(identifier)

			require.Equal(t, catalog.Unknown, status)
			require.Zero(t, typ)
		})
	}
}

// Given a type returned by Lookup, when the caller changes its values, then the catalog keeps its own.
func TestLookup_ChangedResult_LeavesTheCatalogUnchanged(t *testing.T) {
	t.Parallel()

	c := parseSmall(t)

	typ, _ := c.Lookup("HKCategoryTypeIdentifierAppleStandHour")
	typ.Values[0].Key = "changed"

	again, _ := c.Lookup("HKCategoryTypeIdentifierAppleStandHour")
	require.Equal(t, "stood", again.Values[0].Key)
}

// quantity is a valid quantity type whose fields a test replaces.
const quantity = `{key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart}`

// Given a catalog file that breaks a rule, when it is parsed, then it fails with an error that names
// the entry.
func TestParse_BrokenRule_FailsNamingTheEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "duplicate key",
			data: "types:\n  - " + quantity + "\n" +
				"  - {key: heart_rate, identifier: HKQuantityTypeIdentifierRestingHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart}\n",
			want: `type "heart_rate" (HKQuantityTypeIdentifierRestingHeartRate): key is already used`,
		},
		{
			name: "duplicate identifier",
			data: "types:\n  - " + quantity + "\n" +
				"  - {key: pulse, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart}\n",
			want: `type "pulse" (HKQuantityTypeIdentifierHeartRate): identifier is already used`,
		},
		{
			name: "key not in snake case",
			data: "types:\n  - {key: heartRate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart}\n",
			want: `type "heartRate" (HKQuantityTypeIdentifierHeartRate): key is not in snake case`,
		},
		{
			name: "missing key",
			data: "types:\n  - {identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart}\n",
			want: `type "" (HKQuantityTypeIdentifierHeartRate): key is not in snake case`,
		},
		{
			name: "missing identifier",
			data: "types:\n  - {key: heart_rate, kind: quantity, unit: count/min, aggregation: discrete, group: heart}\n",
			want: `type "heart_rate" (): identifier is missing`,
		},
		{
			name: "unknown kind",
			data: "types:\n  - {key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: correlation, unit: count/min, aggregation: discrete, group: heart}\n",
			want: `type "heart_rate" (HKQuantityTypeIdentifierHeartRate): kind "correlation" is not quantity or category`,
		},
		{
			name: "unit that is not canonical",
			data: "types:\n  - {key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: bpm, aggregation: discrete, group: heart}\n",
			want: `type "heart_rate" (HKQuantityTypeIdentifierHeartRate): unit "bpm" is not a canonical unit`,
		},
		{
			name: "unknown aggregation",
			data: "types:\n  - {key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: average, group: heart}\n",
			want: `type "heart_rate" (HKQuantityTypeIdentifierHeartRate): aggregation "average" is not cumulative or discrete`,
		},
		{
			name: "group not in FR-1",
			data: "types:\n  - {key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: cardio}\n",
			want: `type "heart_rate" (HKQuantityTypeIdentifierHeartRate): group "cardio" is not a display group`,
		},
		{
			name: "category without values",
			data: "types:\n  - {key: stand_hour, identifier: HKCategoryTypeIdentifierAppleStandHour, kind: category, unit: count, aggregation: discrete, group: activity}\n",
			want: `type "stand_hour" (HKCategoryTypeIdentifierAppleStandHour): a category has at least one value`,
		},
		{
			name: "quantity with values",
			data: "types:\n  - {key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart, values: [{key: high, identifier: HKCategoryValueHigh}]}\n",
			want: `type "heart_rate" (HKQuantityTypeIdentifierHeartRate): a quantity has no values`,
		},
		{
			name: "duplicate value key",
			data: "types:\n  - {key: stand_hour, identifier: HKCategoryTypeIdentifierAppleStandHour, kind: category, unit: count, aggregation: discrete, group: activity, values: [" +
				"{key: stood, identifier: HKCategoryValueAppleStandHourStood}, {key: stood, identifier: HKCategoryValueAppleStandHourIdle}]}\n",
			want: `type "stand_hour" (HKCategoryTypeIdentifierAppleStandHour): value "stood" (HKCategoryValueAppleStandHourIdle): key is already used`,
		},
		{
			name: "duplicate value identifier",
			data: "types:\n  - {key: stand_hour, identifier: HKCategoryTypeIdentifierAppleStandHour, kind: category, unit: count, aggregation: discrete, group: activity, values: [" +
				"{key: stood, identifier: HKCategoryValueAppleStandHourStood}, {key: idle, identifier: HKCategoryValueAppleStandHourStood}]}\n",
			want: `type "stand_hour" (HKCategoryTypeIdentifierAppleStandHour): value "idle" (HKCategoryValueAppleStandHourStood): identifier is already used`,
		},
		{
			name: "value key not in snake case",
			data: "types:\n  - {key: stand_hour, identifier: HKCategoryTypeIdentifierAppleStandHour, kind: category, unit: count, aggregation: discrete, group: activity, values: [" +
				"{key: Stood, identifier: HKCategoryValueAppleStandHourStood}]}\n",
			want: `type "stand_hour" (HKCategoryTypeIdentifierAppleStandHour): value "Stood" (HKCategoryValueAppleStandHourStood): key is not in snake case`,
		},
		{
			name: "value without identifier",
			data: "types:\n  - {key: stand_hour, identifier: HKCategoryTypeIdentifierAppleStandHour, kind: category, unit: count, aggregation: discrete, group: activity, values: [" +
				"{key: stood}]}\n",
			want: `type "stand_hour" (HKCategoryTypeIdentifierAppleStandHour): value "stood" (): identifier is missing`,
		},
		{
			name: "unsupported identifier of a type",
			data: "types:\n  - " + quantity + "\nunsupported:\n  identifiers: [HKQuantityTypeIdentifierHeartRate]\n",
			want: `unsupported identifier "HKQuantityTypeIdentifierHeartRate": a type has this identifier`,
		},
		{
			name: "duplicate unsupported identifier",
			data: "unsupported:\n  identifiers: [HKDataTypeIdentifierAudiogram, HKDataTypeIdentifierAudiogram]\n",
			want: `unsupported identifier "HKDataTypeIdentifierAudiogram": identifier is already used`,
		},
		{
			name: "unsupported prefix of a type",
			data: "types:\n  - " + quantity + "\nunsupported:\n  prefixes: [HKQuantityTypeIdentifier]\n",
			want: `unsupported prefix "HKQuantityTypeIdentifier": type "heart_rate" (HKQuantityTypeIdentifierHeartRate) starts with it`,
		},
		{
			name: "empty unsupported prefix",
			data: "unsupported:\n  prefixes: [\"\"]\n",
			want: `unsupported prefix "": prefix is missing`,
		},
		{
			name: "unknown field",
			data: "types:\n  - {key: heart_rate, identifier: HKQuantityTypeIdentifierHeartRate, kind: quantity, unit: count/min, aggregation: discrete, group: heart, name: Heart rate}\n",
			want: "field name not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.Parse([]byte(tt.data))

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func shipped(t *testing.T) *catalog.Catalog {
	t.Helper()

	c, err := catalog.Shipped()
	require.NoError(t, err)

	return c
}

// Given the shipped catalog, then it lists quantity and category types with every attribute, such as
// the examples of FR-1.
func TestShipped_ListsTheHealthKitTypesWithTheirAttributes(t *testing.T) {
	t.Parallel()

	c := shipped(t)

	tests := []struct {
		identifier  string
		key         string
		kind        catalog.Kind
		unit        string
		aggregation catalog.Aggregation
		group       catalog.Group
	}{
		{"HKQuantityTypeIdentifierHeartRate", "heart_rate", catalog.KindQuantity, "count/min", catalog.AggregationDiscrete, catalog.GroupHeart},
		{"HKQuantityTypeIdentifierStepCount", "step_count", catalog.KindQuantity, "count", catalog.AggregationCumulative, catalog.GroupActivity},
		{"HKQuantityTypeIdentifierActiveEnergyBurned", "active_energy_burned", catalog.KindQuantity, "kcal", catalog.AggregationCumulative, catalog.GroupActivity},
		{"HKQuantityTypeIdentifierBodyMass", "body_mass", catalog.KindQuantity, "kg", catalog.AggregationDiscrete, catalog.GroupBody},
		{"HKQuantityTypeIdentifierOxygenSaturation", "oxygen_saturation", catalog.KindQuantity, "fraction", catalog.AggregationDiscrete, catalog.GroupRespiratory},
		{"HKQuantityTypeIdentifierDietaryWater", "dietary_water", catalog.KindQuantity, "L", catalog.AggregationCumulative, catalog.GroupNutrition},
		{"HKCategoryTypeIdentifierSleepAnalysis", "sleep_analysis", catalog.KindCategory, "count", catalog.AggregationDiscrete, catalog.GroupSleep},
		{"HKCategoryTypeIdentifierHeadache", "headache", catalog.KindCategory, "count", catalog.AggregationDiscrete, catalog.GroupSymptoms},
	}

	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			t.Parallel()

			typ, status := c.Lookup(tt.identifier)

			require.Equal(t, catalog.Known, status)
			require.Equal(t, tt.key, typ.Key)
			require.Equal(t, tt.identifier, typ.Identifier)
			require.Equal(t, tt.kind, typ.Kind)
			require.Equal(t, tt.unit, typ.Unit)
			require.Equal(t, tt.aggregation, typ.Aggregation)
			require.Equal(t, tt.group, typ.Group)
		})
	}
}

// Given the shipped catalog, then a category lists its values with their key and HealthKit
// identifier, such as asleep_core for HKCategoryValueSleepAnalysisAsleepCore.
func TestShipped_CategoryListsItsValues(t *testing.T) {
	t.Parallel()

	typ, status := shipped(t).Lookup("HKCategoryTypeIdentifierSleepAnalysis")

	require.Equal(t, catalog.Known, status)
	require.ElementsMatch(t, []catalog.Value{
		{Key: "in_bed", Identifier: "HKCategoryValueSleepAnalysisInBed"},
		{Key: "awake", Identifier: "HKCategoryValueSleepAnalysisAwake"},
		{Key: "asleep_core", Identifier: "HKCategoryValueSleepAnalysisAsleepCore"},
		{Key: "asleep_deep", Identifier: "HKCategoryValueSleepAnalysisAsleepDeep"},
		{Key: "asleep_rem", Identifier: "HKCategoryValueSleepAnalysisAsleepREM"},
		{Key: "asleep_unspecified", Identifier: "HKCategoryValueSleepAnalysisAsleepUnspecified"},
	}, typ.Values)
}

// Given the shipped catalog, then the identifiers of FR-1 that Verve does not store yet are
// unsupported.
func TestShipped_UnsupportedIdentifiersOfFR1(t *testing.T) {
	t.Parallel()

	c := shipped(t)

	for _, identifier := range []string{
		"HKDataTypeIdentifierElectrocardiogram",
		"HKDataTypeIdentifierAudiogram",
		"HKDataTypeIdentifierStateOfMind",
		"HKMedicationDoseEventTypeIdentifierMedicationDoseEvent",
		"HKClinicalTypeIdentifierAllergyRecord",
		"HKClinicalTypeIdentifierLabResultRecord",
	} {
		t.Run(identifier, func(t *testing.T) {
			t.Parallel()

			_, status := c.Lookup(identifier)

			require.Equal(t, catalog.Unsupported, status)
		})
	}
}

// architectureUnits are the canonical units of the Units table of docs/architecture.md.
var architectureUnits = []string{
	"count", "kg", "m", "L", "kcal", "s", "ms", "count/min", "m/s", "W", "degC", "fraction", "mg/dL",
	"mmHg", "mL/(kg.min)", "kcal/(kg.h)", "appleEffortScore", "dBASPL", "L/min", "S", "IU",
}

// fr1Groups are the display groups of FR-1.
var fr1Groups = []catalog.Group{
	"activity", "heart", "body", "sleep", "nutrition", "respiratory", "vitals", "mobility", "hearing",
	"cycle", "symptoms", "other",
}

// Given the entries of the shipped catalog, then keys and identifiers are unique, keys are snake
// case, every unit is a canonical unit of the architecture, every group is one of FR-1, a category
// has at least one value and a quantity none.
func TestShipped_EntriesFollowTheRules(t *testing.T) {
	t.Parallel()

	types := shipped(t).Types()
	require.NotEmpty(t, types)

	snakeCase := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	keys := map[string]bool{}
	identifiers := map[string]bool{}
	kinds := map[catalog.Kind]int{}

	for _, typ := range types {
		require.Regexp(t, snakeCase, typ.Key)
		require.False(t, keys[typ.Key], "key %s is used twice", typ.Key)
		require.False(t, identifiers[typ.Identifier], "identifier %s is used twice", typ.Identifier)
		require.Contains(t, architectureUnits, typ.Unit, typ.Key)
		require.Contains(t, fr1Groups, typ.Group, typ.Key)
		require.Contains(t, []catalog.Aggregation{catalog.AggregationCumulative, catalog.AggregationDiscrete}, typ.Aggregation, typ.Key)

		keys[typ.Key], identifiers[typ.Identifier] = true, true
		kinds[typ.Kind]++

		switch typ.Kind {
		case catalog.KindQuantity:
			require.True(t, strings.HasPrefix(typ.Identifier, "HKQuantityTypeIdentifier"), typ.Key)
			require.Empty(t, typ.Values, typ.Key)
		case catalog.KindCategory:
			require.True(t, strings.HasPrefix(typ.Identifier, "HKCategoryTypeIdentifier"), typ.Key)
			require.NotEmpty(t, typ.Values, typ.Key)
		default:
			require.Failf(t, "unknown kind", "type %s has the kind %q", typ.Key, typ.Kind)
		}

		valueKeys := map[string]bool{}
		valueIdentifiers := map[string]bool{}

		for _, v := range typ.Values {
			require.Regexp(t, snakeCase, v.Key, typ.Key)
			require.True(t, strings.HasPrefix(v.Identifier, "HKCategoryValue"), "%s: %s", typ.Key, v.Identifier)
			require.False(t, valueKeys[v.Key], "%s: value key %s is used twice", typ.Key, v.Key)
			require.False(t, valueIdentifiers[v.Identifier], "%s: value identifier %s is used twice", typ.Key, v.Identifier)

			valueKeys[v.Key], valueIdentifiers[v.Identifier] = true, true
		}
	}

	require.Positive(t, kinds[catalog.KindQuantity])
	require.Positive(t, kinds[catalog.KindCategory])
}
