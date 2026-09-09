package connector_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gauthier-se/verve/internal/connector"
	"github.com/gauthier-se/verve/internal/data"
)

// fakeStore records what a Sink writes without a database, so the rules the Sink
// owns can be asserted directly: what it refuses, when it flushes, what it tallies.
type fakeStore struct {
	measurements [][]data.Measurement
	unmapped     [][]data.UnmappedRecord
	states       [][]data.State
	sessions     []data.Session
	stats        map[int64][]data.SessionStat
	routes       []data.Route
	imports      []data.Import

	// known content keys, so a second offer of the same row reports as skipped.
	seen    map[string]bool
	nextID  int64
	failOn  string
	failErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{seen: map[string]bool{}, stats: map[int64][]data.SessionStat{}, nextID: 1}
}

// mask marks each key new the first time it is seen, like the real dedup.
func (f *fakeStore) mask(keys []string) []bool {
	out := make([]bool, len(keys))
	for i, k := range keys {
		if !f.seen[k] {
			f.seen[k] = true
			out[i] = true
		}
	}
	return out
}

func (f *fakeStore) InsertBatch(_ context.Context, ms []data.Measurement) ([]bool, error) {
	if f.failOn == "measurements" {
		return nil, f.failErr
	}
	f.measurements = append(f.measurements, append([]data.Measurement(nil), ms...))
	keys := make([]string, len(ms))
	for i, m := range ms {
		keys[i] = m.ContentKey
	}
	return f.mask(keys), nil
}

func (f *fakeStore) InsertUnmappedBatch(_ context.Context, us []data.UnmappedRecord) ([]bool, error) {
	f.unmapped = append(f.unmapped, append([]data.UnmappedRecord(nil), us...))
	keys := make([]string, len(us))
	for i, u := range us {
		keys[i] = u.ContentKey
	}
	return f.mask(keys), nil
}

func (f *fakeStore) InsertStateBatch(_ context.Context, ss []data.State) ([]bool, error) {
	f.states = append(f.states, append([]data.State(nil), ss...))
	keys := make([]string, len(ss))
	for i, s := range ss {
		keys[i] = s.ContentKey
	}
	return f.mask(keys), nil
}

func (f *fakeStore) InsertWorkout(_ context.Context, s *data.Session, stats []data.SessionStat, routes []data.Route) (data.WorkoutWrite, error) {
	out := data.WorkoutWrite{RoutesAdded: make([]bool, len(routes))}

	out.SessionAdded = !f.seen[s.ContentKey]
	f.seen[s.ContentKey] = true
	s.ID = f.nextID
	f.nextID++
	f.sessions = append(f.sessions, *s)

	f.stats[s.ID] = append(f.stats[s.ID], stats...)

	for i := range routes {
		routes[i].SessionID = s.ID
		out.RoutesAdded[i] = !f.seen[routes[i].ContentKey]
		f.seen[routes[i].ContentKey] = true
		f.routes = append(f.routes, routes[i])
	}
	return out, nil
}

func (f *fakeStore) Record(_ context.Context, imp *data.Import) error {
	f.imports = append(f.imports, *imp)
	return nil
}

func measurement(metric, at string, value float64) data.Measurement {
	return data.Measurement{
		Metric: metric, Value: value, OriginalUnit: "u",
		StartAt: at, EndAt: at, Source: "Watch",
		ContentKey: metric + "-" + at + "-" + strconv.FormatFloat(value, 'f', -1, 64),
	}
}

