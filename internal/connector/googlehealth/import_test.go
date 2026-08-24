package googlehealth

import (
	"archive/zip"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/data"
)

// testStore satisfies connector.Store by embedding the family models, exactly as
// the CLI's ImportStore does. Every family is present even though this Connector
// writes one: the contract is the contract.
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

// sampleArchive is the reference export in miniature: the same directory layout,
// the same headers, the same Source names, and one file of each kind the Connector
// has to tell apart.
var sampleArchive = map[string]string{
	// A mapped family, sharded by month, in grams, the unit its README states.
	"Takeout/Google Health/Physical Activity_GoogleData/weight_2026-05-01.csv": "" +
		"timestamp,weight grams,data source\n" +
		"2026-05-27T05:33:27Z,93400,YAZIO Health Kit\n" +
		"2026-05-27T05:33:27Z,93400,Zepp Life Health Kit\n" +
		"2026-05-28T05:19:44Z,93600,YAZIO Health Kit\n",

	// Metres, and microseconds on the timestamp.
	"Takeout/Google Health/Physical Activity_GoogleData/distance_2026-08-01.csv": "" +
		"timestamp,distance,data source\n" +
		"2026-08-01T01:15:09.096319Z,8.80,Apple Health Health Kit\n" +
		"2026-08-01T01:16:43.873844Z,20.14,Phone Health Kit\n",

	// The same reading under two family names is what the real export does, and it
	// writes the value "51" in one and "51.0" in the other, which is why the key
	// hashes a canonical rendering rather than the cell.
	"Takeout/Google Health/Physical Activity_GoogleData/resting_heart_rate_2026-05-27.csv": "" +
		"timestamp,beats per minute,data source\n" +
		"2026-05-27T22:01:13Z,51,Apple Health Health Kit\n",
	"Takeout/Google Health/Physical Activity_GoogleData/daily_resting_heart_rate.csv": "" +
		"timestamp,beats per minute,data source\n" +
		"2026-05-27T22:01:13Z,51.0,Apple Health Health Kit\n",

	// A family with no Metric: kept whole in the bin rather than dropped.
	"Takeout/Google Health/Physical Activity_GoogleData/calories_in_heart_rate_zone_2026-08-24.csv": "" +
		"timestamp,heart rate zone type,kcal,data source\n" +
		"2026-08-24T11:55:00Z,HEART_RATE_ZONE_TYPE_UNSPECIFIED,1.24463,Google Health App\n",

	// The legacy Fitbit JSON: same quantity, US units, no attribution.
	"Takeout/Google Health/Global Export Data/weight-2026-08-23.json": `[{"logId":1787615999000,"weight":191.8,"bmi":25.98,"date":"08/24/26","time":"00:00:00"}]`,

	// Not health data: settings, a README, an avatar.
	"Takeout/Google Health/Health Fitness Data_GoogleData/UserAppSettingData.csv": "" +
		"value_time,setting_name,setting_value\n" +
		"2026-08-24T11:54:36Z,weight system,metric\n",
	"Takeout/Google Health/Physical Activity_GoogleData/weight_readme.txt": "Time Series Data Export\n",
	"Takeout/Google Health/Your Profile/Media_Avatar Photo.png":            "\x89PNG",

	// Another Takeout service entirely, which this Connector must not touch.
	"Takeout/Google Photos/photo.json": `{"title":"beach.jpg"}`,
}

// writeArchive writes entries into a .zip and returns its path.
func writeArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "takeout.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

func importSample(t *testing.T, store testStore, acc int64, opts connector.Options) connector.Report {
	t.Helper()
	report, err := Import(context.Background(), store, acc, writeArchive(t, sampleArchive), opts)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return report
}

// The units are read from the READMEs Google ships, not inferred: grams to
// kilograms and metres to kilometres, or 93.4 kg becomes 93,400.
func TestImportConvertsToCanonicalUnits(t *testing.T) {
	store, db, acc := openStore(t)
	importSample(t, store, acc, connector.Options{})

	for _, tc := range []struct {
		metric, source string
		want           float64
	}{
		{"body_mass", "YAZIO Health Kit", 93.4},
		{"distance", "Apple Health Health Kit", 0.0088},
	} {
		var got float64
		err := db.QueryRow(`SELECT value FROM measurements WHERE account_id = ? AND metric = ? AND source = ? ORDER BY start_at LIMIT 1`,
			acc, tc.metric, tc.source).Scan(&got)
		if err != nil {
			t.Fatalf("%s: %v", tc.metric, err)
		}
		if diff := got - tc.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s = %v, want %v", tc.metric, got, tc.want)
		}
	}
}

