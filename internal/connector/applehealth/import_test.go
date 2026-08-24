package applehealth

import (
	"archive/zip"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/data"
)

// testStore satisfies Store by embedding the family models, exactly as the CLI's
// importStore does, so the Connector writes every family through one value.
type testStore struct {
	data.MeasurementModel
	data.StateModel
	data.SessionModel
}

// openStore opens a fresh migrated DB and returns a Store over every family plus
// the underlying handle (for SELECT assertions) and a seeded account id.
func openStore(t *testing.T) (testStore, *sql.DB, int64) {
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
	return testStore{models.Measurements, models.States, models.Sessions}, db, acc.ID
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
	store, db, acc := openStore(t)
	ctx := context.Background()

	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML), connector.Options{ArtifactsDir: t.TempDir()}, nil)
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
	// The sleep category is now a State, not an Unmapped record.
	if report.Unmapped != 0 {
		t.Errorf("Unmapped = %d, want 0", report.Unmapped)
	}
	if report.StatesAdded != 1 {
		t.Errorf("StatesAdded = %d, want 1", report.StatesAdded)
	}
	if got := report.PerState["sleep"].Added; got != 1 {
		t.Errorf("sleep states added = %d, want 1", got)
	}
	var kind, stateValue string
	if err := db.QueryRowContext(ctx,
		`SELECT kind, state_value FROM states WHERE account_id = ?`, acc).
		Scan(&kind, &stateValue); err != nil {
		t.Fatalf("select state: %v", err)
	}
	if kind != "sleep" || stateValue != "asleep_core" {
		t.Errorf("state = (%q, %q), want (sleep, asleep_core)", kind, stateValue)
	}

	// A SELECT shows canonical slugs, canonical units, normalized times, owner.
	var metric, unit, start, source string
	var value float64
	err = db.QueryRowContext(ctx,
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
	store, db, acc := openStore(t)
	ctx := context.Background()

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML), connector.Options{ArtifactsDir: t.TempDir()}, nil); err != nil {
		t.Fatalf("first import: %v", err)
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML), connector.Options{ArtifactsDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if report.Added != 0 {
		t.Errorf("re-import Added = %d, want 0", report.Added)
	}
	if report.Skipped != 2 {
		t.Errorf("re-import Skipped = %d, want 2", report.Skipped)
	}
	// The sleep State is idempotent too: nothing added, one skipped.
	if report.StatesAdded != 0 || report.StatesSkipped != 1 {
		t.Errorf("re-import states = %d added, %d skipped, want 0/1", report.StatesAdded, report.StatesSkipped)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM measurements WHERE account_id = ?`, acc).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("measurement rows after re-import = %d, want 2", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM states WHERE account_id = ?`, acc).Scan(&count); err != nil {
		t.Fatalf("count states: %v", err)
	}
	if count != 1 {
		t.Errorf("state rows after re-import = %d, want 1", count)
	}
}

