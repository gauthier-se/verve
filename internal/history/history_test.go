package history

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
	"github.com/gauthier-se/verve/internal/query"
)

func setup(t *testing.T) (Engine, data.Models, int64) {
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
		t.Fatalf("insert account: %v", err)
	}
	return Engine{Query: query.Engine{DB: db}, Models: models}, models, acc.ID
}

// seedMass writes body-mass readings on the given days, one per day.
func seedMass(t *testing.T, models data.Models, acc int64, days []string, value float64) {
	t.Helper()
	ms := make([]data.Measurement, len(days))
	for i, day := range days {
		ms[i] = data.Measurement{
			AccountID: acc, Metric: "body_mass", Value: value + float64(i), OriginalUnit: "kg",
			StartAt: day + "T08:00:00Z", EndAt: day + "T08:00:00Z", Source: "Scale",
			ContentKey: fmt.Sprintf("mass-%s", day),
		}
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), ms); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

func read(t *testing.T, e Engine, acc int64, metric string) Read {
	t.Helper()
	out, err := e.Read(context.Background(), acc, metric)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return out
}

func TestEmptyAccountHasNoBand(t *testing.T) {
	e, _, acc := setup(t)
	view := read(t, e, acc, DefaultMetric)
	if view.Band != nil {
		t.Errorf("band = %+v, want none before any data exists", view.Band)
	}
	if view.First != "" || view.Days != 0 {
		t.Errorf("span = %q…%q (%d days), want an empty span", view.First, view.Last, view.Days)
	}
	if len(view.Events) != 0 {
		t.Errorf("events = %+v, want none", view.Events)
	}
}

// TestFoldsPhasesOntoTheGrid locks that a Phase arrives as bucket keys the band
// actually draws — a span computed from dates by a second boundary rule would
// silently render nothing.
func TestFoldsPhasesOntoTheGrid(t *testing.T) {
	e, models, acc := setup(t)
	days := []string{}
	for d := 1; d <= 28; d++ {
		days = append(days, fmt.Sprintf("2024-01-%02d", d))
	}
	seedMass(t, models, acc, days, 80)

	if _, err := models.Phases.Open(context.Background(), acc, -0.5, "2024-01-10"); err != nil {
		t.Fatalf("open phase: %v", err)
	}

	view := read(t, e, acc, DefaultMetric)
	if view.Band == nil || len(view.Band.Phases) != 1 {
		t.Fatalf("phases = %+v, want one", view.Band)
	}
	phase := view.Band.Phases[0]
	if phase.Kind != "cut" {
		t.Errorf("kind = %q, want cut for a negative rate", phase.Kind)
	}
	if phase.From != "2024-01-10" {
		t.Errorf("phase from = %q, want the bucket holding its start day", phase.From)
	}
	// An open Phase runs to the last drawn bucket: "still cutting" has to be drawn
	// somewhere, and the end of the history is where it ends.
	if phase.To != "2024-01-28" || phase.EndedOn != nil {
		t.Errorf("phase to = %q (ended %v), want the last bucket and no end date", phase.To, phase.EndedOn)
	}
	keys := map[string]bool{}
	for _, p := range view.Band.Points {
		keys[p.Bucket] = true
	}
	if !keys[phase.From] || !keys[phase.To] {
		t.Errorf("phase span %q…%q is not on the drawn grid", phase.From, phase.To)
	}
}

// TestEventsGatherEveryDatedSource locks that the ledger carries each kind once,
// newest first.
func TestEventsGatherEveryDatedSource(t *testing.T) {
	e, models, acc := setup(t)
	seedMass(t, models, acc, []string{"2024-01-01", "2024-02-01"}, 80)

	ctx := context.Background()
	if _, err := models.Phases.Open(ctx, acc, 0.25, "2024-01-20"); err != nil {
		t.Fatalf("open phase: %v", err)
	}
	note := &data.Annotation{AccountID: acc, Label: "Flu", StartsOn: "2024-01-15"}
	if err := models.Annotations.Insert(ctx, note); err != nil {
		t.Fatalf("insert annotation: %v", err)
	}
	imp := &data.Import{AccountID: acc, Connector: "googlehealth", SourceFile: "takeout.zip", AddedCount: 412, SkippedCount: 9, UnmappedCount: 14}
	if err := models.Imports.Record(ctx, imp); err != nil {
		t.Fatalf("record import: %v", err)
	}

	view := read(t, e, acc, DefaultMetric)
	seen := map[string]Event{}
	for _, ev := range view.Events {
		seen[ev.Kind] = ev
	}
	for _, kind := range []string{KindImport, KindPhase, KindNote, KindSource, KindOrigin} {
		if _, ok := seen[kind]; !ok {
			t.Errorf("no %s event in %+v", kind, view.Events)
		}
	}
	if seen[KindPhase].Label != "bulk" || seen[KindPhase].Rate == nil {
		t.Errorf("phase event = %+v, want a bulk carrying its rate", seen[KindPhase])
	}
	if seen[KindSource].Label != "Scale" {
		t.Errorf("source event = %+v, want the source name", seen[KindSource])
	}
	if seen[KindOrigin].Date != "2024-01-01" {
		t.Errorf("origin date = %q, want the earliest measurement", seen[KindOrigin].Date)
	}
	if len(seen[KindImport].Figures) != 3 {
		t.Errorf("import figures = %+v, want added/skipped/unmapped", seen[KindImport].Figures)
	}
	// With two Connectors, a file name no longer says where a history came from, and
	// saying so is what this page is for.
	if seen[KindImport].Label != "takeout.zip · Google Health" {
		t.Errorf("import label = %q, want the file and the source that read it", seen[KindImport].Label)
	}
	// The import happened now; the note in January. Newest first.
	if view.Events[0].Kind != KindImport {
		t.Errorf("first event = %q, want the most recent one", view.Events[0].Kind)
	}
	for i := 1; i < len(view.Events); i++ {
		if view.Events[i-1].Date < view.Events[i].Date {
			t.Fatalf("events are not newest-first around %d: %+v", i, view.Events)
		}
	}
}

