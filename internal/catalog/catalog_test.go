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

// TestCatalogWellFormed guards every entry's invariants.
func TestCatalogWellFormed(t *testing.T) {
	valid := map[Aggregation]bool{Sum: true, Average: true, Latest: true, DurationByState: true}
	for slug, m := range All() {
		if m.Slug != slug {
			t.Errorf("metric keyed %q has Slug %q", slug, m.Slug)
		}
		if m.Unit == "" {
			t.Errorf("metric %q has empty unit", slug)
		}
		if !valid[m.Aggregation] {
			t.Errorf("metric %q has invalid aggregation %q", slug, m.Aggregation)
		}
		if m.Nature != Imported {
			t.Errorf("metric %q nature = %q, want imported", slug, m.Nature)
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