// Google's microsecond timestamps are stored as RFC 3339 UTC, byte for byte what
// the Apple Connector writes, so rows from the two bucket and compare identically.
func TestImportNormalizesTimestamps(t *testing.T) {
	store, db, acc := openStore(t)
	importSample(t, store, acc, connector.Options{})

	var start, end string
	err := db.QueryRow(`SELECT start_at, end_at FROM measurements WHERE metric = 'distance' ORDER BY start_at LIMIT 1`).Scan(&start, &end)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if start != "2026-08-01T01:15:09Z" {
		t.Errorf("start_at = %q, want 2026-08-01T01:15:09Z", start)
	}
	// A Google row is an instant where an Apple row is an interval; claiming a
	// duration the export does not state would be inventing one.
	if end != start {
		t.Errorf("end_at = %q, want it equal to start_at", end)
	}
}

// The export publishes resting heart rate twice, once per reading and once as a
// daily figure, holding the same instants and the same values from the same app,
// written "51" in one family and "51.0" in the other. No special case handles it:
// the Content key hashes a canonical rendering, so the two agree (see canonical).
func TestDuplicateFamiliesCollapseByContentKey(t *testing.T) {
	store, db, acc := openStore(t)
	report := importSample(t, store, acc, connector.Options{})

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM measurements WHERE metric = 'resting_heart_rate'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("resting_heart_rate rows = %d, want 1", n)
	}
	if got := report.PerMetric["resting_heart_rate"]; got.Added != 1 || got.Skipped != 1 {
		t.Errorf("tally = %+v, want 1 added and 1 skipped", got)
	}
}

// The same measurement from two apps is two rows, not one: keeping every Source is
// the whole of ADR 0003, and the read path elects between them.
func TestEverySourceIsKept(t *testing.T) {
	store, db, acc := openStore(t)
	importSample(t, store, acc, connector.Options{})

	rows, err := db.Query(`SELECT source FROM measurements WHERE metric = 'body_mass' AND start_at = '2026-05-27T05:33:27Z' ORDER BY source`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		sources = append(sources, s)
	}
	if len(sources) != 2 || sources[0] != "YAZIO Health Kit" || sources[1] != "Zepp Life Health Kit" {
		t.Errorf("sources = %v, want both apps kept verbatim", sources)
	}
}

// A family with no Metric is kept whole and inspectable (ADR 0002), and the legacy
// JSON goes there too: same quantities, US units, no attribution.
func TestUnmappedFamiliesAreKept(t *testing.T) {
	store, db, acc := openStore(t)
	report := importSample(t, store, acc, connector.Options{})

	for _, want := range []string{"google_health/calories_in_heart_rate_zone", "google_health/global_export/weight"} {
		var value string
		err := db.QueryRow(`SELECT value FROM unmapped_records WHERE account_id = ? AND source_type = ?`, acc, want).Scan(&value)
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if value == "" {
			t.Errorf("%s kept an empty value", want)
		}
		if report.UnmappedTypes[want] != 1 {
			t.Errorf("%s reported %d times, want 1", want, report.UnmappedTypes[want])
		}
	}
}

// Settings, READMEs and an avatar are not health records, so they are not binned,
// and not silent either. A Photos entry from the same Takeout is untouched.
func TestNonHealthFilesAreIgnoredAndCounted(t *testing.T) {
	store, _, acc := openStore(t)
	report := importSample(t, store, acc, connector.Options{})

	if report.Ignored["Health Fitness Data_GoogleData"] != 1 {
		t.Errorf("settings ignored = %d, want 1", report.Ignored["Health Fitness Data_GoogleData"])
	}
	if report.Ignored["Physical Activity_GoogleData"] != 1 {
		t.Errorf("readme ignored = %d, want 1 (the readme beside the series)", report.Ignored["Physical Activity_GoogleData"])
	}
	if report.Ignored["Your Profile"] != 1 {
		t.Errorf("avatar ignored = %d, want 1", report.Ignored["Your Profile"])
	}
	for dir := range report.Ignored {
		if dir == "." || dir == "" {
			t.Errorf("an entry outside Google Health was counted: %q", dir)
		}
	}
}