// TestSinkRefusesAnExclusionAndCountsIt is the rule each Connector used to carry
// its own copy of: what an Exclusion names is never written, and is reported
// rather than swallowed (ADR 0033).
func TestSinkRefusesAnExclusionAndCountsIt(t *testing.T) {
	store := newFakeStore()
	sink := connector.NewSink(store, 7, "test", "export.zip", connector.Options{
		// An unbounded Exclusion: the whole Metric, whatever the day.
		Exclusions: data.ExclusionSet{"body_fat_percentage": {{Metric: "body_fat_percentage"}}},
	})
	ctx := context.Background()

	for _, m := range []data.Measurement{
		measurement("steps", "2024-01-01T08:00:00Z", 900),
		measurement("body_fat_percentage", "2024-01-01T08:00:00Z", 0.21),
		measurement("steps", "2024-01-02T08:00:00Z", 1100),
	} {
		if err := sink.Measurement(ctx, m); err != nil {
			t.Fatalf("Measurement: %v", err)
		}
	}

	report, err := sink.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if report.Excluded != 1 || report.PerMetric["body_fat_percentage"].Excluded != 1 {
		t.Errorf("excluded = %d (per-metric %+v), want 1", report.Excluded, report.PerMetric["body_fat_percentage"])
	}
	if report.Added != 2 {
		t.Errorf("added = %d, want the two rows that were not refused", report.Added)
	}
	// The refused row must not reach the store at all, and must not be diverted into
	// the Unmapped bin, which would keep exactly what was asked to be dropped.
	for _, batch := range store.measurements {
		for _, m := range batch {
			if m.Metric == "body_fat_percentage" {
				t.Errorf("an excluded row was written: %+v", m)
			}
		}
	}
	if len(store.unmapped) != 0 {
		t.Errorf("unmapped batches = %+v, want none: a refusal is not an unmapped record", store.unmapped)
	}
}

// TestSinkFlushesAtBatchSize pins the bound on memory and on the WAL that every
// Connector used to assert for itself with its own length check.
func TestSinkFlushesAtBatchSize(t *testing.T) {
	store := newFakeStore()
	sink := connector.NewSink(store, 7, "test", "export.zip", connector.Options{})
	ctx := context.Background()

	for i := 0; i < connector.BatchSize+3; i++ {
		if err := sink.Measurement(ctx, measurement("steps", "2024-01-01T08:00:00Z", float64(i))); err != nil {
			t.Fatalf("Measurement %d: %v", i, err)
		}
	}
	// One full batch has gone out; the remainder is still pending.
	if len(store.measurements) != 1 || len(store.measurements[0]) != connector.BatchSize {
		t.Fatalf("batches before Close = %d, want one of %d", len(store.measurements), connector.BatchSize)
	}

	report, err := sink.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(store.measurements) != 2 || len(store.measurements[1]) != 3 {
		t.Errorf("batches after Close = %d, want the remainder flushed", len(store.measurements))
	}
	if report.Added != connector.BatchSize+3 {
		t.Errorf("added = %d, want every row", report.Added)
	}
}

// TestSinkTalliesAddedAndSkipped: a re-import adds nothing and says so, which is
// what proves idempotence (ADR 0006).
func TestSinkTalliesAddedAndSkipped(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()
	row := measurement("steps", "2024-01-01T08:00:00Z", 900)

	first := connector.NewSink(store, 7, "test", "export.zip", connector.Options{})
	if err := first.Measurement(ctx, row); err != nil {
		t.Fatalf("Measurement: %v", err)
	}
	if _, err := first.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := connector.NewSink(store, 7, "test", "export.zip", connector.Options{})
	if err := second.Measurement(ctx, row); err != nil {
		t.Fatalf("Measurement: %v", err)
	}
	report, err := second.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if report.Added != 0 || report.Skipped != 1 {
		t.Errorf("re-import = %d added / %d skipped, want 0/1", report.Added, report.Skipped)
	}
	if got := report.PerMetric["steps"]; got.Skipped != 1 {
		t.Errorf("per-metric tally = %+v, want one skip", got)
	}
}

// TestSinkScopesRowsToTheAccount: a Connector building a row need not carry the
// Account, and cannot write one belonging to another (ADR 0007).
func TestSinkScopesRowsToTheAccount(t *testing.T) {
	store := newFakeStore()
	sink := connector.NewSink(store, 42, "test", "export.zip", connector.Options{})
	ctx := context.Background()

	if err := sink.Measurement(ctx, measurement("steps", "2024-01-01T08:00:00Z", 900)); err != nil {
		t.Fatalf("Measurement: %v", err)
	}
	if err := sink.State(ctx, data.State{Kind: "sleep", StateValue: "asleep_core", ContentKey: "s1"}); err != nil {
		t.Fatalf("State: %v", err)
	}
	if _, err := sink.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.measurements[0][0].AccountID; got != 42 {
		t.Errorf("measurement account = %d, want 42", got)
	}
	if got := store.states[0][0].AccountID; got != 42 {
		t.Errorf("state account = %d, want 42", got)
	}
}

