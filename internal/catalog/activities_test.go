package catalog_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
)

// smallActivities is a valid activity catalog with two activity types.
const smallActivities = `
activities:
  - {key: running, identifier: HKWorkoutActivityTypeRunning, raw_value: 37, family: running}
  - {key: yoga, identifier: HKWorkoutActivityTypeYoga, raw_value: 57, family: mind_and_body}
`

// parseSmallActivities parses the small activity catalog.
func parseSmallActivities(t *testing.T) *catalog.Activities {
	t.Helper()

	a, err := catalog.ParseActivities([]byte(smallActivities))
	require.NoError(t, err)

	return a
}

// running is the running activity type of the small activity catalog.
var running = catalog.Activity{
	Key:        "running",
	Identifier: "HKWorkoutActivityTypeRunning",
	RawValue:   37,
	Family:     catalog.FamilyRunning,
}

// Given a key, an export name or a raw value, when the lookup runs, then it returns the activity type.
func TestActivities_KnownKeyNameOrRawValue_ReturnsTheActivityType(t *testing.T) {
	t.Parallel()

	a := parseSmallActivities(t)

	tests := []struct {
		name   string
		lookup func() (catalog.Activity, bool)
	}{
		{name: "key", lookup: func() (catalog.Activity, bool) { return a.ByKey("running") }},
		{name: "export name", lookup: func() (catalog.Activity, bool) { return a.ByIdentifier("HKWorkoutActivityTypeRunning") }},
		{name: "raw value", lookup: func() (catalog.Activity, bool) { return a.ByRawValue(37) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			activity, ok := tt.lookup()

			require.True(t, ok)
			require.Equal(t, running, activity)
		})
	}
}

// Given an unknown key, export name or raw value, when the lookup runs, then it reports it as unknown.
func TestActivities_UnknownKeyNameOrRawValue_ReportsUnknown(t *testing.T) {
	t.Parallel()

	a := parseSmallActivities(t)

	tests := []struct {
		name   string
		lookup func() (catalog.Activity, bool)
	}{
		{name: "unknown key", lookup: func() (catalog.Activity, bool) { return a.ByKey("cycling") }},
		{name: "export name as a key", lookup: func() (catalog.Activity, bool) { return a.ByKey("HKWorkoutActivityTypeRunning") }},
		{name: "empty key", lookup: func() (catalog.Activity, bool) { return a.ByKey("") }},
		{name: "unknown export name", lookup: func() (catalog.Activity, bool) { return a.ByIdentifier("HKWorkoutActivityTypeCycling") }},
		{name: "key as an export name", lookup: func() (catalog.Activity, bool) { return a.ByIdentifier("running") }},
		{name: "unknown raw value", lookup: func() (catalog.Activity, bool) { return a.ByRawValue(13) }},
		{name: "zero raw value", lookup: func() (catalog.Activity, bool) { return a.ByRawValue(0) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			activity, ok := tt.lookup()

			require.False(t, ok)
			require.Zero(t, activity)
		})
	}
}

// validActivity is a valid activity type entry that a test puts next to a broken one.
const validActivity = `{key: running, identifier: HKWorkoutActivityTypeRunning, raw_value: 37, family: running}`

