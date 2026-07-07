package catalog

import "testing"

func TestLookupKnownMetric(t *testing.T) {
	m, ok := Lookup("heart_rate")
	if !ok {
		t.Fatal("heart_rate not found in Catalog")
	}
	if m.Unit != "count/min" {
		t.Errorf("heart_rate unit = %q, want count/min", m.Unit)
	}
	if m.Aggregation != Average {
		t.Errorf("heart_rate aggregation = %q, want average", m.Aggregation)
	}
	if m.Nature != Imported {
		t.Errorf("heart_rate nature = %q, want imported", m.Nature)
	}
}

func TestLookupUnknownMetric(t *testing.T) {
	if _, ok := Lookup("not_a_metric"); ok {
		t.Error("Lookup of unknown slug returned ok=true")
	}
}

// TestCatalogIsBroad guards ADR 0011: the seed must stay broad so an import
// captures nearly everything. A regression that drops entries should fail here.
func TestCatalogIsBroad(t *testing.T) {
	if n := len(All()); n < 60 {
		t.Errorf("Catalog has %d metrics, want a broad seed (>= 60)", n)
	}
}

// TestCatalogWellFormed guards every entry's invariants. Imported and derived
// Metrics have mutually exclusive shapes: an imported Metric has a valid
// aggregation rule and no Formula; a derived Metric has a Formula and no rule.
func TestCatalogWellFormed(t *testing.T) {
	valid := map[Aggregation]bool{Sum: true, Average: true, Latest: true, DurationByState: true}
	for slug, m := range All() {
		if m.Slug != slug {
			t.Errorf("metric keyed %q has Slug %q", slug, m.Slug)
		}
		if m.Unit == "" {
			t.Errorf("metric %q has empty unit", slug)
		}
		switch m.Nature {
		case Imported:
			if !valid[m.Aggregation] {
				t.Errorf("imported metric %q has invalid aggregation %q", slug, m.Aggregation)
			}
			if m.Formula != nil {
				t.Errorf("imported metric %q carries a Formula", slug)
			}
		case Derived:
			if m.Aggregation != "" {
				t.Errorf("derived metric %q declares aggregation %q; it has no rule", slug, m.Aggregation)
			}
			if m.Formula == nil {
				t.Errorf("derived metric %q has no Formula", slug)
			}
		default:
			t.Errorf("metric %q has unknown nature %q", slug, m.Nature)
		}
	}
}

// TestNutritionAggregatesBySum reflects CONTEXT.md: each nutrient is a
// Measurement with sum aggregation.
func TestNutritionAggregatesBySum(t *testing.T) {
	for _, slug := range []string{"dietary_energy", "dietary_protein", "dietary_water"} {
		m, ok := Lookup(slug)
		if !ok {
			t.Fatalf("%s missing from Catalog", slug)
		}
		if m.Aggregation != Sum {
			t.Errorf("%s aggregation = %q, want sum", slug, m.Aggregation)
		}
	}
}
