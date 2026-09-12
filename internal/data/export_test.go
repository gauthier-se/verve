package data

import (
	"context"
	"fmt"
	"testing"
)

// seedExportMeasurements inserts n Measurements of one Metric, one per minute,
// with distinct content keys.
func seedExportMeasurements(t *testing.T, models Models, acc int64, metric string, n int) {
	t.Helper()
	batch := make([]Measurement, 0, n)
	for i := 0; i < n; i++ {
		batch = append(batch, Measurement{
			AccountID: acc, Metric: metric, Value: float64(i), OriginalUnit: "count",
			StartAt:    fmt.Sprintf("2024-01-01T%02d:%02d:00Z", i/60, i%60),
			EndAt:      fmt.Sprintf("2024-01-01T%02d:%02d:00Z", i/60, i%60),
			Source:     "Watch",
			ContentKey: fmt.Sprintf("%s-%04d", metric, i),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), batch); err != nil {
		t.Fatalf("seed measurements: %v", err)
	}
}

// drainMeasurements pages the whole family with the given page size, the way the
// Archive writer does.
func drainMeasurements(t *testing.T, models Models, acc int64, limit int) []Measurement {
	t.Helper()
	var out []Measurement
	var cursor MeasurementCursor
	for {
		page, after, err := models.Measurements.ExportPage(context.Background(), acc, cursor, limit)
		if err != nil {
			t.Fatalf("ExportPage: %v", err)
		}
		if len(page) == 0 {
			return out
		}
		out = append(out, page...)
		cursor = after
		if len(out) > 10_000 {
			t.Fatal("ExportPage did not terminate")
		}
	}
}

// Paging has to be total and ordered, because the Archive's every property
// depends on it: a repeated row duplicates data on the way back in, a skipped
// one loses it silently, and an unstable order breaks the byte-identity that
// makes two Archives comparable.
func TestExportPagingIsTotalAndOrdered(t *testing.T) {
	_, models := openTestDB(t)
	acc := seedAccount(t, models)
	seedExportMeasurements(t, models, acc, "steps", 125)
	seedExportMeasurements(t, models, acc, "heart_rate", 125)

	got := drainMeasurements(t, models, acc, 7)
	if len(got) != 250 {
		t.Fatalf("drained %d rows, want 250", len(got))
	}

	seen := make(map[string]bool, len(got))
	for i, m := range got {
		if seen[m.ContentKey] {
			t.Fatalf("row %d (%s) came back twice", i, m.ContentKey)
		}
		seen[m.ContentKey] = true
		if i == 0 {
			continue
		}
		prev := got[i-1]
		if key(prev) >= key(m) {
			t.Fatalf("row %d is out of order: %q then %q", i, key(prev), key(m))
		}
	}
	// heart_rate sorts before steps, and the order is by Metric first: the
	// export order is the index's, not the clock's.
	if got[0].Metric != "heart_rate" || got[len(got)-1].Metric != "steps" {
		t.Fatalf("first/last metric = %q/%q, want heart_rate/steps", got[0].Metric, got[len(got)-1].Metric)
	}
}

func key(m Measurement) string { return m.Metric + "\x1f" + m.StartAt + "\x1f" + m.ContentKey }

// A page boundary landing inside a run of rows sharing (metric, start_at) is the
// case the content_key tiebreak exists for: without it the cursor cannot
// advance, and the export either loops or drops the rest of the run.
func TestExportPagingAdvancesWithinOneTimestamp(t *testing.T) {
	_, models := openTestDB(t)
	acc := seedAccount(t, models)

	const n = 10
	batch := make([]Measurement, 0, n)
	for i := 0; i < n; i++ {
		batch = append(batch, Measurement{
			AccountID: acc, Metric: "steps", Value: float64(i), OriginalUnit: "count",
			StartAt: "2024-03-01T08:00:00Z", EndAt: "2024-03-01T08:00:00Z",
			Source: "Watch", ContentKey: fmt.Sprintf("same-%02d", i),
		})
	}
	if _, err := models.Measurements.InsertBatch(context.Background(), batch); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	got := drainMeasurements(t, models, acc, 3)
	if len(got) != n {
		t.Fatalf("drained %d rows of one timestamp, want %d", len(got), n)
	}
}

func TestExportPagesAreScopedToOneAccount(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)
	other := &Account{Email: "other@example.com"}
	if err := models.Accounts.Insert(ctx, other); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	seedExportMeasurements(t, models, acc, "steps", 3)

	batch := []Measurement{{
		AccountID: other.ID, Metric: "steps", Value: 9, OriginalUnit: "count",
		StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T00:00:00Z",
		Source: "Phone", ContentKey: "theirs",
	}}
	if _, err := models.Measurements.InsertBatch(ctx, batch); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	for _, m := range drainMeasurements(t, models, acc, 2) {
		if m.ContentKey == "theirs" {
			t.Fatal("another Account's row came out of this Account's export")
		}
	}
}