// TestImportStreamNormalizesUnits feeds a body mass in grams and expects it
// stored in the canonical kg, with the original unit preserved.
func TestImportStreamNormalizesUnits(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	const grams = `<HealthData locale="en_US">
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="g" startDate="2024-02-01 07:00:00 +0000" endDate="2024-02-01 07:00:00 +0000" value="70500"/>
</HealthData>`

	if _, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(grams), connector.Options{ArtifactsDir: t.TempDir()}, nil); err != nil {
		t.Fatalf("importStream: %v", err)
	}

	var value float64
	var unit string
	if err := db.QueryRowContext(ctx,
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

// TestImportReportsDecodeBytes verifies the web import's honest second
// phase: reading a real .zip drives the Progress hook up to the export.xml entry's
// declared uncompressed size (ADR 0016).
func TestImportReportsDecodeBytes(t *testing.T) {
	store, _, acc := openStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "export.zip")
	writeExportZip(t, path, sampleXML)

	var lastDecoded, gotTotal atomic.Int64
	var calls atomic.Int64
	progress := func(decoded, total int64) {
		lastDecoded.Store(decoded)
		gotTotal.Store(total)
		calls.Add(1)
	}

	if _, err := Import(ctx, store, acc, path, connector.Options{ArtifactsDir: dir, Progress: progress}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	wantTotal := int64(len(sampleXML))
	if gotTotal.Load() != wantTotal {
		t.Errorf("progress total = %d, want %d (uncompressed export.xml size)", gotTotal.Load(), wantTotal)
	}
	if lastDecoded.Load() != wantTotal {
		t.Errorf("final decoded = %d, want %d (whole entry read)", lastDecoded.Load(), wantTotal)
	}
	if calls.Load() == 0 {
		t.Error("progress was never called")
	}
}

// writeExportZip writes a .zip holding xml at apple_health_export/export.xml, the
// nesting Apple uses, so findExportXML resolves it by base name.
func writeExportZip(t *testing.T, path, xml string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("apple_health_export/export.xml")
	if err != nil {
		t.Fatalf("zip create entry: %v", err)
	}
	if _, err := io.WriteString(w, xml); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

// TestMappingTargetsExist guards the Connector's half of ADR 0009: every mapping
// target is a real Catalog slug. A State kind is not asserted here: "stand" is
// deliberately not a Metric, since apple_stand_time already answers that question
// as a Measurement (CONTEXT.md). The other half, that no imported Catalog Metric
// is an orphan, moved to internal/connector/registry once a second Connector
// existed, because it is an invariant about the Catalog and not about Apple.
func TestMappingTargetsExist(t *testing.T) {
	for appleType, slug := range typeToMetric {
		if _, ok := catalog.Lookup(slug); !ok {
			t.Errorf("mapping %s → %q targets a slug absent from the Catalog", appleType, slug)
		}
	}
}

// An unbounded Exclusion refuses a Metric outright: nothing of it is written, and
// the Report says how much was refused rather than swallowing it (ADR 0033).
func TestImportRefusesAnExcludedMetric(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	opts := connector.Options{
		ArtifactsDir: t.TempDir(),
		Exclusions:   data.ExclusionSet{"body_mass": {{Metric: "body_mass"}}},
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML), opts, nil)
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}

	if report.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1", report.Excluded)
	}
	if got := report.PerMetric["body_mass"].Excluded; got != 1 {
		t.Errorf("body_mass excluded = %d, want 1", got)
	}
	if got := report.PerMetric["body_mass"].Added; got != 0 {
		t.Errorf("body_mass added = %d, want 0", got)
	}
	// The refused Record is not "skipped" either: skipped means the row was already
	// stored, and conflating the two would hide the count this feature owes.
	if got := report.PerMetric["body_mass"].Skipped; got != 0 {
		t.Errorf("body_mass skipped = %d, want 0", got)
	}
	if report.Added != 1 {
		t.Errorf("Added = %d, want 1 (the step count still lands)", report.Added)
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM measurements WHERE metric = 'body_mass'`).Scan(&rows); err != nil {
		t.Fatalf("count body_mass: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d body_mass rows written, want 0", rows)
	}

	// Not the Unmapped bin either: that bin is for what the Catalog cannot read, and
	// putting a refusal there would keep exactly what was asked to be dropped.
	var binned int
	if err := db.QueryRow(`SELECT COUNT(*) FROM unmapped_records`).Scan(&binned); err != nil {
		t.Fatalf("count unmapped: %v", err)
	}
	if binned != 0 {
		t.Errorf("%d rows in the Unmapped bin, want 0", binned)
	}
}

// A bounded Exclusion refuses only its span; the same Metric outside it still lands.
func TestImportRefusesOnlyTheExcludedSpan(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<HealthData locale="en_US">
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="kg" startDate="2023-12-31 07:00:00 +0000" endDate="2023-12-31 07:00:00 +0000" value="71.0"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="kg" startDate="2024-01-01 07:00:00 +0000" endDate="2024-01-01 07:00:00 +0000" value="70.5"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Scale" unit="kg" startDate="2024-06-01 07:00:00 +0000" endDate="2024-06-01 07:00:00 +0000" value="69.0"/>
</HealthData>`

	opts := connector.Options{
		ArtifactsDir: t.TempDir(),
		Exclusions:   data.ExclusionSet{"body_mass": {{Metric: "body_mass", StartsOn: "2024-01-01"}}},
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(xml), opts, nil)
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}
	if report.Excluded != 2 {
		t.Errorf("Excluded = %d, want 2", report.Excluded)
	}
	if report.Added != 1 {
		t.Errorf("Added = %d, want 1 (the reading before the bound)", report.Added)
	}

	var start string
	if err := db.QueryRow(`SELECT start_at FROM measurements WHERE metric = 'body_mass'`).Scan(&start); err != nil {
		t.Fatalf("read body_mass: %v", err)
	}
	if start != "2023-12-31T07:00:00Z" {
		t.Errorf("kept row starts at %q, want the one before the bound", start)
	}
}

// An Exclusion naming a Metric the export does not carry changes nothing, which is
// the state of most of the set for most imports.
func TestImportUnaffectedByIrrelevantExclusion(t *testing.T) {
	store, _, acc := openStore(t)
	ctx := context.Background()

	opts := connector.Options{
		ArtifactsDir: t.TempDir(),
		Exclusions:   data.ExclusionSet{"dietary_protein": {{Metric: "dietary_protein"}}},
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML), opts, nil)
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}
	if report.Excluded != 0 {
		t.Errorf("Excluded = %d, want 0", report.Excluded)
	}
	if report.Added != 2 {
		t.Errorf("Added = %d, want the same 2 an import with no exclusion adds", report.Added)
	}
}

// Sleep is stored as a State, and this milestone excludes Measurements. The API
// refuses a `sleep` Exclusion at the door (ADR 0033); if one reached the Connector
// anyway it must not silently appear to work.
func TestImportDoesNotExcludeStates(t *testing.T) {
	store, db, acc := openStore(t)
	ctx := context.Background()

	opts := connector.Options{
		ArtifactsDir: t.TempDir(),
		Exclusions:   data.ExclusionSet{"sleep": {{Metric: "sleep"}}},
	}
	report, err := importStream(ctx, store, acc, "export.xml", strings.NewReader(sampleXML), opts, nil)
	if err != nil {
		t.Fatalf("importStream: %v", err)
	}
	if report.Excluded != 0 {
		t.Errorf("Excluded = %d, want 0: a State is not reached by an Exclusion", report.Excluded)
	}
	var states int
	if err := db.QueryRow(`SELECT COUNT(*) FROM states`).Scan(&states); err != nil {
		t.Fatalf("count states: %v", err)
	}
	if states != 1 {
		t.Errorf("%d states written, want 1", states)
	}
}
