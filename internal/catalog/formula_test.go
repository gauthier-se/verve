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

// TestFormulaEvaluate exercises the per-bucket combine (ADR 0014): a weighted
// sum, a signed difference, a ratio, and a scaled ratio — each computed from one
// bucket's operand values.
func TestFormulaEvaluate(t *testing.T) {
	tests := map[string]struct {
		formula Formula
		values  map[string]float64
		want    float64
	}{
		"weighted sum": {
			formula: Formula{Scale: 1, Numerator: []Term{
				{Metric: "active_energy", Coefficient: 1},
				{Metric: "basal_energy", Coefficient: 1},
			}},
			values: map[string]float64{"active_energy": 400, "basal_energy": 1600},
			want:   2000,
		},
		"signed difference": {
			formula: Formula{Scale: 1, Numerator: []Term{
				{Metric: "dietary_energy", Coefficient: 1},
				{Metric: "active_energy", Coefficient: -1},
				{Metric: "basal_energy", Coefficient: -1},
			}},
			values: map[string]float64{"dietary_energy": 1800, "active_energy": 400, "basal_energy": 1600},
			want:   -200,
		},
		"ratio": {
			formula: Formula{Scale: 1,
				Numerator:   []Term{{Metric: "dietary_protein", Coefficient: 1}},
				Denominator: []Term{{Metric: "body_mass", Coefficient: 1}}},
			values: map[string]float64{"dietary_protein": 120, "body_mass": 80},
			want:   1.5,
		},
		"scaled ratio with coefficient": {
			formula: Formula{Scale: 100,
				Numerator:   []Term{{Metric: "dietary_protein", Coefficient: 4}},
				Denominator: []Term{{Metric: "dietary_energy", Coefficient: 1}}},
			values: map[string]float64{"dietary_protein": 100, "dietary_energy": 2000},
			want:   20, // 100 · (4·100) / 2000
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := tc.formula.Evaluate(tc.values)
			if !ok {
				t.Fatalf("Evaluate returned ok=false, want a value")
			}
			if got != tc.want {
				t.Errorf("Evaluate = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFormulaEvaluateGaps proves the gap semantics: a missing operand (numerator
// or denominator), a zero denominator, and an absent denominator all yield a gap
// rather than a zero — never zero-filling (ADR 0014).
func TestFormulaEvaluateGaps(t *testing.T) {
	ratio := Formula{Scale: 1,
		Numerator:   []Term{{Metric: "dietary_protein", Coefficient: 1}},
		Denominator: []Term{{Metric: "body_mass", Coefficient: 1}}}
	sum := Formula{Scale: 1, Numerator: []Term{
		{Metric: "active_energy", Coefficient: 1},
		{Metric: "basal_energy", Coefficient: 1},
	}}

	tests := map[string]struct {
		formula Formula
		values  map[string]float64
	}{
		"missing numerator operand":   {ratio, map[string]float64{"body_mass": 80}},
		"missing denominator operand": {ratio, map[string]float64{"dietary_protein": 120}},
		"zero denominator":            {ratio, map[string]float64{"dietary_protein": 120, "body_mass": 0}},
		"missing sum operand":         {sum, map[string]float64{"active_energy": 400}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if v, ok := tc.formula.Evaluate(tc.values); ok {
				t.Errorf("Evaluate = %v, ok=true; want a gap (ok=false)", v)
			}
		})
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