// A workout travels whole or not at all: the stats and Routes have to come out
// attached to the Session they belong to, including when a page holds several.
func TestExportSessionCarriesItsStatsAndRoutes(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)

	distance := 10.5
	for i := 0; i < 3; i++ {
		s := &Session{
			AccountID: acc, ActivityType: "running",
			StartAt: fmt.Sprintf("2024-05-0%dT06:00:00Z", i+1), EndAt: fmt.Sprintf("2024-05-0%dT07:00:00Z", i+1),
			Duration: 3600, TotalDistance: &distance, Source: "Watch",
			ContentKey: fmt.Sprintf("session-%d", i),
		}
		stats := []SessionStat{
			{Metric: "heart_rate", Stat: StatAverage, Value: 140},
			{Metric: "heart_rate", Stat: StatMax, Value: 170},
		}
		routes := []Route{{
			AccountID: acc, Artifact: fmt.Sprintf("%d.gpx", i),
			StartAt: s.StartAt, EndAt: s.EndAt, Source: "Watch",
			ContentKey: fmt.Sprintf("route-%d", i),
		}}
		if _, err := models.Sessions.InsertWorkout(ctx, s, stats, routes); err != nil {
			t.Fatalf("InsertWorkout: %v", err)
		}
	}

	page, _, err := models.Sessions.ExportPage(ctx, acc, SessionCursor{}, 2)
	if err != nil {
		t.Fatalf("ExportPage: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("page held %d sessions, want 2", len(page))
	}
	for _, es := range page {
		if len(es.Stats) != 2 {
			t.Fatalf("session %s carried %d stats, want 2", es.Session.ContentKey, len(es.Stats))
		}
		if len(es.Routes) != 1 {
			t.Fatalf("session %s carried %d routes, want 1", es.Session.ContentKey, len(es.Routes))
		}
		if es.Stats[0].Stat != StatAverage || es.Stats[1].Stat != StatMax {
			t.Fatalf("stats came back unordered: %+v", es.Stats)
		}
	}

	_, cursor, err := models.Sessions.ExportPage(ctx, acc, SessionCursor{}, 2)
	if err != nil {
		t.Fatalf("ExportPage: %v", err)
	}
	rest, _, err := models.Sessions.ExportPage(ctx, acc, cursor, 2)
	if err != nil {
		t.Fatalf("ExportPage rest: %v", err)
	}
	if len(rest) != 1 || rest[0].Session.ContentKey != "session-2" {
		t.Fatalf("second page = %d sessions, want the last one", len(rest))
	}
}

func TestExportCountsEveryFamily(t *testing.T) {
	_, models := openTestDB(t)
	ctx := context.Background()
	acc := seedAccount(t, models)
	seedExportMeasurements(t, models, acc, "steps", 4)

	if _, err := models.States.InsertStateBatch(ctx, []State{{
		AccountID: acc, Kind: "sleep", StateValue: "asleep_rem",
		StartAt: "2024-01-01T23:00:00Z", EndAt: "2024-01-02T00:00:00Z",
		Source: "Watch", ContentKey: "state-1",
	}}); err != nil {
		t.Fatalf("InsertStateBatch: %v", err)
	}
	s := &Session{
		AccountID: acc, ActivityType: "running", StartAt: "2024-05-01T06:00:00Z",
		EndAt: "2024-05-01T07:00:00Z", Duration: 3600, Source: "Watch", ContentKey: "s1",
	}
	stats := []SessionStat{{Metric: "heart_rate", Stat: StatAverage, Value: 140}}
	routes := []Route{{AccountID: acc, Artifact: "a.gpx", StartAt: s.StartAt, EndAt: s.EndAt, Source: "Watch", ContentKey: "r1"}}
	if _, err := models.Sessions.InsertWorkout(ctx, s, stats, routes); err != nil {
		t.Fatalf("InsertWorkout: %v", err)
	}
	if _, err := models.Imports.InsertUnmappedBatch(ctx, []UnmappedRecord{{
		AccountID: acc, SourceType: "HKQuantityTypeIdentifierSomething", Value: "1", Unit: "count",
		StartAt: "2024-01-01T00:00:00Z", EndAt: "2024-01-01T00:00:00Z", Source: "Watch", ContentKey: "u1",
	}}); err != nil {
		t.Fatalf("InsertUnmappedBatch: %v", err)
	}
	if err := models.Imports.Record(ctx, &Import{
		AccountID: acc, Connector: "applehealth", SourceFile: "export.zip", AddedCount: 4,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := models.ExportCounts(ctx, acc)
	if err != nil {
		t.Fatalf("ExportCounts: %v", err)
	}
	want := ExportCounts{Measurements: 4, States: 1, Sessions: 1, SessionStats: 1, Routes: 1, Unmapped: 1, Imports: 1}
	if got != want {
		t.Fatalf("ExportCounts = %+v, want %+v", got, want)
	}
}
