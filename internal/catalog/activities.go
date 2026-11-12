package catalog

import (
	"bytes"
	_ "embed" // for the shipped activity types
	"errors"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
)

// Family is the activity family of an activity type, which deduplication compares.
type Family string

// The activity families of FR-4 of docs/features/data-storage-and-aggregates.md.
const (
	FamilyRunning     Family = "running"
	FamilyWalking     Family = "walking"
	FamilyHiking      Family = "hiking"
	FamilyCycling     Family = "cycling"
	FamilySwimming    Family = "swimming"
	FamilyRowing      Family = "rowing"
	FamilyStrength    Family = "strength"
	FamilyCardio      Family = "cardio"
	FamilyDance       Family = "dance"
	FamilyMindAndBody Family = "mind_and_body"
	FamilyTeamSport   Family = "team_sport"
	FamilyRacketSport Family = "racket_sport"
	FamilyWinterSport Family = "winter_sport"
	FamilyWaterSport  Family = "water_sport"
	FamilyCombat      Family = "combat"
	FamilyOther       Family = "other"
)

// Known reports whether f is one of the families of FR-4.
func (f Family) Known() bool {
	switch f {
	case FamilyRunning, FamilyWalking, FamilyHiking, FamilyCycling, FamilySwimming, FamilyRowing,
		FamilyStrength, FamilyCardio, FamilyDance, FamilyMindAndBody, FamilyTeamSport, FamilyRacketSport,
		FamilyWinterSport, FamilyWaterSport, FamilyCombat, FamilyOther:
		return true
	}

	return false
}

// Activity is a workout activity type, a value of HKWorkoutActivityType.
type Activity struct {
	// Key names the activity type in the API and in the translations, such as running.
	Key string `yaml:"key"`
	// Identifier is its name in the XML export, such as HKWorkoutActivityTypeRunning.
	Identifier string `yaml:"identifier"`
	// RawValue is its raw value in HealthKit, such as 37.
	RawValue uint   `yaml:"raw_value"`
	Family   Family `yaml:"family"`
}

// Activities is the parsed activity type catalog. It never changes once parsed, so that it can be
// shared.
type Activities struct {
	activities   []Activity
	byKey        map[string]int // index in activities
	byIdentifier map[string]int
	byRawValue   map[uint]int
}

// activitiesFile is the layout of activities.yaml.
type activitiesFile struct {
	Activities []Activity `yaml:"activities"`
}

// shippedActivities is the activity type catalog of the binary.
//
//go:embed activities.yaml
var shippedActivities string

// ShippedActivities parses the activity type catalog of the binary. Call it once at startup and
// share the result.
func ShippedActivities() (*Activities, error) {
	return ParseActivities([]byte(shippedActivities))
}

// ParseActivities reads an activity type catalog in the layout of activities.yaml.
func ParseActivities(data []byte) (*Activities, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var f activitiesFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse activities: %w", err)
	}

	a := &Activities{
		activities:   f.Activities,
		byKey:        make(map[string]int, len(f.Activities)),
		byIdentifier: make(map[string]int, len(f.Activities)),
		byRawValue:   make(map[uint]int, len(f.Activities)),
	}

	for i, activity := range f.Activities {
		if err := a.add(i, activity); err != nil {
			return nil, fmt.Errorf("parse activities: activity %q (%s): %w", activity.Key, activity.Identifier, err)
		}
	}

	return a, nil
}

// add indexes the activity type at index i, once it checked the rules of the catalog.
func (a *Activities) add(i int, activity Activity) error {
	switch {
	case !snakeCase.MatchString(activity.Key):
		return errors.New("key is not in snake case")
	case activity.Identifier == "":
		return errors.New("identifier is missing")
	case activity.RawValue == 0:
		return errors.New("raw value is missing")
	case !activity.Family.Known():
		return fmt.Errorf("family %q is not a family of FR-4", activity.Family)
	}

	if _, ok := a.byKey[activity.Key]; ok {
		return errors.New("key is already used")
	}

	if _, ok := a.byIdentifier[activity.Identifier]; ok {
		return errors.New("identifier is already used")
	}

	if _, ok := a.byRawValue[activity.RawValue]; ok {
		return fmt.Errorf("raw value %d is already used", activity.RawValue)
	}

	a.byKey[activity.Key], a.byIdentifier[activity.Identifier], a.byRawValue[activity.RawValue] = i, i, i

	return nil
}

// ByKey returns the activity type of a key, or false when the catalog does not list it.
func (a *Activities) ByKey(key string) (Activity, bool) {
	return lookup(a, a.byKey, key)
}

// ByIdentifier returns the activity type of a name in the XML export, such as
// HKWorkoutActivityTypeRunning, or false when the catalog does not list it.
func (a *Activities) ByIdentifier(identifier string) (Activity, bool) {
	return lookup(a, a.byIdentifier, identifier)
}

// ByRawValue returns the activity type of a raw value of HealthKit, or false when the catalog does
// not list it.
func (a *Activities) ByRawValue(rawValue uint) (Activity, bool) {
	return lookup(a, a.byRawValue, rawValue)
}

// lookup returns the activity type that index holds under k, or false when it holds none.
func lookup[K comparable](a *Activities, index map[K]int, k K) (Activity, bool) {
	i, ok := index[k]
	if !ok {
		return Activity{}, false
	}

	return a.activities[i], true
}

// All returns every activity type, in the order of the catalog file.
func (a *Activities) All() []Activity {
	return slices.Clone(a.activities)
}
