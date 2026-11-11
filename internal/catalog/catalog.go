// Package catalog describes the HealthKit types that Verve knows: the metric type catalog of
// docs/features/data-storage-and-aggregates.md FR-1. It ships with the binary in catalog.yaml.
package catalog

import (
	"bytes"
	_ "embed" // for the shipped catalog
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Kind is the kind of samples a type holds.
type Kind string

// The kinds of FR-1.
const (
	KindQuantity Kind = "quantity"
	KindCategory Kind = "category"
)

// Aggregation is the aggregation style of a type, the one of HealthKit.
type Aggregation string

// The aggregation styles of FR-1.
const (
	// AggregationCumulative sums values, such as steps or energy.
	AggregationCumulative Aggregation = "cumulative"
	// AggregationDiscrete averages values, such as heart rate or weight.
	AggregationDiscrete Aggregation = "discrete"
)

// Group is the display group of a type.
type Group string

// The display groups of FR-1.
const (
	GroupActivity    Group = "activity"
	GroupHeart       Group = "heart"
	GroupBody        Group = "body"
	GroupSleep       Group = "sleep"
	GroupNutrition   Group = "nutrition"
	GroupRespiratory Group = "respiratory"
	GroupVitals      Group = "vitals"
	GroupMobility    Group = "mobility"
	GroupHearing     Group = "hearing"
	GroupCycle       Group = "cycle"
	GroupSymptoms    Group = "symptoms"
	GroupOther       Group = "other"
)

// Value is a value of a category type.
type Value struct {
	// Key is what a sample stores, such as asleep_core.
	Key string `yaml:"key"`
	// Identifier is the HealthKit identifier, such as HKCategoryValueSleepAnalysisAsleepCore.
	Identifier string `yaml:"identifier"`
}

// Type is a metric type of the catalog.
type Type struct {
	// Key names the type in the API and in the translations, such as heart_rate.
	Key string `yaml:"key"`
	// Identifier is the HealthKit identifier, such as HKQuantityTypeIdentifierHeartRate.
	Identifier string `yaml:"identifier"`
	Kind       Kind   `yaml:"kind"`
	// Unit is the canonical unit of the values, one of the Units table of docs/architecture.md.
	Unit        string      `yaml:"unit"`
	Aggregation Aggregation `yaml:"aggregation"`
	Group       Group       `yaml:"group"`
	// Values lists the values of a category type. A quantity type has none.
	Values []Value `yaml:"values"`
}

// Status tells what the catalog knows of an identifier.
type Status int

// The answers of Lookup.
const (
	// Unknown is an identifier that the catalog does not list.
	Unknown Status = iota
	// Known is the identifier of a type of the catalog.
	Known
	// Unsupported is an identifier of data that Verve does not store yet.
	Unsupported
)

// Catalog is the parsed catalog. It never changes once parsed, so that it can be shared.
type Catalog struct {
	types        []Type
	byIdentifier map[string]int // index in types
	unsupported  unsupported
}

// unsupported lists the identifiers that are known but unsupported.
type unsupported struct {
	Identifiers []string `yaml:"identifiers"`
	// Prefixes make unsupported every identifier that starts with one of them.
	Prefixes []string `yaml:"prefixes"`
}

// file is the layout of catalog.yaml.
type file struct {
	Types       []Type      `yaml:"types"`
	Unsupported unsupported `yaml:"unsupported"`
}

// shipped is the catalog of the binary.
//
//go:embed catalog.yaml
var shipped string

// Shipped parses the catalog of the binary. Call it once at startup and share the result.
func Shipped() (*Catalog, error) {
	return Parse([]byte(shipped))
}

// Parse reads a catalog in the layout of catalog.yaml.
func Parse(data []byte) (*Catalog, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}

	if err := f.validate(); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}

	c := &Catalog{
		types:        f.Types,
		byIdentifier: make(map[string]int, len(f.Types)),
		unsupported:  f.Unsupported,
	}

	for i, t := range f.Types {
		c.byIdentifier[t.Identifier] = i
	}

	return c, nil
}

