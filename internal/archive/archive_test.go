package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// exportedAt is pinned in every test: it and the build version are the only two
// fields of an Archive that are not a function of the data.
var exportedAt = time.Date(2026, 9, 12, 8, 30, 0, 0, time.UTC)

// newSeededSource builds a migrated database holding one Account with a row in
// every family, and returns a Source over it with its artifacts directory.
//
// The seeded data is deliberately awkward: two Measurements share a timestamp,
// one workout carries two Routes, and one of those Routes points at a file that
// is not on disk.
func newSeededSource(t *testing.T) (ModelSource, data.Models, int64) {
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
	models := data.NewModels(db)
	ctx := context.Background()

	dob, sex := "1990-04-01", "male"
	acc := &data.Account{Email: "owner@example.com", DateOfBirth: &dob, BiologicalSex: &sex}
	if err := models.Accounts.Insert(ctx, acc); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	if _, err := models.Measurements.InsertBatch(ctx, []data.Measurement{
		{AccountID: acc.ID, Metric: "steps", Value: 1200, OriginalUnit: "count",
			StartAt: "2026-03-01T08:00:00Z", EndAt: "2026-03-01T09:00:00Z", Source: "Watch", ContentKey: "m1"},
		{AccountID: acc.ID, Metric: "steps", Value: 900, OriginalUnit: "count",
			StartAt: "2026-03-01T08:00:00Z", EndAt: "2026-03-01T09:00:00Z", Source: "Phone", ContentKey: "m2"},
		{AccountID: acc.ID, Metric: "body_mass", Value: 71.5, OriginalUnit: "kg",
			StartAt: "2026-03-02T07:00:00Z", EndAt: "2026-03-02T07:00:00Z", Source: "Scale", ContentKey: "m3"},
	}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	if _, err := models.States.InsertStateBatch(ctx, []data.State{{
		AccountID: acc.ID, Kind: "sleep", StateValue: "asleep_rem",
		StartAt: "2026-03-01T23:00:00Z", EndAt: "2026-03-02T00:30:00Z", Source: "Watch", ContentKey: "s1",
	}}); err != nil {
		t.Fatalf("InsertStateBatch: %v", err)
	}

	distance, energy := 10.2, 620.0
	session := &data.Session{
		AccountID: acc.ID, ActivityType: "running",
		StartAt: "2026-03-03T06:00:00Z", EndAt: "2026-03-03T07:00:00Z",
		Duration: 3600, TotalDistance: &distance, TotalEnergy: &energy,
		Source: "Watch", ContentKey: "w1",
	}
	stats := []data.SessionStat{
		{Metric: "heart_rate", Stat: data.StatAverage, Value: 142},
		{Metric: "heart_rate", Stat: data.StatMax, Value: 171},
	}
	routes := []data.Route{
		{AccountID: acc.ID, Artifact: "present.gpx", StartAt: session.StartAt, EndAt: session.EndAt, Source: "Watch", ContentKey: "r1"},
		{AccountID: acc.ID, Artifact: "absent.gpx", StartAt: session.StartAt, EndAt: session.EndAt, Source: "Watch", ContentKey: "r2"},
	}
	if _, err := models.Sessions.InsertWorkout(ctx, session, stats, routes); err != nil {
		t.Fatalf("InsertWorkout: %v", err)
	}

	if _, err := models.Imports.InsertUnmappedBatch(ctx, []data.UnmappedRecord{{
		AccountID: acc.ID, SourceType: "HKQuantityTypeIdentifierUnknown", Value: "3", Unit: "count",
		StartAt: "2026-03-01T10:00:00Z", EndAt: "2026-03-01T10:00:00Z", Source: "Watch", ContentKey: "u1",
	}}); err != nil {
		t.Fatalf("InsertUnmappedBatch: %v", err)
	}
	if err := models.Imports.Record(ctx, &data.Import{
		AccountID: acc.ID, Connector: "applehealth", SourceFile: "export.zip",
		AddedCount: 3, SkippedCount: 0, UnmappedCount: 1,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	artifacts := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifacts, 0o750); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "present.gpx"), []byte("<gpx></gpx>"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	return NewModelSource(models, acc.ID, artifacts), models, acc.ID
}

// write runs one export and returns the bytes with the summary.
func write(t *testing.T, src Source) ([]byte, Summary) {
	t.Helper()
	var buf bytes.Buffer
	summary, err := Write(context.Background(), &buf, src, "test", exportedAt)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes(), summary
}

// entries reads an Archive into a name → contents map.
func entries(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
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

func lines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Two exports of the same rows are the same bytes. It is what makes a shelf of
// monthly Archives diffable, and it is the property the round-trip test rests
// on: without it, "the two Archives are identical" could not be asserted at all.
func TestArchiveIsDeterministic(t *testing.T) {
	src, _, _ := newSeededSource(t)

	first, _ := write(t, src)
	second, _ := write(t, src)

	if !bytes.Equal(first, second) {
		t.Fatalf("two exports of the same data differ: %d vs %d bytes", len(first), len(second))
	}
}

// Every family travels, including the Unmapped bin: it exists so nothing
// incoming is lost to a Catalog gap (ADR 0002), and an Archive without it would
// drop exactly what Verve promised to keep.
func TestArchiveHoldsEveryFamily(t *testing.T) {
	src, _, _ := newSeededSource(t)
	raw, summary := write(t, src)
	got := entries(t, raw)

	for _, name := range []string{ManifestName, MeasurementsName, StatesName, SessionsName, UnmappedName, ImportsName, "artifacts/present.gpx"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("archive is missing %s (has %v)", name, keys(got))
		}
	}

	var m Manifest
	if err := json.Unmarshal([]byte(got[ManifestName]), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if m.VerveArchive != Schema || m.Schema != Schema {
		t.Fatalf("manifest marker/schema = %d/%d, want %d", m.VerveArchive, m.Schema, Schema)
	}
	if m.ExportedAt != exportedAt.Format(time.RFC3339) || m.VerveVersion != "test" {
		t.Fatalf("manifest = %q %q, want the injected version and timestamp", m.VerveVersion, m.ExportedAt)
	}
	if m.Profile == nil || m.Profile.DateOfBirth != "1990-04-01" || m.Profile.BiologicalSex != "male" {
		t.Fatalf("manifest profile = %+v, want the seeded fields", m.Profile)
	}
	want := Counts{Measurements: 3, States: 1, Sessions: 1, SessionStats: 2, Routes: 2, Unmapped: 1, Imports: 1}
	if m.Counts != want {
		t.Fatalf("manifest counts = %+v, want %+v", m.Counts, want)
	}

	if n := len(lines(got[MeasurementsName])); n != 3 {
		t.Fatalf("measurements.ndjson holds %d lines, want 3", n)
	}
	if n := len(lines(got[UnmappedName])); n != 1 {
		t.Fatalf("unmapped.ndjson holds %d lines, want 1", n)
	}

	// A workout travels whole: its stats and Routes are nested under it, the way
	// the store writes them.
	var s Session
	if err := json.Unmarshal([]byte(lines(got[SessionsName])[0]), &s); err != nil {
		t.Fatalf("session line: %v", err)
	}
	if len(s.Stats) != 2 || len(s.Routes) != 2 {
		t.Fatalf("session carried %d stats and %d routes, want 2 and 2", len(s.Stats), len(s.Routes))
	}
	if s.ContentKey != "w1" || s.TotalDistance == nil {
		t.Fatalf("session = %+v, want the seeded workout with its distance", s)
	}

	// The content key travels, the row id does not.
	var measurement map[string]any
	if err := json.Unmarshal([]byte(lines(got[MeasurementsName])[0]), &measurement); err != nil {
		t.Fatalf("measurement line: %v", err)
	}
	if _, ok := measurement["id"]; ok {
		t.Fatal("a row id travelled in the Archive")
	}
	if measurement["content_key"] == "" {
		t.Fatal("the content key did not travel; nothing downstream can recompute it")
	}

	if summary.Artifacts != 1 {
		t.Fatalf("summary wrote %d artifacts, want 1", summary.Artifacts)
	}
}

// A Route whose GPX is gone from disk still travels as a row. The track is lost
// either way; dropping the Route with it would lose the fact that the workout
// had one, which is a second loss nobody asked for.
func TestArchiveWarnsAboutAMissingRouteFile(t *testing.T) {
	src, _, _ := newSeededSource(t)
	raw, summary := write(t, src)
	got := entries(t, raw)

	if len(summary.Warnings) != 1 || !strings.Contains(summary.Warnings[0], "absent.gpx") {
		t.Fatalf("warnings = %v, want one naming absent.gpx", summary.Warnings)
	}
	if _, ok := got["artifacts/absent.gpx"]; ok {
		t.Fatal("an artifact with no file behind it was written anyway")
	}

	var s Session
	if err := json.Unmarshal([]byte(lines(got[SessionsName])[0]), &s); err != nil {
		t.Fatalf("session line: %v", err)
	}
	if len(s.Routes) != 2 {
		t.Fatalf("the workout travelled with %d routes, want both", len(s.Routes))
	}
}

// Accounts never see each other's data (ADR 0007), and an export is the one read
// that touches every table at once, so it is the one worth asserting twice.
func TestArchiveIsScopedToOneAccount(t *testing.T) {
	src, models, _ := newSeededSource(t)
	ctx := context.Background()

	other := &data.Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(ctx, other); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	if _, err := models.Measurements.InsertBatch(ctx, []data.Measurement{{
		AccountID: other.ID, Metric: "steps", Value: 5, OriginalUnit: "count",
		StartAt: "2026-03-01T08:00:00Z", EndAt: "2026-03-01T09:00:00Z",
		Source: "Phone", ContentKey: "theirs",
	}}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if err := models.Imports.Record(ctx, &data.Import{
		AccountID: other.ID, Connector: "googlehealth", SourceFile: "takeout.zip",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	raw, _ := write(t, src)
	for name, body := range entries(t, raw) {
		if strings.Contains(body, "theirs") || strings.Contains(body, "takeout.zip") {
			t.Fatalf("%s carries another Account's data", name)
		}
	}
}

// The artifact name is the file's content hash, so two Routes over the same
// track are one entry, not two copies of the same bytes.
func TestArchiveWritesEachArtifactOnce(t *testing.T) {
	src, models, acc := newSeededSource(t)
	ctx := context.Background()

	second := &data.Session{
		AccountID: acc, ActivityType: "cycling",
		StartAt: "2026-03-04T06:00:00Z", EndAt: "2026-03-04T07:00:00Z",
		Duration: 3600, Source: "Watch", ContentKey: "w2",
	}
	routes := []data.Route{{
		AccountID: acc, Artifact: "present.gpx", StartAt: second.StartAt, EndAt: second.EndAt,
		Source: "Watch", ContentKey: "r3",
	}}
	if _, err := models.Sessions.InsertWorkout(ctx, second, nil, routes); err != nil {
		t.Fatalf("InsertWorkout: %v", err)
	}

	raw, summary := write(t, src)
	if summary.Artifacts != 1 {
		t.Fatalf("wrote %d artifacts for one shared track, want 1", summary.Artifacts)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	count := 0
	for _, f := range zr.File {
		if f.Name == "artifacts/present.gpx" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("artifacts/present.gpx appears %d times, want once", count)
	}
}

// An Account that never imported an Apple `Me` record carries no profile object
// rather than an empty one: the manifest says what is there.
func TestArchiveOmitsAnEmptyProfile(t *testing.T) {
	_, models, _ := newSeededSource(t)
	ctx := context.Background()
	other := &data.Account{Email: "bare@example.com"}
	if err := models.Accounts.Insert(ctx, other); err != nil {
		t.Fatalf("insert other: %v", err)
	}

	bare := NewModelSource(models, other.ID, t.TempDir())
	raw, _ := write(t, bare)
	body := entries(t, raw)[ManifestName]
	if strings.Contains(body, "profile") {
		t.Fatalf("manifest carries a profile for an Account with none:\n%s", body)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