// Re-importing the same Takeout adds nothing: the export is cumulative and the
// owner will drop a bigger one every month (ADR 0006).
func TestReimportIsIdempotent(t *testing.T) {
	store, _, acc := openStore(t)
	first := importSample(t, store, acc, connector.Options{})
	second := importSample(t, store, acc, connector.Options{})

	if second.Added != 0 {
		t.Errorf("second import added %d rows, want 0", second.Added)
	}
	if second.Skipped != first.Added+first.Skipped {
		t.Errorf("second import skipped %d, want %d", second.Skipped, first.Added+first.Skipped)
	}
	if second.Unmapped != 0 {
		t.Errorf("second import kept %d new unmapped rows, want 0", second.Unmapped)
	}
}

// An Exclusion refuses what it names at import, from every Connector, and is
// counted rather than swallowed (ADR 0033).
func TestExclusionsAreHonoured(t *testing.T) {
	store, db, acc := openStore(t)
	report := importSample(t, store, acc, connector.Options{
		Exclusions: data.ExclusionSet{"body_mass": {{Metric: "body_mass"}}},
	})

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM measurements WHERE metric = 'body_mass'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("body_mass rows = %d, want none", n)
	}
	if report.Excluded != 3 || report.PerMetric["body_mass"].Excluded != 3 {
		t.Errorf("excluded = %d (per-metric %d), want 3", report.Excluded, report.PerMetric["body_mass"].Excluded)
	}
}

// Progress is reported in bytes against the entries the Connector will read, so the
// web import's second phase means the same thing here as for a streamed XML export.
func TestProgressReachesTotal(t *testing.T) {
	store, _, acc := openStore(t)

	var lastDecoded, total int64
	importSample(t, store, acc, connector.Options{
		Progress: func(decoded, t int64) { lastDecoded, total = decoded, t },
	})
	if total <= 0 {
		t.Fatalf("total = %d, want the summed size of the entries read", total)
	}
	if lastDecoded != total {
		t.Errorf("decoded = %d, want it to reach total %d", lastDecoded, total)
	}
}

// A mapped family whose value column is missing fails loudly rather than reading
// whichever column happens to sit at that index.
func TestMissingValueColumnFails(t *testing.T) {
	store, _, acc := openStore(t)
	path := writeArchive(t, map[string]string{
		"Takeout/Google Health/Physical Activity_GoogleData/steps_2026-08-01.csv": "timestamp,paces,data source\n2026-08-01T01:15:09Z,12,Phone Health Kit\n",
	})
	if _, err := Import(context.Background(), store, acc, path, connector.Options{}); err == nil {
		t.Fatal("Import succeeded, want an error naming the missing column")
	}
}

// The Import is recorded with the Connector that ran it, which is what lets the
// History say where an archive came from once there are two.
func TestImportIsRecordedWithItsConnector(t *testing.T) {
	store, db, acc := openStore(t)
	importSample(t, store, acc, connector.Options{})

	var name, file string
	if err := db.QueryRow(`SELECT connector, source_file FROM imports WHERE account_id = ?`, acc).Scan(&name, &file); err != nil {
		t.Fatalf("query import: %v", err)
	}
	if name != connectorName {
		t.Errorf("connector = %q, want %q", name, connectorName)
	}
	if file != "takeout.zip" {
		t.Errorf("source_file = %q, want takeout.zip", file)
	}
}

// TestMappingTargetsExist guards the Connector's half of ADR 0009: every mapping
// target is a real Catalog slug. The other half, that no imported Catalog Metric
// is an orphan, lives in internal/connector/registry, because it is an invariant
// about the Catalog and not about any one source.
func TestMappingTargetsExist(t *testing.T) {
	for fam, m := range familyToMetric {
		metric, ok := catalog.Lookup(m.Metric)
		if !ok {
			t.Errorf("mapping %s → %q targets a slug absent from the Catalog", fam, m.Metric)
			continue
		}
		if metric.Nature != catalog.Imported {
			t.Errorf("mapping %s → %q targets a derived Metric, which owns no rows", fam, m.Metric)
		}
	}
}

// family is the file identity the mapping is keyed by: the basename without its
// date shard, whether the export shards by day, by month or not at all.
func TestFamilyStripsTheDateShard(t *testing.T) {
	cases := map[string]string{
		"Physical Activity_GoogleData/heart_rate_2026-08-20.csv":           "heart_rate",
		"Physical Activity_GoogleData/active_energy_burned_2026-07-01.csv": "active_energy_burned",
		"Physical Activity_GoogleData/daily_heart_rate_zones.csv":          "daily_heart_rate_zones",
		"Physical Activity_GoogleData/vo2_max.csv":                         "vo2_max",
		"Global Export Data/weight-2026-08-23.json":                        "weight",
	}
	for rel, want := range cases {
		if got := family(rel); got != want {
			t.Errorf("family(%q) = %q, want %q", rel, got, want)
		}
	}
}
