package catalog

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

// TestLookupDerivedMetric is the acceptance case: calorie_balance resolves to a
// derived Metric with its Formula, unit kcal, signed = true, and no rule.
func TestLookupDerivedMetric(t *testing.T) {
	m, ok := Lookup("calorie_balance")
	if !ok {
		t.Fatal("calorie_balance not found in Catalog")
	}
	if m.Nature != Derived {
		t.Errorf("calorie_balance nature = %q, want derived", m.Nature)
	}
	if m.Unit != "kcal" {
		t.Errorf("calorie_balance unit = %q, want kcal", m.Unit)
	}
	if !m.Signed {
		t.Error("calorie_balance is not signed, want signed")
	}
	if m.Aggregation != "" {
		t.Errorf("calorie_balance declares aggregation %q, want none", m.Aggregation)
	}
	if m.Formula == nil {
		t.Fatal("calorie_balance has no Formula")
	}
}

// TestDerivedMetricsWellFormed is the build-time validation (ADR 0014): for
// every derived Metric, each operand slug exists in the Catalog and is imported,
// numerator terms share a unit, no aggregation rule is declared, and the declared
// unit is consistent with the operand units.
func TestDerivedMetricsWellFormed(t *testing.T) {
	derived := 0
	for slug, m := range All() {
		if m.Nature != Derived {
			continue
		}
		derived++

		if m.Aggregation != "" {
			t.Errorf("derived %q declares aggregation %q; derived Metrics have no rule", slug, m.Aggregation)
		}
		if m.Formula == nil {
			t.Errorf("derived %q has no Formula", slug)
			continue
		}
		f := *m.Formula
		if len(f.Numerator) == 0 {
			t.Errorf("derived %q has an empty numerator", slug)
		}
		if f.Scale == 0 {
			t.Errorf("derived %q has zero scale (would flatten every value to 0)", slug)
		}

		// Every operand must exist and be imported: the seed has no
		// derived-of-derived, which keeps per-bucket recompute one level deep.
		for _, s := range f.Operands() {
			op, ok := Lookup(s)
			if !ok {
				t.Errorf("derived %q references unknown slug %q", slug, s)
				continue
			}
			if op.Nature != Imported {
				t.Errorf("derived %q references non-imported operand %q", slug, s)
			}
		}

		units, err := acceptableUnits(f)
		if err != nil {
			t.Errorf("derived %q: %v", slug, err)
			continue
		}
		if !slices.Contains(units, m.Unit) {
			t.Errorf("derived %q declares unit %q, want one of %v from its Formula", slug, m.Unit, units)
		}
	}
	if derived == 0 {
		t.Fatal("no derived Metrics in the Catalog; expected the seed set")
	}
}

// TestFormulaValidationCatchesUnknownSlug proves the guard rejects an operand
// that is not in the Catalog.
func TestFormulaValidationCatchesUnknownSlug(t *testing.T) {
	f := Formula{Scale: 1, Numerator: []Term{{Metric: "not_a_metric", Coefficient: 1}}}
	if _, err := acceptableUnits(f); err == nil {
		t.Error("acceptableUnits accepted an unknown operand slug")
	}
}

// TestFormulaValidationCatchesMixedNumeratorUnits proves the guard rejects a
// numerator whose terms do not share a unit (dietary_protein is g, body_mass kg).
func TestFormulaValidationCatchesMixedNumeratorUnits(t *testing.T) {
	f := Formula{Scale: 1, Numerator: []Term{
		{Metric: "dietary_protein", Coefficient: 1},
		{Metric: "body_mass", Coefficient: 1},
	}}
	if _, err := acceptableUnits(f); err == nil {
		t.Error("acceptableUnits accepted a numerator that mixes units")
	}
}

// acceptableUnits returns the declared result units consistent with a Formula's
// operand units (ADR 0014). A weighted sum (empty denominator) keeps its shared
// numerator unit — kcal for the energy sums. A ratio may be declared either as
// "num/den" (protein_per_kg → g/kg) or, when its units cancel or a coefficient
// carries the conversion (the Atwater kcal/g factors of the macro shares), as
// the dimensionless "%". Coefficients are unit-less data here, so both readings
// of a ratio are accepted; the check still rejects an unrelated declared unit.
func acceptableUnits(f Formula) ([]string, error) {
	numUnit, err := termsUnit(f.Numerator)
	if err != nil {
		return nil, fmt.Errorf("numerator: %w", err)
	}
	if len(f.Denominator) == 0 {
		return []string{numUnit}, nil
	}
	denUnit, err := termsUnit(f.Denominator)
	if err != nil {
		return nil, fmt.Errorf("denominator: %w", err)
	}
	if numUnit == denUnit {
		return []string{"%"}, nil // units cancel: a dimensionless percentage
	}
	return []string{numUnit + "/" + denUnit, "%"}, nil
}

// termsUnit returns the shared Catalog unit of a weighted sum's terms, or an
// error if the terms are empty, reference an unknown slug, or mix units.
func termsUnit(terms []Term) (string, error) {
	if len(terms) == 0 {
		return "", errors.New("no terms")
	}
	unit := ""
	for i, t := range terms {
		m, ok := Lookup(t.Metric)
		if !ok {
			return "", fmt.Errorf("unknown slug %q", t.Metric)
		}
		if i == 0 {
			unit = m.Unit
		} else if m.Unit != unit {
			return "", fmt.Errorf("mixes units %q and %q", unit, m.Unit)
		}
	}
	return unit, nil
}