// TestSinkRecordsTheImportLast: the Import row is the receipt for the rows that
// were written, so it carries their counts and is written after them.
func TestSinkRecordsTheImportLast(t *testing.T) {
	store := newFakeStore()
	sink := connector.NewSink(store, 7, "applehealth", "export.zip", connector.Options{})
	ctx := context.Background()

	if err := sink.Measurement(ctx, measurement("steps", "2024-01-01T08:00:00Z", 900)); err != nil {
		t.Fatalf("Measurement: %v", err)
	}
	if err := sink.Unmapped(ctx, data.UnmappedRecord{SourceType: "weird", ContentKey: "u1"}); err != nil {
		t.Fatalf("Unmapped: %v", err)
	}
	if len(store.imports) != 0 {
		t.Fatal("the Import was recorded before the rows it summarises")
	}

	report, err := sink.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(store.imports) != 1 {
		t.Fatalf("imports = %d, want one", len(store.imports))
	}
	imp := store.imports[0]
	if imp.Connector != "applehealth" || imp.SourceFile != "export.zip" {
		t.Errorf("import = %+v, want the connector and file that ran", imp)
	}
	if imp.AddedCount != 1 || imp.UnmappedCount != 1 {
		t.Errorf("import counts = %+v, want 1 added / 1 unmapped", imp)
	}
	if report.UnmappedTypes["weird"] != 1 {
		t.Errorf("unmapped types = %+v, want the raw type counted", report.UnmappedTypes)
	}
}

// TestSinkSurfacesAWriteFailure: a store error is returned rather than tallied, so
// an import that cannot write fails loudly instead of reporting a partial success.
func TestSinkSurfacesAWriteFailure(t *testing.T) {
	store := newFakeStore()
	store.failOn = "measurements"
	store.failErr = errors.New("disk full")
	sink := connector.NewSink(store, 7, "test", "export.zip", connector.Options{})

	if err := sink.Measurement(context.Background(), measurement("steps", "2024-01-01T08:00:00Z", 900)); err != nil {
		t.Fatalf("Measurement queued, so it should not fail yet: %v", err)
	}
	_, err := sink.Close(context.Background())
	if !errors.Is(err, store.failErr) {
		t.Errorf("Close error = %v, want the store's failure", err)
	}
	if len(store.imports) != 0 {
		t.Error("an Import was recorded for rows that were never written")
	}
}

// TestSinkAttachesSessionStatsOnAReimport: stats are attached whether or not the
// Session is new, which is what makes a re-import converge rather than leave every
// existing database stat-less forever.
func TestSinkAttachesSessionStatsOnAReimport(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()
	stats := []data.SessionStat{{Metric: "heart_rate", Stat: "average", Value: 142}}

	for i := 0; i < 2; i++ {
		sink := connector.NewSink(store, 7, "test", "export.zip", connector.Options{})
		sess := data.Session{ActivityType: "running", ContentKey: "w1"}
		if err := sink.Workout(ctx, &sess, stats, nil); err != nil {
			t.Fatalf("Workout: %v", err)
		}
		report, err := sink.Close(ctx)
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
		if i == 0 && report.SessionsAdded != 1 {
			t.Errorf("first import = %d added, want 1", report.SessionsAdded)
		}
		if i == 1 && report.SessionsSkipped != 1 {
			t.Errorf("re-import = %d skipped, want 1", report.SessionsSkipped)
		}
	}
	// Both runs attached their stats, to the Session each was given.
	if len(store.stats) != 2 {
		t.Errorf("sessions carrying stats = %d, want both runs to have attached them", len(store.stats))
	}
}