// TestReadsAnyCatalogMetric locks that the band is not hard-wired to body mass:
// the default is a default, not the only answer.
func TestReadsAnyCatalogMetric(t *testing.T) {
	e, models, acc := setup(t)
	if _, err := models.Measurements.InsertBatch(context.Background(), []data.Measurement{
		{AccountID: acc, Metric: "steps", Value: 9000, OriginalUnit: "count",
			StartAt: "2024-03-01T08:00:00Z", EndAt: "2024-03-01T08:00:00Z", Source: "Watch", ContentKey: "s1"},
		{AccountID: acc, Metric: "steps", Value: 11000, OriginalUnit: "count",
			StartAt: "2024-03-05T08:00:00Z", EndAt: "2024-03-05T08:00:00Z", Source: "Watch", ContentKey: "s2"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	view := read(t, e, acc, "steps")
	if view.Band == nil || view.Band.Metric != "steps" || view.Band.Unit != "count" {
		t.Fatalf("band = %+v, want the steps band", view.Band)
	}
	if len(view.Band.Points) != 5 { // Mar 1 → Mar 5
		t.Errorf("points = %d, want 5", len(view.Band.Points))
	}
}

// TestImportEventIsDatedToday guards the date projection: an Import is stamped
// RFC 3339 and has to reach the ledger as a plain day.
func TestImportEventIsDatedToday(t *testing.T) {
	e, models, acc := setup(t)
	imp := &data.Import{AccountID: acc, SourceFile: "export.zip"}
	if err := models.Imports.Record(context.Background(), imp); err != nil {
		t.Fatalf("record import: %v", err)
	}

	view := read(t, e, acc, DefaultMetric)
	if len(view.Events) != 1 {
		t.Fatalf("events = %+v, want the import alone", view.Events)
	}
	if want := time.Now().UTC().Format(dayLayout); view.Events[0].Date != want {
		t.Errorf("import date = %q, want %q", view.Events[0].Date, want)
	}
}

// An Exclusion is dated to the day it was decided, carries the span it is about in
// its own fields, and reports what it removed. The History exists to explain the
// shape of the data (ADR 0032), and this is the one gap Verve itself made.
func TestCarriesTheExclusionEvent(t *testing.T) {
	e, models, acc := setup(t)
	seedMass(t, models, acc, []string{"2024-01-01", "2024-02-01"}, 80)

	ctx := context.Background()
	x := &data.Exclusion{AccountID: acc, Metric: "body_mass", StartsOn: "2024-02-01"}
	if _, err := models.Exclusions.Insert(ctx, x); err != nil {
		t.Fatalf("insert exclusion: %v", err)
	}

	view := read(t, e, acc, DefaultMetric)
	var event *Event
	for i, ev := range view.Events {
		if ev.Kind == KindExclusion {
			event = &view.Events[i]
			break
		}
	}
	if event == nil {
		t.Fatalf("no exclusion event in %+v", view.Events)
	}
	if event.Label != "body_mass" {
		t.Errorf("label = %q, want the Metric slug", event.Label)
	}
	if event.Date != time.Now().UTC().Format(dayLayout) {
		t.Errorf("date = %q, want today: it is dated when it was decided", event.Date)
	}
	if event.SpanFrom != "2024-02-01" || event.SpanTo != "" {
		t.Errorf("span = %q..%q, want an open-ended span from the excluded day", event.SpanFrom, event.SpanTo)
	}
	// EndsOn draws a band from Date across the axis; the span is not that.
	if event.EndsOn != nil {
		t.Errorf("ends_on = %v, want nil: an exclusion is a point event", *event.EndsOn)
	}
	if len(event.Figures) != 1 || event.Figures[0].Key != "purged" || event.Figures[0].Value != 1 {
		t.Errorf("figures = %+v, want one purged=1 chip", event.Figures)
	}

	// Deleting the rule takes its event with it: the History reads current state.
	if err := models.Exclusions.Delete(ctx, acc, x.ID); err != nil {
		t.Fatalf("delete exclusion: %v", err)
	}
	for _, ev := range read(t, e, acc, DefaultMetric).Events {
		if ev.Kind == KindExclusion {
			t.Errorf("exclusion event survives its rule: %+v", ev)
		}
	}
}
