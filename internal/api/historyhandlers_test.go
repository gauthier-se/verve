package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gauthier-se/verve/internal/data"
)

// getHistory is a test helper reading the History page for the session.
func getHistory(t *testing.T, srv *Server, cookie *http.Cookie, target string) historyView {
	t.Helper()
	res, body := do(t, srv, target, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("history status = %d, want 200 (%s)", res.StatusCode, body["error"])
	}
	var view historyView
	if err := json.Unmarshal(body["history"], &view); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	return view
}

// seedMass writes body-mass readings on the given days, one per day.
func seedMass(t *testing.T, models data.Models, email string, days []string, value float64) {
	t.Helper()
	ms := make([]data.Measurement, len(days))
	for i, day := range days {
		ms[i] = data.Measurement{
			Metric: "body_mass", Value: value + float64(i), OriginalUnit: "kg",
			StartAt: day + "T08:00:00Z", EndAt: day + "T08:00:00Z", Source: "Scale",
			ContentKey: fmt.Sprintf("mass-%s", day),
		}
	}
	seedSteps(t, models, email, ms)
}

func TestHistoryRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, _ := do(t, srv, "/v1/history")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated history status = %d, want 401", res.StatusCode)
	}
}

// TestHistoryEmptyAccountHasNoBand locks that an Account with no data gets a page
// rather than an error, and that no window is invented for it.
func TestHistoryEmptyAccountHasNoBand(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	view := getHistory(t, srv, cookie, "/v1/history")
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

// TestHistoryBandIsDenseAndNamesItsGaps is the point of the band: the grid is
// materialised, so an empty stretch is a bucket that says it is empty rather than an
// absence the client would have to infer boundaries to notice.
func TestHistoryBandIsDenseAndNamesItsGaps(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	// Two clusters a fortnight apart, inside one month, so the band comes out daily.
	seedMass(t, models, testEmail, []string{
		"2024-01-01", "2024-01-02", "2024-01-03",
		"2024-01-14", "2024-01-15",
	}, 80)

	view := getHistory(t, srv, cookie, "/v1/history")
	if view.Band == nil {
		t.Fatal("band = nil, want the long view")
	}
	if view.Band.Bucket != "day" {
		t.Errorf("bucket = %q, want day for a fortnight of history", view.Band.Bucket)
	}
	if got := len(view.Band.Points); got != 15 { // Jan 1 → Jan 15 inclusive
		t.Errorf("points = %d, want 15 — one per bucket, gaps included", got)
	}
	gaps := 0
	for _, p := range view.Band.Points {
		if p.Gap {
			gaps++
		}
	}
	if gaps != 10 { // Jan 4 → Jan 13
		t.Errorf("gap buckets = %d, want 10", gaps)
	}
	if len(view.Band.Gaps) != 1 {
		t.Fatalf("gap runs = %+v, want one", view.Band.Gaps)
	}
	if view.Band.Gaps[0].From != "2024-01-04" || view.Band.Gaps[0].To != "2024-01-13" {
		t.Errorf("gap run = %+v, want 2024-01-04 → 2024-01-13", view.Band.Gaps[0])
	}
	if view.First != "2024-01-01" || view.Last != "2024-01-15" {
		t.Errorf("span = %q…%q, want the real extent of the data", view.First, view.Last)
	}
}

// TestHistoryFoldsPhasesOntoTheGrid locks that a Phase arrives as bucket keys the
// band actually draws — a span computed from dates by a second boundary rule would
// silently render nothing.
func TestHistoryFoldsPhasesOntoTheGrid(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	days := []string{}
	for d := 1; d <= 28; d++ {
		days = append(days, fmt.Sprintf("2024-01-%02d", d))
	}
	seedMass(t, models, testEmail, days, 80)

	acc, err := models.Accounts.GetByEmail(context.Background(), testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if _, err := models.Phases.Open(context.Background(), acc.ID, -0.5, "2024-01-10"); err != nil {
		t.Fatalf("open phase: %v", err)
	}

	view := getHistory(t, srv, cookie, "/v1/history")
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

// TestHistoryEventsGatherEveryDatedSource locks that the ledger carries each kind
// once, newest first.
func TestHistoryEventsGatherEveryDatedSource(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedMass(t, models, testEmail, []string{"2024-01-01", "2024-02-01"}, 80)

	ctx := context.Background()
	acc, err := models.Accounts.GetByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if _, err := models.Phases.Open(ctx, acc.ID, 0.25, "2024-01-20"); err != nil {
		t.Fatalf("open phase: %v", err)
	}
	note := &data.Annotation{AccountID: acc.ID, Label: "Flu", StartsOn: "2024-01-15"}
	if err := models.Annotations.Insert(ctx, note); err != nil {
		t.Fatalf("insert annotation: %v", err)
	}
	imp := &data.Import{AccountID: acc.ID, SourceFile: "export.zip", AddedCount: 412, SkippedCount: 9, UnmappedCount: 14}
	if err := models.Measurements.RecordImport(ctx, imp); err != nil {
		t.Fatalf("record import: %v", err)
	}

	view := getHistory(t, srv, cookie, "/v1/history")
	seen := map[string]historyEventView{}
	for _, e := range view.Events {
		seen[e.Kind] = e
	}
	for _, kind := range []string{eventImport, eventPhase, eventNote, eventSource, eventOrigin} {
		if _, ok := seen[kind]; !ok {
			t.Errorf("no %s event in %+v", kind, view.Events)
		}
	}
	if seen[eventPhase].Label != "bulk" || seen[eventPhase].Rate == nil {
		t.Errorf("phase event = %+v, want a bulk carrying its rate", seen[eventPhase])
	}
	if seen[eventSource].Label != "Scale" {
		t.Errorf("source event = %+v, want the source name", seen[eventSource])
	}
	if seen[eventOrigin].Date != "2024-01-01" {
		t.Errorf("origin date = %q, want the earliest measurement", seen[eventOrigin].Date)
	}
	if len(seen[eventImport].Figures) != 3 {
		t.Errorf("import figures = %+v, want added/skipped/unmapped", seen[eventImport].Figures)
	}
	// The import happened now; the note in January. Newest first.
	if view.Events[0].Kind != eventImport {
		t.Errorf("first event = %q, want the most recent one", view.Events[0].Kind)
	}
	for i := 1; i < len(view.Events); i++ {
		if view.Events[i-1].Date < view.Events[i].Date {
			t.Fatalf("events are not newest-first around %d: %+v", i, view.Events)
		}
	}
}

func TestHistoryRejectsUnknownMetric(t *testing.T) {
	srv, _, cookie := newTestServer(t)
	res, _ := do(t, srv, "/v1/history?metric=not_a_metric", cookie)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown metric status = %d, want 422", res.StatusCode)
	}
}

// TestHistoryReadsAnyCatalogMetric locks that the band is not hard-wired to body
// mass: the default is a default, not the only answer.
func TestHistoryReadsAnyCatalogMetric(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	seedSteps(t, models, testEmail, []data.Measurement{
		{Metric: "steps", Value: 9000, OriginalUnit: "count",
			StartAt: "2024-03-01T08:00:00Z", EndAt: "2024-03-01T08:00:00Z", Source: "Watch", ContentKey: "s1"},
		{Metric: "steps", Value: 11000, OriginalUnit: "count",
			StartAt: "2024-03-05T08:00:00Z", EndAt: "2024-03-05T08:00:00Z", Source: "Watch", ContentKey: "s2"},
	})

	view := getHistory(t, srv, cookie, "/v1/history?metric=steps")
	if view.Band == nil || view.Band.Metric != "steps" || view.Band.Unit != "count" {
		t.Fatalf("band = %+v, want the steps band", view.Band)
	}
	if len(view.Band.Points) != 5 { // Mar 1 → Mar 5
		t.Errorf("points = %d, want 5", len(view.Band.Points))
	}
}

// TestHistoryImportEventIsDatedToday guards the date projection: an Import is
// stamped RFC 3339 and has to reach the ledger as a plain day.
func TestHistoryImportEventIsDatedToday(t *testing.T) {
	srv, models, cookie := newTestServer(t)
	ctx := context.Background()
	acc, err := models.Accounts.GetByEmail(ctx, testEmail)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	imp := &data.Import{AccountID: acc.ID, SourceFile: "export.zip"}
	if err := models.Measurements.RecordImport(ctx, imp); err != nil {
		t.Fatalf("record import: %v", err)
	}

	view := getHistory(t, srv, cookie, "/v1/history")
	if len(view.Events) != 1 {
		t.Fatalf("events = %+v, want the import alone", view.Events)
	}
	if want := time.Now().UTC().Format(dayLayout); view.Events[0].Date != want {
		t.Errorf("import date = %q, want %q", view.Events[0].Date, want)
	}
}
