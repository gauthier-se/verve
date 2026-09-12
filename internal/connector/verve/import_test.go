package verve

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/archive"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/connector/applehealth"
	"github.com/gauthier-se/verve/internal/connector/googlehealth"
	"github.com/gauthier-se/verve/internal/data"
)

var exportedAt = time.Date(2026, 9, 12, 8, 30, 0, 0, time.UTC)

// instance is one Verve: a migrated database, its models and its artifacts
// directory. The round trip needs two of them, or it proves only that a machine
// agrees with itself.
type instance struct {
	models    data.Models
	artifacts string
}

func newInstance(t *testing.T) instance {
	t.Helper()
	dir := t.TempDir()
	db, err := data.Open(filepath.Join(dir, "verve.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := data.Migrate(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	artifacts := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifacts, 0o750); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	return instance{models: data.NewModels(db), artifacts: artifacts}
}

func (in instance) account(t *testing.T, email string) int64 {
	t.Helper()
	acc := &data.Account{Email: email}
	if err := in.models.Accounts.Insert(context.Background(), acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return acc.ID
}

// measurement builds a Measurement with a real content key, because the reader
// validates the key's shape and a hand-written "k1" is not one.
func measurement(acc int64, metric, source, start, rawValue, unit string, value float64) data.Measurement {
	return data.Measurement{
		AccountID: acc, Metric: metric, Value: value, OriginalUnit: unit,
		StartAt: start, EndAt: start, Source: source,
		ContentKey: data.ContentKey(metric, source, start, start, rawValue, unit),
	}
}

// seed fills an Account with a row in every family, including a Manual entry, a
// workout with a stat and a Route, and a record in the Unmapped bin.
func seed(t *testing.T, in instance, acc int64) {
	t.Helper()
	ctx := context.Background()

	if _, err := in.models.Measurements.InsertBatch(ctx, []data.Measurement{
		measurement(acc, "steps", "Watch", "2026-03-01T08:00:00Z", "1200", "count", 1200),
		measurement(acc, "body_mass", "Scale", "2026-03-02T07:00:00Z", "71.5", "kg", 71.5),
		measurement(acc, "body_fat_percentage", "Scale", "2026-03-02T07:00:00Z", "18", "%", 18),
		measurement(acc, "body_mass", catalog.SourceManual, "2026-03-03T07:00:00Z", "71", "kg", 71),
	}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if _, err := in.models.States.InsertStateBatch(ctx, []data.State{{
		AccountID: acc, Kind: "sleep", StateValue: "asleep_rem",
		StartAt: "2026-03-01T23:00:00Z", EndAt: "2026-03-02T00:30:00Z", Source: "Watch",
		ContentKey: data.StateContentKey("sleep", "asleep_rem", "Watch", "2026-03-01T23:00:00Z", "2026-03-02T00:30:00Z"),
	}}); err != nil {
		t.Fatalf("InsertStateBatch: %v", err)
	}

	distance := 10.2
	session := &data.Session{
		AccountID: acc, ActivityType: "running",
		StartAt: "2026-03-03T06:00:00Z", EndAt: "2026-03-03T07:00:00Z",
		Duration: 3600, TotalDistance: &distance, Source: "Watch",
		ContentKey: data.SessionContentKey("running", "Watch", "2026-03-03T06:00:00Z", "2026-03-03T07:00:00Z"),
	}
	stats := []data.SessionStat{{Metric: "heart_rate", Stat: data.StatAverage, Value: 142}}
	routes := []data.Route{{
		AccountID: acc, Artifact: "0123456789abcdef.gpx",
		StartAt: session.StartAt, EndAt: session.EndAt, Source: "Watch",
		ContentKey: data.ContentKey("route", "Watch", session.StartAt, session.EndAt, "gpx", ""),
	}}
	if _, err := in.models.Sessions.InsertWorkout(ctx, session, stats, routes); err != nil {
		t.Fatalf("InsertWorkout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(in.artifacts, "0123456789abcdef.gpx"), []byte("<gpx></gpx>"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	if _, err := in.models.Imports.InsertUnmappedBatch(ctx, []data.UnmappedRecord{{
		AccountID: acc, SourceType: "HKQuantityTypeIdentifierUnknown", Value: "3", Unit: "count",
		StartAt: "2026-03-01T10:00:00Z", EndAt: "2026-03-01T10:00:00Z", Source: "Watch",
		ContentKey: data.ContentKey("HKQuantityTypeIdentifierUnknown", "Watch", "2026-03-01T10:00:00Z", "2026-03-01T10:00:00Z", "3", "count"),
	}}); err != nil {
		t.Fatalf("InsertUnmappedBatch: %v", err)
	}
	if err := in.models.Imports.Record(ctx, &data.Import{
		AccountID: acc, Connector: "applehealth", SourceFile: "export.zip", AddedCount: 4,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

// export writes an Account's Archive to a file and returns its path.
func export(t *testing.T, in instance, acc int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verve.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()
	src := archive.NewModelSource(in.models, acc, in.artifacts)
	if _, err := archive.Write(context.Background(), f, src, "test", exportedAt); err != nil {
		t.Fatalf("archive.Write: %v", err)
	}
	return path
}

// importInto runs this Connector the way both import paths do.
func importInto(t *testing.T, in instance, acc int64, path string, opts connector.Options) connector.Report {
	t.Helper()
	if opts.ArtifactsDir == "" {
		opts.ArtifactsDir = in.artifacts
	}
	report, err := Import(context.Background(), in.models.ImportStore(), acc, path, opts)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return report
}

// dataEntries reads an Archive's rows, leaving out the manifest: it carries the
// export timestamp and the profile, which are properties of the run and of the
// Account rather than of the data.
func dataEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zr.Close()

	out := map[string]string{}
	for _, f := range zr.File {
		if f.Name == archive.ManifestName || f.Name == archive.ImportsName {
			continue // provenance, deliberately not replayed, so deliberately not compared
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = string(body)
	}
	return out
}

func countRows(t *testing.T, in instance, acc int64, table string) int {
	t.Helper()
	counts, err := in.models.ExportCounts(context.Background(), acc)
	if err != nil {
		t.Fatalf("ExportCounts: %v", err)
	}
	switch table {
	case "measurements":
		return int(counts.Measurements)
	case "states":
		return int(counts.States)
	case "sessions":
		return int(counts.Sessions)
	case "unmapped":
		return int(counts.Unmapped)
	default:
		t.Fatalf("unknown table %q", table)
		return 0
	}
}

// The round trip is the milestone. Export an Account, import it into an empty
// one on another instance, export that, and the two Archives' data entries are
// the same bytes. One assertion pins the format, the ordering, the carriage of
// the content key and every family at once, and it is what fails the day a
// change makes the Archive lossy.
func TestRoundTripIsByteIdentical(t *testing.T) {
	source := newInstance(t)
	acc := source.account(t, "owner@example.com")
	seed(t, source, acc)
	first := export(t, source, acc)

	destination := newInstance(t)
	fresh := destination.account(t, "owner@example.com")
	importInto(t, destination, fresh, first, connector.Options{})
	second := export(t, destination, fresh)

	before, after := dataEntries(t, first), dataEntries(t, second)
	// Guard the guard: an empty Archive compares equal to another empty one, so
	// assert the fixture actually travelled before trusting the comparison.
	if n := strings.Count(before[archive.MeasurementsName], "\n"); n != 4 {
		t.Fatalf("the source Archive holds %d measurements, want the 4 seeded", n)
	}
	if _, ok := before["artifacts/0123456789abcdef.gpx"]; !ok {
		t.Fatalf("the source Archive carries no route file: %v", keys(before))
	}
	if len(before) != len(after) {
		t.Fatalf("the round trip changed the entry set: %v then %v", keys(before), keys(after))
	}
	for name, want := range before {
		got, ok := after[name]
		if !ok {
			t.Fatalf("%s did not survive the round trip", name)
		}
		if got != want {
			t.Fatalf("%s changed in the round trip:\n--- before\n%s\n--- after\n%s", name, want, got)
		}
	}
}

// Re-importing the same Archive adds nothing: the content key travelled, so the
// second run recognizes every row (ADR 0006).
func TestReimportAddsNothing(t *testing.T) {
	in := newInstance(t)
	acc := in.account(t, "owner@example.com")
	seed(t, in, acc)
	path := export(t, in, acc)

	fresh := in.account(t, "second@example.com")
	first := importInto(t, in, fresh, path, connector.Options{})
	second := importInto(t, in, fresh, path, connector.Options{})

	if first.Added == 0 {
		t.Fatal("the first import added nothing")
	}
	if second.Added != 0 || second.Skipped != first.Added {
		t.Fatalf("second import added %d and skipped %d, want 0 added and %d skipped",
			second.Added, second.Skipped, first.Added)
	}
	if second.SessionsAdded != 0 || second.StatesAdded != 0 {
		t.Fatalf("second import added %d sessions and %d states, want none",
			second.SessionsAdded, second.StatesAdded)
	}
}

// Importing an Archive into the Account it came from adds nothing, which is the
// test that fails the day the content key is recomputed instead of carried: a
// second derivation would store every reading twice, once under each identity.
func TestArchiveImportedBesideItsOwnRowsAddsNothing(t *testing.T) {
	in := newInstance(t)
	acc := in.account(t, "owner@example.com")
	seed(t, in, acc)
	before := countRows(t, in, acc, "measurements")

	report := importInto(t, in, acc, export(t, in, acc), connector.Options{})

	if report.Added != 0 {
		t.Fatalf("re-importing an Account's own Archive added %d rows, want 0", report.Added)
	}
	if got := countRows(t, in, acc, "measurements"); got != before {
		t.Fatalf("measurements = %d after the round trip, want %d", got, before)
	}
}

// An Exclusion refuses what an Archive carries, Manual rows included. The purge
// already deletes Manual rows (ADR 0033), so re-admitting one from an Archive
// would undo a deletion the owner asked for: the standing refusal is the more
// recent statement.
func TestExclusionsRefuseArchivedRowsIncludingManual(t *testing.T) {
	source := newInstance(t)
	acc := source.account(t, "owner@example.com")
	seed(t, source, acc)
	path := export(t, source, acc)

	destination := newInstance(t)
	fresh := destination.account(t, "owner@example.com")
	ctx := context.Background()
	for _, metric := range []string{"body_fat_percentage", "body_mass"} {
		if _, err := destination.models.Exclusions.Insert(ctx, &data.Exclusion{AccountID: fresh, Metric: metric}); err != nil {
			t.Fatalf("insert exclusion: %v", err)
		}
	}
	set, err := destination.models.Exclusions.Set(ctx, fresh)
	if err != nil {
		t.Fatalf("Exclusions.Set: %v", err)
	}

	report := importInto(t, destination, fresh, path, connector.Options{Exclusions: set})

	// One body fat, two body mass rows, and the Manual one is among them.
	if report.Excluded != 3 {
		t.Fatalf("report excluded %d rows, want 3", report.Excluded)
	}
	if report.PerMetric["body_mass"].Excluded != 2 {
		t.Fatalf("body_mass excluded = %d, want 2 (the imported row and the Manual one)",
			report.PerMetric["body_mass"].Excluded)
	}
	var manual int
	if err := destination.models.Measurements.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM measurements WHERE account_id = ? AND source = ?`, fresh, catalog.SourceManual,
	).Scan(&manual); err != nil {
		t.Fatalf("count manual: %v", err)
	}
	if manual != 0 {
		t.Fatalf("an excluded Manual row was restored anyway (%d rows)", manual)
	}
}

// A Manual Measurement is the one family nothing else can restore, and it comes
// back as Manual: still overlaying its day, still deletable, which imported rows
// are not (ADR 0022).
func TestManualRowsSurviveAsManual(t *testing.T) {
	source := newInstance(t)
	acc := source.account(t, "owner@example.com")
	seed(t, source, acc)
	path := export(t, source, acc)

	destination := newInstance(t)
	fresh := destination.account(t, "owner@example.com")
	importInto(t, destination, fresh, path, connector.Options{})

	ctx := context.Background()
	rows, err := destination.models.Measurements.ListManual(ctx, fresh, "", 10)
	if err != nil {
		t.Fatalf("ListManual: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("restored %d Manual rows, want 1", len(rows))
	}
	if err := destination.models.Measurements.Delete(ctx, fresh, rows[0].ID); err != nil {
		t.Fatalf("a restored Manual row is not deletable: %v", err)
	}
}

// An Archive from a newer Verve can name a Metric this build has never heard of.
// The bin is where a record whose type cannot be mapped belongs (ADR 0002), so
// nothing is lost and a later upgrade reads it as a Measurement.
func TestUnknownMetricGoesToTheBin(t *testing.T) {
	in := newInstance(t)
	acc := in.account(t, "owner@example.com")
	path := writeArchive(t, map[string]string{
		archive.ManifestName: `{"verve_archive":1,"schema":1,"counts":{"measurements":1}}`,
		archive.MeasurementsName: line(`{"metric":"vo2_max_but_newer","value":52,"original_unit":"ml/kg/min",` +
			`"start_at":"2026-03-01T08:00:00Z","end_at":"2026-03-01T08:00:00Z","source":"Watch","content_key":"` + key("a") + `"}`),
	})

	report := importInto(t, in, acc, path, connector.Options{})

	if report.Added != 0 {
		t.Fatalf("an unknown slug was stored as a Measurement (%d added)", report.Added)
	}
	if report.Unmapped != 1 || report.UnmappedTypes["vo2_max_but_newer"] != 1 {
		t.Fatalf("bin = %d rows %v, want one under the unknown slug", report.Unmapped, report.UnmappedTypes)
	}
}

// A damaged or hand-edited Archive stops the import naming the line. The
// Unmapped bin is for source data the Catalog cannot map, which is expected; a
// bad line here means the file is not what it says it is, and a partial import
// nobody notices is worse than a refusal.
func TestRefusalsNameTheProblemAndWriteNothing(t *testing.T) {
	valid := `{"metric":"steps","value":1,"original_unit":"count","start_at":"2026-03-01T08:00:00Z",` +
		`"end_at":"2026-03-01T08:00:00Z","source":"Watch","content_key":"` + key("a") + `"}`

	tests := map[string]struct {
		entries map[string]string
		wants   string
	}{
		"a newer schema": {
			entries: map[string]string{archive.ManifestName: `{"verve_archive":1,"schema":99,"counts":{}}`},
			wants:   "newer Verve",
		},
		"no manifest": {
			entries: map[string]string{archive.MeasurementsName: line(valid)},
			wants:   "manifest.json",
		},
		"a content key that is not one": {
			entries: map[string]string{
				archive.ManifestName:     `{"verve_archive":1,"schema":1,"counts":{"measurements":1}}`,
				archive.MeasurementsName: line(strings.Replace(valid, key("a"), "not-a-key", 1)),
			},
			wants: "content key",
		},
		"an interval that runs backwards": {
			entries: map[string]string{
				archive.ManifestName: `{"verve_archive":1,"schema":1,"counts":{"measurements":1}}`,
				archive.MeasurementsName: line(strings.Replace(valid,
					`"end_at":"2026-03-01T08:00:00Z"`, `"end_at":"2026-02-01T08:00:00Z"`, 1)),
			},
			wants: "end_at is before start_at",
		},
		"a line that is not JSON": {
			entries: map[string]string{
				archive.ManifestName:     `{"verve_archive":1,"schema":1,"counts":{"measurements":1}}`,
				archive.MeasurementsName: line(`{"metric":`),
			},
			wants: "measurements.ndjson line 1",
		},
		"fewer rows than the manifest promises": {
			entries: map[string]string{
				archive.ManifestName:     `{"verve_archive":1,"schema":1,"counts":{"measurements":7}}`,
				archive.MeasurementsName: line(valid),
			},
			wants: "incomplete",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			in := newInstance(t)
			acc := in.account(t, "owner@example.com")
			path := writeArchive(t, tc.entries)

			_, err := Import(context.Background(), in.models.ImportStore(), acc, path,
				connector.Options{ArtifactsDir: in.artifacts})
			if err == nil {
				t.Fatal("the import should have been refused")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// A truncated file is not a zip, and the refusal has to name the file rather
// than surfacing a decoder's idea of the problem.
func TestATruncatedArchiveIsRefused(t *testing.T) {
	in := newInstance(t)
	acc := in.account(t, "owner@example.com")
	seed(t, in, acc)
	full, err := os.ReadFile(export(t, in, acc))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	path := filepath.Join(t.TempDir(), "truncated.zip")
	if err := os.WriteFile(path, full[:len(full)/2], 0o600); err != nil {
		t.Fatalf("write truncated: %v", err)
	}

	if _, err := Import(context.Background(), in.models.ImportStore(), acc, path, connector.Options{}); err == nil {
		t.Fatal("a truncated archive should be refused")
	}
	if Accepts(path) {
		t.Fatal("a truncated archive should not be recognized")
	}
}

// Detection is by content and the three detectors must stay disjoint, or which
// Connector reads a file becomes a function of the order in the registry.
func TestDetectionIsDisjoint(t *testing.T) {
	in := newInstance(t)
	acc := in.account(t, "owner@example.com")
	seed(t, in, acc)
	archivePath := export(t, in, acc)

	applePath := filepath.Join(t.TempDir(), "export.xml")
	if err := os.WriteFile(applePath, []byte(`<HealthData locale="en_US"></HealthData>`), 0o600); err != nil {
		t.Fatalf("write apple export: %v", err)
	}

	if !Accepts(archivePath) {
		t.Fatal("the Verve Connector does not recognize its own Archive")
	}
	if Accepts(applePath) {
		t.Fatal("the Verve Connector accepted an Apple export")
	}
	if (applehealth.Connector{}).Accepts(archivePath) {
		t.Fatal("the Apple Connector accepted a Verve Archive")
	}
	if (googlehealth.Connector{}).Accepts(archivePath) {
		t.Fatal("the Google Connector accepted a Verve Archive")
	}
}

// Accepts is the Connector's detector, called as a function for the tests.
func Accepts(path string) bool { return Connector{}.Accepts(path) }

// writeArchive builds a zip from literal entries, for the malformed cases the
// writer cannot produce.
func writeArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	path := filepath.Join(t.TempDir(), "verve.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	return path
}

func line(s string) string { return s + "\n" }

// key builds a well-shaped content key from a seed, so a fixture reads as one
// without hashing anything real.
func key(seed string) string {
	return strings.Repeat(seed, 64)[:64]
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