// Given an activity catalog file that breaks a rule, when it is parsed, then it fails with an error
// that names the entry.
func TestParseActivities_BrokenRule_FailsNamingTheEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "duplicate key",
			data: "activities:\n  - " + validActivity + "\n" +
				"  - {key: running, identifier: HKWorkoutActivityTypeWalking, raw_value: 52, family: walking}\n",
			want: `activity "running" (HKWorkoutActivityTypeWalking): key is already used`,
		},
		{
			name: "duplicate export name",
			data: "activities:\n  - " + validActivity + "\n" +
				"  - {key: jogging, identifier: HKWorkoutActivityTypeRunning, raw_value: 52, family: running}\n",
			want: `activity "jogging" (HKWorkoutActivityTypeRunning): identifier is already used`,
		},
		{
			name: "duplicate raw value",
			data: "activities:\n  - " + validActivity + "\n" +
				"  - {key: walking, identifier: HKWorkoutActivityTypeWalking, raw_value: 37, family: walking}\n",
			want: `activity "walking" (HKWorkoutActivityTypeWalking): raw value 37 is already used`,
		},
		{
			name: "key not in snake case",
			data: "activities:\n  - {key: Running, identifier: HKWorkoutActivityTypeRunning, raw_value: 37, family: running}\n",
			want: `activity "Running" (HKWorkoutActivityTypeRunning): key is not in snake case`,
		},
		{
			name: "missing key",
			data: "activities:\n  - {identifier: HKWorkoutActivityTypeRunning, raw_value: 37, family: running}\n",
			want: `activity "" (HKWorkoutActivityTypeRunning): key is not in snake case`,
		},
		{
			name: "missing export name",
			data: "activities:\n  - {key: running, raw_value: 37, family: running}\n",
			want: `activity "running" (): identifier is missing`,
		},
		{
			name: "missing raw value",
			data: "activities:\n  - {key: running, identifier: HKWorkoutActivityTypeRunning, family: running}\n",
			want: `activity "running" (HKWorkoutActivityTypeRunning): raw value is missing`,
		},
		{
			name: "negative raw value",
			data: "activities:\n  - {key: running, identifier: HKWorkoutActivityTypeRunning, raw_value: -1, family: running}\n",
			want: "line 2: cannot unmarshal !!int `-1` into uint",
		},
		{
			name: "family not in FR-4",
			data: "activities:\n  - {key: running, identifier: HKWorkoutActivityTypeRunning, raw_value: 37, family: jogging}\n",
			want: `activity "running" (HKWorkoutActivityTypeRunning): family "jogging" is not a family of FR-4`,
		},
		{
			name: "missing family",
			data: "activities:\n  - {key: running, identifier: HKWorkoutActivityTypeRunning, raw_value: 37}\n",
			want: `activity "running" (HKWorkoutActivityTypeRunning): family "" is not a family of FR-4`,
		},
		{
			name: "unknown field",
			data: "activities:\n  - {key: running, identifier: HKWorkoutActivityTypeRunning, raw_value: 37, family: running, name: Running}\n",
			want: "line 2: field name not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.ParseActivities([]byte(tt.data))

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func shippedActivities(t *testing.T) *catalog.Activities {
	t.Helper()

	a, err := catalog.ShippedActivities()
	require.NoError(t, err)

	return a
}

// fr4Families are the activity families of FR-4.
var fr4Families = []catalog.Family{
	"running", "walking", "hiking", "cycling", "swimming", "rowing", "strength", "cardio", "dance",
	"mind_and_body", "team_sport", "racket_sport", "winter_sport", "water_sport", "combat", "other",
}

// Given the shipped activity types, then they list the values of HKWorkoutActivityType, each with a
// key, its name in the XML export, its raw value and a family of FR-4.
func TestShippedActivities_ListTheValuesOfHKWorkoutActivityType(t *testing.T) {
	t.Parallel()

	a := shippedActivities(t)

	// The values of HKWorkoutActivityType in HKWorkout.h of the iOS 26.5 SDK: 1 to 80, 82 to 84 and
	// 3000.
	all := a.All()
	require.Len(t, all, 84)

	for _, activity := range all {
		require.True(t, strings.HasPrefix(activity.Identifier, "HKWorkoutActivityType"), activity.Key)
		require.Contains(t, fr4Families, activity.Family, activity.Key)

		ok := (activity.RawValue >= 1 && activity.RawValue <= 80) ||
			(activity.RawValue >= 82 && activity.RawValue <= 84) || activity.RawValue == 3000
		require.True(t, ok, "%s has the raw value %d", activity.Key, activity.RawValue)
	}

	for _, want := range []catalog.Activity{
		{Key: "american_football", Identifier: "HKWorkoutActivityTypeAmericanFootball", RawValue: 1, Family: catalog.FamilyTeamSport},
		{Key: "cycling", Identifier: "HKWorkoutActivityTypeCycling", RawValue: 13, Family: catalog.FamilyCycling},
		{Key: "dance", Identifier: "HKWorkoutActivityTypeDance", RawValue: 14, Family: catalog.FamilyDance},
		{Key: "hiking", Identifier: "HKWorkoutActivityTypeHiking", RawValue: 24, Family: catalog.FamilyHiking},
		{Key: "running", Identifier: "HKWorkoutActivityTypeRunning", RawValue: 37, Family: catalog.FamilyRunning},
		{Key: "swimming", Identifier: "HKWorkoutActivityTypeSwimming", RawValue: 46, Family: catalog.FamilySwimming},
		{Key: "traditional_strength_training", Identifier: "HKWorkoutActivityTypeTraditionalStrengthTraining", RawValue: 50, Family: catalog.FamilyStrength},
		{Key: "walking", Identifier: "HKWorkoutActivityTypeWalking", RawValue: 52, Family: catalog.FamilyWalking},
		{Key: "yoga", Identifier: "HKWorkoutActivityTypeYoga", RawValue: 57, Family: catalog.FamilyMindAndBody},
		{Key: "high_intensity_interval_training", Identifier: "HKWorkoutActivityTypeHighIntensityIntervalTraining", RawValue: 63, Family: catalog.FamilyCardio},
		{Key: "wheelchair_run_pace", Identifier: "HKWorkoutActivityTypeWheelchairRunPace", RawValue: 71, Family: catalog.FamilyRunning},
		{Key: "cooldown", Identifier: "HKWorkoutActivityTypeCooldown", RawValue: 80, Family: catalog.FamilyMindAndBody},
		{Key: "swim_bike_run", Identifier: "HKWorkoutActivityTypeSwimBikeRun", RawValue: 82, Family: catalog.FamilyOther},
		{Key: "underwater_diving", Identifier: "HKWorkoutActivityTypeUnderwaterDiving", RawValue: 84, Family: catalog.FamilyWaterSport},
		{Key: "other", Identifier: "HKWorkoutActivityTypeOther", RawValue: 3000, Family: catalog.FamilyOther},
	} {
		got, ok := a.ByIdentifier(want.Identifier)
		require.True(t, ok, want.Identifier)
		require.Equal(t, want, got)
	}
}

// Given the shipped activity types, then keys, names and raw values are unique, keys are snake case,
// and each key is its name in the XML export in snake case, without the prefix.
func TestShippedActivities_EntriesFollowTheRules(t *testing.T) {
	t.Parallel()

	all := shippedActivities(t).All()
	require.NotEmpty(t, all)

	snakeCase := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	keys := map[string]bool{}
	identifiers := map[string]bool{}
	rawValues := map[uint]bool{}

	for _, activity := range all {
		require.Regexp(t, snakeCase, activity.Key)
		require.False(t, keys[activity.Key], "key %s is used twice", activity.Key)
		require.False(t, identifiers[activity.Identifier], "identifier %s is used twice", activity.Identifier)
		require.False(t, rawValues[activity.RawValue], "raw value %d is used twice", activity.RawValue)
		require.Equal(t, "hk_workout_activity_type_"+activity.Key, catalog.UncuratedKey(activity.Identifier),
			"the key of %s", activity.Identifier)

		keys[activity.Key], identifiers[activity.Identifier], rawValues[activity.RawValue] = true, true, true
	}
}

// Given the activity types returned by All, when the caller changes them, then the catalog keeps its
// own.
func TestActivities_ChangedList_LeavesTheCatalogUnchanged(t *testing.T) {
	t.Parallel()

	a := parseSmallActivities(t)

	a.All()[0].Key = "changed"

	require.Equal(t, "running", a.All()[0].Key)
}
