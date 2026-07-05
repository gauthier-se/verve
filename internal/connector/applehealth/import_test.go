package applehealth

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/data"
)

// openStore opens a fresh migrated DB and returns its MeasurementModel (which
// satisfies Store) plus a seeded account id.
func openStore(t *testing.T) (data.MeasurementModel, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.db")
	db, err := data.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	models := data.NewModels(db)
	acc := &data.Account{Email: "owner@example.com"}
	if err := models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return models.Measurements, acc.ID
}

// sampleXML is a tiny export: two mappable scalar Records (one in kg, one a
// sleep category that is out of scope), plus a Correlation whose child Record
// duplicates a top-level one (must not be double-counted).
const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<HealthData locale="en_US">
 <ExportDate value="2024-01-02 10:00:00 +0000"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Watch" unit="count" startDate="2024-01-01 08:00:00 +0000" endDate="2024-01-01 09:00:00 +0000" creationDate="2024-01-01 09:05:00 +0000" value="120"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="kg" startDate="2024-01-01 07:00:00 +0000" endDate="2024-01-01 07:00:00 +0000" value="70.5"/>
 <Record type="HKCategoryTypeIdentifierSleepAnalysis" sourceName="Watch" startDate="2024-01-01 23:00:00 +0000" endDate="2024-01-02 06:00:00 +0000" value="HKCategoryValueSleepAnalysisAsleepCore"/>
 <Correlation type="HKCorrelationTypeIdentifierFood" sourceName="Yazio" startDate="2024-01-01 12:00:00 +0000" endDate="2024-01-01 12:00:00 +0000">
  <Record type="HKQuantityTypeIdentifierStepCount" sourceName="Watch" unit="count" startDate="2024-01-01 08:00:00 +0000" endDate="2024-01-01 09:00:00 +0000" value="120"/>
 </Correlation>
</HealthData>`

func TestImportStreamMapsAndBins(t *testing.T) {
	store, acc := openStore(t)
	ctx := context.Background()

	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML))
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}

	// Two distinct top-level scalar Records mapped; the Correlation child is a
	// duplicate of the step count and must not add a third measurement.
	if report.Added != 2 {
		t.Errorf("Added = %d, want 2", report.Added)
	}
	if got := report.PerMetric["steps"].Added; got != 1 {
		t.Errorf("steps added = %d, want 1", got)
	}
	if got := report.PerMetric["body_mass"].Added; got != 1 {
		t.Errorf("body_mass added = %d, want 1", got)
	}
	// The sleep category is out of scope → Unmapped bin, kept not dropped.
	if report.Unmapped != 1 {
		t.Errorf("Unmapped = %d, want 1", report.Unmapped)
	}
	if got := report.UnmappedTypes["HKCategoryTypeIdentifierSleepAnalysis"]; got != 1 {
		t.Errorf("unmapped sleep count = %d, want 1", got)
	}

	// A SELECT shows canonical slugs, canonical units, normalized times, owner.
	var metric, unit, start, source string
	var value float64
	err = store.DB.QueryRowContext(ctx,
		`SELECT metric, value, original_unit, start_at, source FROM measurements
		 WHERE account_id = ? AND metric = 'steps'`, acc).
		Scan(&metric, &value, &unit, &start, &source)
	if err != nil {
		t.Fatalf("select steps: %v", err)
	}
	if value != 120 {
		t.Errorf("steps value = %v, want 120", value)
	}
	if start != "2024-01-01T08:00:00Z" {
		t.Errorf("start_at = %q, want RFC3339 UTC", start)
	}
	if source != "Watch" {
		t.Errorf("source = %q, want Watch", source)
	}
}

// TestImportStreamIdempotent is the acceptance guard: importing the same stream
// twice adds nothing the second time (ADR 0006).
func TestImportStreamIdempotent(t *testing.T) {
	store, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML)); err != nil {
		t.Fatalf("first import: %v", err)
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML))
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if report.Added != 0 {
		t.Errorf("re-import Added = %d, want 0", report.Added)
	}
	if report.Skipped != 2 {
		t.Errorf("re-import Skipped = %d, want 2", report.Skipped)
	}

	var count int
	if err := store.DB.QueryRowContext(ctx, `SELECT count(*) FROM measurements WHERE account_id = ?`, acc).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("measurement rows after re-import = %d, want 2", count)
	}
}

// TestImportStreamNormalizesUnits feeds a body mass in grams and expects it
// stored in the canonical kg, with the original unit preserved.
func TestImportStreamNormalizesUnits(t *testing.T) {
	store, acc := openStore(t)
	ctx := context.Background()

	const grams = `<HealthData locale="en_US">
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="g" startDate="2024-02-01 07:00:00 +0000" endDate="2024-02-01 07:00:00 +0000" value="70500"/>
</HealthData>`

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(grams)); err != nil {
		t.Fatalf("importStream: %v", err)
	}

	var value float64
	var unit string
	if err := store.DB.QueryRowContext(ctx,
		`SELECT value, original_unit FROM measurements WHERE account_id = ? AND metric = 'body_mass'`, acc).
		Scan(&value, &unit); err != nil {
		t.Fatalf("select: %v", err)
	}
	if value != 70.5 {
		t.Errorf("value = %v, want 70.5 kg", value)
	}
	if unit != "g" {
		t.Errorf("original_unit = %q, want g", unit)
	}
}

// TestMappingMatchesCatalog guards ADR 0009: every mapping target is a real
// Catalog slug, and every Catalog Metric has an Apple mapping (broad seed).
func TestMappingMatchesCatalog(t *testing.T) {
	for appleType, slug := range typeToMetric {
		if _, ok := catalog.Lookup(slug); !ok {
			t.Errorf("mapping %s → %q targets a slug absent from the Catalog", appleType, slug)
		}
	}

	mapped := make(map[string]bool, len(typeToMetric))
	for _, slug := range typeToMetric {
		mapped[slug] = true
	}
	for slug := range catalog.All() {
		if !mapped[slug] {
			t.Errorf("Catalog metric %q has no Apple mapping", slug)
		}
	}
}