// snakeCase matches a key: lower case words of ASCII letters and digits joined by underscores.
var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// validate checks the rules of the catalog, and names the entry that breaks one.
func (f file) validate() error {
	keys := make(map[string]bool, len(f.Types))
	identifiers := make(map[string]bool, len(f.Types))

	for _, t := range f.Types {
		if err := t.validate(); err != nil {
			return fmt.Errorf("type %q (%s): %w", t.Key, t.Identifier, err)
		}

		if keys[t.Key] {
			return fmt.Errorf("type %q (%s): key is already used", t.Key, t.Identifier)
		}

		if identifiers[t.Identifier] {
			return fmt.Errorf("type %q (%s): identifier is already used", t.Key, t.Identifier)
		}

		keys[t.Key], identifiers[t.Identifier] = true, true
	}

	unsupported := make(map[string]bool, len(f.Unsupported.Identifiers))

	for _, identifier := range f.Unsupported.Identifiers {
		switch {
		case identifier == "":
			return fmt.Errorf("unsupported identifier %q: identifier is missing", identifier)
		case identifiers[identifier]:
			return fmt.Errorf("unsupported identifier %q: a type has this identifier", identifier)
		case unsupported[identifier]:
			return fmt.Errorf("unsupported identifier %q: identifier is already used", identifier)
		}

		unsupported[identifier] = true
	}

	for _, prefix := range f.Unsupported.Prefixes {
		if prefix == "" {
			return fmt.Errorf("unsupported prefix %q: prefix is missing", prefix)
		}

		for _, t := range f.Types {
			if strings.HasPrefix(t.Identifier, prefix) {
				return fmt.Errorf("unsupported prefix %q: type %q (%s) starts with it", prefix, t.Key, t.Identifier)
			}
		}
	}

	return nil
}

// validate checks the rules that apply to one type.
func (t Type) validate() error {
	switch {
	case !snakeCase.MatchString(t.Key):
		return errors.New("key is not in snake case")
	case t.Identifier == "":
		return errors.New("identifier is missing")
	case t.Kind != KindQuantity && t.Kind != KindCategory:
		return fmt.Errorf("kind %q is not quantity or category", t.Kind)
	case !canonicalUnit(t.Unit):
		return fmt.Errorf("unit %q is not a canonical unit", t.Unit)
	case t.Aggregation != AggregationCumulative && t.Aggregation != AggregationDiscrete:
		return fmt.Errorf("aggregation %q is not cumulative or discrete", t.Aggregation)
	case !displayGroup(t.Group):
		return fmt.Errorf("group %q is not a display group", t.Group)
	case t.Kind == KindCategory && len(t.Values) == 0:
		return errors.New("a category has at least one value")
	case t.Kind == KindQuantity && len(t.Values) > 0:
		return errors.New("a quantity has no values")
	}

	keys := make(map[string]bool, len(t.Values))
	identifiers := make(map[string]bool, len(t.Values))

	for _, v := range t.Values {
		switch {
		case !snakeCase.MatchString(v.Key):
			return fmt.Errorf("value %q (%s): key is not in snake case", v.Key, v.Identifier)
		case v.Identifier == "":
			return fmt.Errorf("value %q (%s): identifier is missing", v.Key, v.Identifier)
		case keys[v.Key]:
			return fmt.Errorf("value %q (%s): key is already used", v.Key, v.Identifier)
		case identifiers[v.Identifier]:
			return fmt.Errorf("value %q (%s): identifier is already used", v.Key, v.Identifier)
		}

		keys[v.Key], identifiers[v.Identifier] = true, true
	}

	return nil
}

// canonicalUnit reports whether unit is a canonical unit of the Units table of docs/architecture.md.
// A category type, whose samples are counted, has the unit count.
func canonicalUnit(unit string) bool {
	switch unit {
	case "count", "kg", "m", "L", "kcal", "s", "ms", "count/min", "m/s", "W", "degC", "fraction",
		"mg/dL", "mmHg", "mL/(kg.min)", "kcal/(kg.h)", "appleEffortScore", "dBASPL", "L/min", "S", "IU":
		return true
	}

	return false
}

// displayGroup reports whether group is a display group of FR-1.
func displayGroup(group Group) bool {
	switch group {
	case GroupActivity, GroupHeart, GroupBody, GroupSleep, GroupNutrition, GroupRespiratory, GroupVitals,
		GroupMobility, GroupHearing, GroupCycle, GroupSymptoms, GroupOther:
		return true
	}

	return false
}

// Lookup returns the type of a HealthKit identifier and Known, or reports the identifier as
// Unsupported or Unknown with a zero type.
func (c *Catalog) Lookup(identifier string) (Type, Status) {
	if i, ok := c.byIdentifier[identifier]; ok {
		return clone(c.types[i]), Known
	}

	if slices.Contains(c.unsupported.Identifiers, identifier) {
		return Type{}, Unsupported
	}

	for _, prefix := range c.unsupported.Prefixes {
		if strings.HasPrefix(identifier, prefix) {
			return Type{}, Unsupported
		}
	}

	return Type{}, Unknown
}

// Types returns every type, in the order of the catalog file.
func (c *Catalog) Types() []Type {
	types := make([]Type, 0, len(c.types))
	for _, t := range c.types {
		types = append(types, clone(t))
	}

	return types
}

// clone copies t, so that a caller never changes the catalog.
func clone(t Type) Type {
	t.Values = slices.Clone(t.Values)

	return t
}
