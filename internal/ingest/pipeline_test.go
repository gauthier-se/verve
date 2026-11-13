package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
	"github.com/gauthier-se/verve/internal/workouts/route"
)

// pipelineTime is the current time of the store of the pipeline tests, when their imports are
// created.
var pipelineTime = time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC)

// pipelineEnv is a migrated store with the shipped catalog synchronized and a user, and the
// catalogs and the codec of a pipeline.
type pipelineEnv struct {
	store      store.Store
	user       store.User
	catalog    *catalog.Catalog
	activities *catalog.Activities
	codec      *route.Codec
}

// newPipelineEnv returns an environment on a database in a fresh temporary directory, closed at the
// end of the test.
func newPipelineEnv(t *testing.T) *pipelineEnv {
	t.Helper()

	ctx := t.Context()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(ctx, db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clock.NewFake(pipelineTime))
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	cat, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, s.MetricTypes().Sync(ctx, cat))

	activities, err := catalog.ShippedActivities()
	require.NoError(t, err)

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, enc.Close())
		dec.Close()
	})

	return &pipelineEnv{
		store:      s,
		user:       user,
		catalog:    cat,
		activities: activities,
		codec:      route.NewCodec(enc, dec),
	}
}

// pipeline returns a pipeline of the environment, configured by opts.
func (e *pipelineEnv) pipeline(opts ...func(*ingest.Config)) *ingest.Pipeline {
	cfg := ingest.Config{Store: e.store, Catalog: e.catalog, Activities: e.activities, Codec: e.codec}
	for _, opt := range opts {
		opt(&cfg)
	}

	return ingest.NewPipeline(cfg)
}

// running creates a running import of origin for the user.
func (e *pipelineEnv) running(t *testing.T, origin store.Origin) store.Import {
	t.Helper()

	imp, err := e.store.Imports().Create(t.Context(), store.NewImport{UserID: e.user.ID, Origin: origin})
	require.NoError(t, err)

	imp, err = e.store.Imports().SetStatus(t.Context(), e.user.ID, imp.ID, store.ImportRunning)
	require.NoError(t, err)

	return imp
}

// run runs records through a default pipeline, in a new running import of origin, and returns the
// import once the run is over.
func (e *pipelineEnv) run(t *testing.T, origin store.Origin, records ...ingest.Record) store.Import {
	t.Helper()

	imp := e.running(t, origin)
	require.NoError(t, e.pipeline().Run(t.Context(), e.user.ID, imp.ID, recordsOf(records...)))

	return e.importOf(t, imp.ID)
}

// importOf returns the stored import of id.
func (e *pipelineEnv) importOf(t *testing.T, id int64) store.Import {
	t.Helper()

	imp, err := e.store.Imports().Get(t.Context(), e.user.ID, id)
	require.NoError(t, err)

	return imp
}

// metricType returns the stored type of identifier.
func (e *pipelineEnv) metricType(t *testing.T, identifier string) store.MetricType {
	t.Helper()

	mt, err := e.store.MetricTypes().GetByIdentifier(t.Context(), identifier)
	require.NoError(t, err)

	return mt
}

// samples returns every stored sample of the user of the type of identifier.
func (e *pipelineEnv) samples(t *testing.T, identifier string) []store.Sample {
	t.Helper()

	samples, err := e.store.Samples().List(t.Context(), e.user.ID, store.SamplesQuery{
		TypeID: e.metricType(t, identifier).ID,
		From:   time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit:  10000,
	})
	require.NoError(t, err)

	return samples
}

// workouts returns every stored workout of the user, most recent first.
func (e *pipelineEnv) workouts(t *testing.T) []store.Workout {
	t.Helper()

	workouts, err := e.store.Workouts().List(t.Context(), e.user.ID, store.WorkoutsQuery{
		From:  time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit: 1000,
	})
	require.NoError(t, err)

	return workouts
}

// recordsOf returns the records, each at its index in records.
func recordsOf(records ...ingest.Record) iter.Seq2[ingest.Record, error] {
	return func(yield func(ingest.Record, error) bool) {
		for i, r := range records {
			r.Position = int64(i)
			if !yield(r, nil) {
				return
			}
		}
	}
}

// quantity returns a record of s.
func quantity(s ingest.QuantitySample) ingest.Record {
	return ingest.Record{Quantity: &s}
}

// category returns a record of s.
func category(s ingest.CategorySample) ingest.Record {
	return ingest.Record{Category: &s}
}

// workoutRecord returns a record of w.
func workoutRecord(w ingest.Workout) ingest.Record {
	return ingest.Record{Workout: &w}
}

// heartRates returns n heart rate samples of the watch without uuid, one minute apart, each before
// the previous one.
func heartRates(n int) []ingest.Record {
	records := make([]ingest.Record, n)

	for i := range records {
		s := quantitySample()
		s.UUID = ""
		s.Start = time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)
		s.End = s.Start
		records[i] = quantity(s)
	}

	return records
}

// Given a stream of normalized records, when the pipeline runs, then it writes them in
// transactions of at most 5,000 records, each storing the position of the next record with the
// batch.
func TestRun_WritesInTransactionsOfAtMost5000Records(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)
	ctx := t.Context()
	imp := env.running(t, store.OriginNative)
	records := heartRates(5001)

	var positions []int64

	seq := func(yield func(ingest.Record, error) bool) {
		for i, r := range records {
			stored := env.importOf(t, imp.ID)
			positions = append(positions, stored.Position)

			r.Position = int64(i)
			if !yield(r, nil) {
				return
			}
		}
	}

	require.NoError(t, env.pipeline().Run(ctx, env.user.ID, imp.ID, seq))

	require.Len(t, positions, 5001)
	require.Equal(t, int64(0), positions[4999], "nothing is committed before 5,000 records")
	require.Equal(t, int64(5000), positions[5000], "the first 5,000 records are committed together")

	got := env.importOf(t, imp.ID)
	require.Equal(t, int64(5001), got.Position)
	require.Equal(t, store.ImportCounters{Received: 5001, Inserted: 5001}, got.Report.Total)
	require.Equal(t, map[string]store.ImportCounters{"heart_rate": {Received: 5001, Inserted: 5001}}, got.Report.Types)
	require.Len(t, env.samples(t, "HKQuantityTypeIdentifierHeartRate"), 5001)
}

// Given an identifier of the catalog, then the record gets that type. Given an identifier listed as
// unsupported, then it is counted unsupported, under the identifier as received, and dropped. Given
// another identifier, then an uncurated type is created with the kind and unit of the record.
func TestRun_MapsIdentifiersToTypes(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	ecg := quantitySample()
	ecg.UUID, ecg.Type = "", "HKDataTypeIdentifierElectrocardiogram"

	uv := quantitySample()
	uv.UUID, uv.Type, uv.Value, uv.Unit = "", "HKQuantityTypeIdentifierPollenIndex", 3, "count"

	mood := categorySample()
	mood.UUID, mood.Type, mood.Value = "", "HKCategoryTypeIdentifierLucidDream", "HKCategoryValueLucidDreamVivid"

	imp := env.run(t, store.OriginNative, quantity(quantitySample()), quantity(ecg), quantity(uv), category(mood))

	require.Equal(t, map[string]store.ImportCounters{
		"heart_rate":                            {Received: 1, Inserted: 1},
		"HKDataTypeIdentifierElectrocardiogram": {Received: 1, Unsupported: 1},
		"pollen_index":                          {Received: 1, Inserted: 1},
		"lucid_dream":                           {Received: 1, Inserted: 1},
	}, imp.Report.Types)
	require.Equal(t, store.ImportCounters{Received: 4, Inserted: 3, Unsupported: 1}, imp.Report.Total)

	hr := env.samples(t, "HKQuantityTypeIdentifierHeartRate")
	require.Len(t, hr, 1)
	require.InDelta(t, 62.0, hr[0].Value, 1e-9)

	_, err := env.store.MetricTypes().GetByIdentifier(t.Context(), ecg.Type)
	require.ErrorIs(t, err, store.ErrNotFound, "an unsupported identifier gets no type")

	uvType := env.metricType(t, uv.Type)
	require.Equal(t, catalog.KindQuantity, uvType.Kind)
	require.Equal(t, "count", uvType.Unit)
	require.False(t, uvType.Curated)
	require.Len(t, env.samples(t, uv.Type), 1)

	moodType := env.metricType(t, mood.Type)
	require.Equal(t, catalog.KindCategory, moodType.Kind)
	require.False(t, moodType.Curated)

	moods := env.samples(t, mood.Type)
	require.Len(t, moods, 1)
	require.Equal(t, "HKCategoryValueLucidDreamVivid", moods[0].Category, "an uncurated type keeps its values as received")
}

// Given a category sample of a curated type, then it stores the key of its value, or the value as
// received when the catalog does not list it.
func TestRun_StoresTheKeysOfCategoryValues(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	core := categorySample()

	future := categorySample()
	future.UUID, future.Start, future.End = "", "2026-11-10T03:00:00+01:00", "2026-11-10T03:30:00+01:00"
	future.Value = "HKCategoryValueSleepAnalysisAsleepDreaming"

	imp := env.run(t, store.OriginNative, category(core), category(future))
	require.Equal(t, store.ImportCounters{Received: 2, Inserted: 2}, imp.Report.Total)

	samples := env.samples(t, core.Type)
	require.Len(t, samples, 2)
	require.Equal(t, "asleep_core", samples[0].Category)
	require.Equal(t, "HKCategoryValueSleepAnalysisAsleepDreaming", samples[1].Category)
}

// resolveCounting is a store whose transactions count the calls of Resolve, by source name.
type resolveCounting struct {
	store.Store

	resolves map[string]int
}

func (s *resolveCounting) WithTx(ctx context.Context, fn func(context.Context, store.Repositories) error) error {
	return s.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		return fn(ctx, resolveCountingTx{Repositories: tx, s: s})
	})
}

type resolveCountingTx struct {
	store.Repositories

	s *resolveCounting
}

func (tx resolveCountingTx) Sources() store.SourcesRepository {
	return resolveCountingSources{SourcesRepository: tx.Repositories.Sources(), s: tx.s}
}

type resolveCountingSources struct {
	store.SourcesRepository

	s *resolveCounting
}

func (r resolveCountingSources) Resolve(ctx context.Context, userID int64, info store.SourceInfo, kind store.SourceKind, seenAt time.Time) (store.Source, error) {
	r.s.resolves[info.Name]++

	return r.SourcesRepository.Resolve(ctx, userID, info, kind, seenAt)
}

// Given records, then their source is resolved and its kind detected, once per distinct source in
// a batch, and seen at the latest start of its records.
func TestRun_ResolvesEachSourceOncePerBatch(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)
	counting := &resolveCounting{Store: env.store, resolves: make(map[string]int)}
	imp := env.running(t, store.OriginNative)

	diary := store.SourceInfo{Name: "Glucose Diary", BundleID: "com.example.glucosediary"}
	glucose := quantitySample()
	glucose.UUID, glucose.Type, glucose.Value, glucose.Unit = "", "HKQuantityTypeIdentifierBloodGlucose", 5.4, "mmol<180.15588000005408>/L"
	glucose.Source = diary

	records := heartRates(4)
	slices.Reverse(records)
	records = slices.Insert(records, 2, quantity(glucose))

	pipeline := env.pipeline(func(cfg *ingest.Config) { cfg.Store, cfg.BatchSize = counting, 3 })
	require.NoError(t, pipeline.Run(t.Context(), env.user.ID, imp.ID, recordsOf(records...)))

	require.Equal(t, map[string]int{"Apple Watch": 2, "Glucose Diary": 1}, counting.resolves,
		"two batches of the watch, one of the diary")

	sources, err := env.store.Sources().List(t.Context(), env.user.ID)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	byName := make(map[string]store.Source)
	for _, s := range sources {
		byName[s.Name] = s
	}

	require.Equal(t, store.SourceWatch, byName["Apple Watch"].Kind)
	require.Equal(t, store.SourceApp, byName["Glucose Diary"].Kind)
	require.Equal(t, time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC), byName["Apple Watch"].LastSeenAt)

	for _, s := range env.samples(t, "HKQuantityTypeIdentifierHeartRate") {
		require.Equal(t, byName["Apple Watch"].ID, s.SourceID)
	}

	glucoses := env.samples(t, glucose.Type)
	require.Len(t, glucoses, 1)
	require.Equal(t, byName["Glucose Diary"].ID, glucoses[0].SourceID)
}

// Given samples, then their natural key and rule follow dedup FR-1: from the uuid, whatever its
// case, with update_if_changed, from (type, source, start, end, value, category value) with ignore,
// or from (type, source, start, end) with replace for pre-aggregated points.
func TestRun_SampleKeysFollowTheRulesOfTheirOrigin(t *testing.T) {
	t.Parallel()

	withUUID := quantitySample()

	raw := quantitySample()
	raw.UUID = ""

	point := quantitySample()
	point.UUID, point.Type, point.Value, point.Unit = "", "HKQuantityTypeIdentifierStepCount", 1240, "count"
	point.End = "2026-11-10T09:00:00+01:00"
	point.PreAggregated = true

	tests := []struct {
		name   string
		first  ingest.QuantitySample
		again  func(*ingest.QuantitySample)
		want   store.ImportCounters
		values []float64
	}{
		{"UUIDSameValue", withUUID, func(*ingest.QuantitySample) {}, store.ImportCounters{Duplicates: 1}, []float64{62}},
		{"UUIDNewValue", withUUID, func(s *ingest.QuantitySample) { s.Value = 64 }, store.ImportCounters{Updated: 1}, []float64{64}},
		{"UUIDOtherCase", withUUID, func(s *ingest.QuantitySample) { s.UUID, s.Value = strings.ToLower(s.UUID), 64 }, store.ImportCounters{Updated: 1}, []float64{64}},
		{"RawSameValue", raw, func(*ingest.QuantitySample) {}, store.ImportCounters{Duplicates: 1}, []float64{62}},
		{"RawNewValue", raw, func(s *ingest.QuantitySample) { s.Value = 64 }, store.ImportCounters{Inserted: 1}, []float64{62, 64}},
		{"PointSameValue", point, func(*ingest.QuantitySample) {}, store.ImportCounters{Updated: 1}, []float64{1240}},
		{"PointNewValue", point, func(s *ingest.QuantitySample) { s.Value = 1300 }, store.ImportCounters{Updated: 1}, []float64{1300}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := newPipelineEnv(t)
			env.run(t, store.OriginNative, quantity(tt.first))

			again := tt.first
			tt.again(&again)

			imp := env.run(t, store.OriginNative, quantity(again))

			tt.want.Received = 1
			require.Equal(t, tt.want, imp.Report.Total)

			var values []float64
			for _, s := range env.samples(t, tt.first.Type) {
				values = append(values, s.Value)
			}

			require.InDeltaSlice(t, tt.values, values, 1e-9)
		})
	}
}

// Given the same records twice, then the second run counts zero inserts.
func TestRun_SameRecordsTwiceCountZeroInserts(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	raw := quantitySample()
	raw.UUID = ""

	records := []ingest.Record{quantity(quantitySample()), quantity(raw), category(categorySample()), workoutRecord(workout())}

	first := env.run(t, store.OriginNative, records...)
	require.Equal(t, store.ImportCounters{Received: 4, Inserted: 4}, first.Report.Total)

	second := env.run(t, store.OriginNative, records...)
	require.Equal(t, store.ImportCounters{Received: 4, Duplicates: 4}, second.Report.Total)
	require.Equal(t, store.ImportCounters{Received: 1, Duplicates: 1}, second.Report.Workouts)
}

// Given workouts, then their natural key and rule follow dedup FR-1: (origin, external id) with
// update, or (activity type, source, start, end) with ignore, and their route is encoded and stored.
func TestRun_WorkoutKeysFollowTheirRulesAndRoutesAreStored(t *testing.T) {
	t.Parallel()

	t.Run("ExternalID", func(t *testing.T) {
		t.Parallel()

		env := newPipelineEnv(t)
		first := env.run(t, store.OriginNative, workoutRecord(workout()))
		require.Equal(t, store.ImportCounters{Received: 1, Inserted: 1}, first.Report.Workouts)

		stored := env.workouts(t)
		require.Len(t, stored, 1)

		w := stored[0]
		require.Equal(t, "running", w.Activity)
		require.Equal(t, "9E7D5C3B-1A2F-4E6D-8C0B-2F4A6E8C0D1B", w.ExternalID)
		require.Equal(t, store.OriginNative, w.Origin)
		require.Equal(t, first.ID, w.ImportID)
		require.Equal(t, time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC), w.Start)
		require.Equal(t, 3600, w.UTCOffset)
		require.InDelta(t, 1800, w.Duration, 1e-9)
		require.InDelta(t, 5210, *w.Distance, 1e-9)
		require.Equal(t, ptr(false), w.Indoor)
		require.Equal(t, store.LocationOutdoor, w.LocationType)
		require.Len(t, w.Events, 2)

		rt, err := env.store.Workouts().GetRoute(t.Context(), env.user.ID, w.ID)
		require.NoError(t, err)
		require.Equal(t, 2, rt.PointCount)
		require.Equal(t, store.RouteBox{MinLat: 48.8566, MinLon: 2.3522, MaxLat: 48.85662, MaxLon: 2.35224}, rt.Box)
		require.InDelta(t, 3.68, rt.Distance, 0.01)
		require.NotEmpty(t, rt.Polyline)

		points, err := env.codec.Decode(rt.Encoded)
		require.NoError(t, err)
		require.Len(t, points, 2)
		require.Equal(t, time.Date(2026, 11, 10, 17, 0, 1, 0, time.UTC), points[0].Time.UTC())

		same := env.run(t, store.OriginNative, workoutRecord(workout()))
		require.Equal(t, store.ImportCounters{Received: 1, Duplicates: 1}, same.Report.Workouts)

		longer := workout()
		longer.Totals.Distance = &ingest.Quantity{Value: 5.4, Unit: "km"}
		longer.UUID = strings.ToLower(longer.UUID)
		updated := env.run(t, store.OriginNative, workoutRecord(longer))
		require.Equal(t, store.ImportCounters{Received: 1, Updated: 1}, updated.Report.Workouts)

		stored = env.workouts(t)
		require.Len(t, stored, 1)
		require.Equal(t, w.ID, stored[0].ID)
		require.InDelta(t, 5400, *stored[0].Distance, 1e-9)
	})

	t.Run("WithoutExternalID", func(t *testing.T) {
		t.Parallel()

		env := newPipelineEnv(t)

		w := workout()
		w.UUID = ""
		env.run(t, store.OriginXML, workoutRecord(w))

		again := w
		again.Totals.Distance = &ingest.Quantity{Value: 5.4, Unit: "km"}
		same := env.run(t, store.OriginXML, workoutRecord(again))
		require.Equal(t, store.ImportCounters{Received: 1, Duplicates: 1}, same.Report.Workouts)

		later := w
		later.Start, later.End, later.Route = "2026-11-10T18:01:00+01:00", "2026-11-10T18:32:00+01:00", nil
		other := env.run(t, store.OriginXML, workoutRecord(later))
		require.Equal(t, store.ImportCounters{Received: 1, Inserted: 1}, other.Report.Workouts)

		stored := env.workouts(t)
		require.Len(t, stored, 2)
		require.InDelta(t, 5210, *stored[1].Distance, 1e-9, "ignore keeps the stored workout")
	})

	t.Run("DuplicateGainsTheRouteItLacked", func(t *testing.T) {
		t.Parallel()

		env := newPipelineEnv(t)

		bare := workout()
		bare.Route = nil
		env.run(t, store.OriginNative, workoutRecord(bare))

		imp := env.run(t, store.OriginNative, workoutRecord(workout()))
		require.Equal(t, store.ImportCounters{Received: 1, Duplicates: 1}, imp.Report.Workouts)

		rt, err := env.store.Workouts().GetRoute(t.Context(), env.user.ID, env.workouts(t)[0].ID)
		require.NoError(t, err)
		require.Equal(t, 2, rt.PointCount)
	})

	t.Run("UnknownActivityType", func(t *testing.T) {
		t.Parallel()

		env := newPipelineEnv(t)

		w := workout()
		w.ActivityType = "HKWorkoutActivityTypeSkyRunning"
		imp := env.run(t, store.OriginNative, workoutRecord(w))

		require.Equal(t, store.ImportCounters{Received: 1, Inserted: 1, Warnings: 1}, imp.Report.Workouts)
		require.Equal(t, []string{`record 0 (workout): activity type "HKWorkoutActivityTypeSkyRunning" is not a known activity type, stored as other`}, imp.Report.Errors)
		require.Equal(t, "other", env.workouts(t)[0].Activity)
	})
}

// Given a valid and an invalid record in one batch, then the valid one is stored and the invalid
// one counted with its reason, and the counters hold per type. A sample without type counts in the
// total only, and a suspicious one is stored, counted and explained.
func TestRun_ValidAndInvalidRecordsInOneBatch(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	bpm := quantitySample()
	bpm.UUID, bpm.Unit = "", "bpm"

	untyped := quantitySample()
	untyped.UUID, untyped.Type = "", ""

	nested := categorySample()
	nested.Metadata = json.RawMessage(`[1]`)

	ancient := quantitySample()
	ancient.UUID, ancient.Start, ancient.End = "", "1999-12-31T23:00:00+01:00", "1999-12-31T23:00:00+01:00"

	backwards := workout()
	backwards.End = "2026-11-10T17:00:00+01:00"

	nameless := quantitySample()
	nameless.UUID, nameless.Source = "", store.SourceInfo{DeviceModel: "Watch"}

	imp := env.run(t, store.OriginNative,
		quantity(quantitySample()), quantity(bpm), quantity(untyped), category(nested), quantity(ancient), workoutRecord(backwards),
		quantity(nameless))

	require.Equal(t, store.ImportReport{
		Total: store.ImportCounters{Received: 7, Inserted: 2, Invalid: 5, Suspicious: 1},
		Types: map[string]store.ImportCounters{
			"heart_rate":     {Received: 4, Inserted: 2, Invalid: 2, Suspicious: 1},
			"sleep_analysis": {Received: 1, Invalid: 1},
		},
		Workouts: store.ImportCounters{Received: 1, Invalid: 1},
		Errors: []string{
			`record 1 (HKQuantityTypeIdentifierHeartRate): unit "bpm" is not a unit of count/min`,
			`record 2: type is missing`,
			`record 3 (HKCategoryTypeIdentifierSleepAnalysis): metadata is not a JSON object`,
			`record 4 (HKQuantityTypeIdentifierHeartRate): start is before 2000 or more than one day after the import was created, stored as suspicious`,
			`record 5 (workout): end is before start`,
			`record 6 (HKQuantityTypeIdentifierHeartRate): source name is missing`,
		},
	}, imp.Report)

	require.Len(t, env.samples(t, "HKQuantityTypeIdentifierHeartRate"), 2)
	require.Empty(t, env.samples(t, "HKCategoryTypeIdentifierSleepAnalysis"))
	require.Empty(t, env.workouts(t))
}

// blockFake is a block rule set that records the candidates it sees and blocks those of a uuid.
type blockFake struct {
	uuid       string
	candidates []ingest.BlockCandidate
}

func (b *blockFake) Blocks(_ context.Context, _ store.Repositories, _ int64, c ingest.BlockCandidate) (bool, error) {
	b.candidates = append(b.candidates, c)

	return c.UUID == b.uuid, nil
}

// dirtyFake is a dirty range recorder that keeps the ranges it is given.
type dirtyFake struct {
	ranges []ingest.DirtyRange
}

func (d *dirtyFake) Record(_ context.Context, _ store.Repositories, _ int64, ranges []ingest.DirtyRange) error {
	d.ranges = append(d.ranges, ranges...)

	return nil
}

// Given a block rule interface and a dirty range interface, then the pipeline calls both: block
// rules see each valid record with its type and source and drop those they block, and the dirty
// ranges of the samples stored or changed, merged per type, are recorded in the batch.
func TestRun_CallsBlockRulesAndDirtyRanges(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	blocked := quantitySample()
	blocked.UUID = "11111111-2222-4333-8444-555555555555"

	overlapping := quantitySample()
	overlapping.UUID, overlapping.End, overlapping.Value = "", "2026-11-10T08:10:00+01:00", 70

	later := quantitySample()
	later.UUID, later.Start, later.End = "", "2026-11-10T10:00:00+01:00", "2026-11-10T10:00:00+01:00"

	records := []ingest.Record{
		quantity(quantitySample()), quantity(blocked), quantity(overlapping), quantity(later),
		category(categorySample()), workoutRecord(workout()),
	}

	blocks := &blockFake{uuid: blocked.UUID}
	dirty := &dirtyFake{}
	imp := env.running(t, store.OriginNative)
	pipeline := env.pipeline(func(cfg *ingest.Config) { cfg.BlockRules, cfg.DirtyRanges = blocks, dirty })
	require.NoError(t, pipeline.Run(t.Context(), env.user.ID, imp.ID, recordsOf(records...)))

	hr := env.metricType(t, "HKQuantityTypeIdentifierHeartRate")
	sleep := env.metricType(t, "HKCategoryTypeIdentifierSleepAnalysis")

	sources, err := env.store.Sources().List(t.Context(), env.user.ID)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	require.Len(t, blocks.candidates, 6)
	require.Equal(t, ingest.BlockCandidate{
		TypeID:     hr.ID,
		SourceID:   sources[0].ID,
		Start:      time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC),
		UUID:       blocked.UUID,
		NaturalKey: blocks.candidates[1].NaturalKey,
	}, blocks.candidates[1])
	require.NotEmpty(t, blocks.candidates[1].NaturalKey)
	require.Zero(t, blocks.candidates[5].TypeID, "a workout has no metric type")
	require.Equal(t, workout().UUID, blocks.candidates[5].UUID)

	got := env.importOf(t, imp.ID)
	require.Equal(t, store.ImportCounters{Received: 4, Inserted: 3, Blocked: 1}, got.Report.Types["heart_rate"])
	require.Len(t, env.samples(t, "HKQuantityTypeIdentifierHeartRate"), 3)

	require.Equal(t, []ingest.DirtyRange{
		{TypeID: hr.ID, From: time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 7, 10, 0, 0, time.UTC)},
		{TypeID: hr.ID, From: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)},
		{TypeID: sleep.ID, From: time.Date(2026, 11, 10, 0, 12, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 1, 3, 30, 250_000_000, time.UTC)},
	}, dirty.ranges)

	dirty.ranges = nil
	again := env.running(t, store.OriginNative)
	require.NoError(t, pipeline.Run(t.Context(), env.user.ID, again.ID, recordsOf(records...)))
	require.Empty(t, dirty.ranges, "duplicates change no aggregate")
}

// Given the block rules and dirty ranges of this story, then they let every record through and
// record nothing.
func TestNoBlockRulesAndNoDirtyRanges_LetEverythingThrough(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	blocked, err := ingest.NoBlockRules{}.Blocks(t.Context(), env.store, env.user.ID, ingest.BlockCandidate{TypeID: 1, SourceID: 1})
	require.NoError(t, err)
	require.False(t, blocked)

	require.NoError(t, ingest.NoDirtyRanges{}.Record(t.Context(), env.store, env.user.ID, []ingest.DirtyRange{{TypeID: 1}}))

	imp := env.run(t, store.OriginNative, quantity(quantitySample()), workoutRecord(workout()))
	require.Equal(t, store.ImportCounters{Received: 2, Inserted: 2}, imp.Report.Total)
}

// errSourceFailed is the error of a record source that fails.
var errSourceFailed = errors.New("source failed")

// failingAfter returns the records of seq up to the one at position stop, excluded, then
// errSourceFailed.
func failingAfter(seq iter.Seq2[ingest.Record, error], stop int64) iter.Seq2[ingest.Record, error] {
	return func(yield func(ingest.Record, error) bool) {
		for r, err := range seq {
			if r.Position == stop {
				yield(ingest.Record{}, errSourceFailed)

				return
			}

			if !yield(r, err) {
				return
			}
		}
	}
}

// resumeRecords are records of every outcome, for the runs that stop and resume.
func resumeRecords() []ingest.Record {
	bpm := quantitySample()
	bpm.UUID, bpm.Unit = "", "bpm"

	ecg := quantitySample()
	ecg.UUID, ecg.Type = "", "HKDataTypeIdentifierElectrocardiogram"

	records := heartRates(3)
	records = append(records, quantity(bpm), quantity(ecg), category(categorySample()), workoutRecord(workout()))

	return append(records, heartRates(2)...)
}

// Given a run stopped after a committed batch, when it resumes from the stored position, then the
// committed records are skipped and the final report equals the one of an uninterrupted run.
func TestRun_ResumesFromTheStoredPosition(t *testing.T) {
	t.Parallel()

	records := resumeRecords()
	batches := func(cfg *ingest.Config) { cfg.BatchSize = 3 }

	whole := newPipelineEnv(t)
	wholeImp := whole.running(t, store.OriginNative)
	require.NoError(t, whole.pipeline(batches).Run(t.Context(), whole.user.ID, wholeImp.ID, recordsOf(records...)))
	want := whole.importOf(t, wholeImp.ID)

	env := newPipelineEnv(t)
	imp := env.running(t, store.OriginNative)

	err := env.pipeline(batches).Run(t.Context(), env.user.ID, imp.ID, failingAfter(recordsOf(records...), 5))
	require.ErrorIs(t, err, errSourceFailed)

	stopped := env.importOf(t, imp.ID)
	require.Equal(t, int64(3), stopped.Position, "the first batch is committed, the second is not")
	require.Equal(t, store.ImportCounters{Received: 3, Inserted: 3}, stopped.Report.Total)

	require.NoError(t, env.pipeline(batches).Run(t.Context(), env.user.ID, imp.ID, recordsOf(records...)))

	got := env.importOf(t, imp.ID)
	require.Equal(t, want.Position, got.Position)
	require.Equal(t, want.Report, got.Report)
	require.Equal(t, store.ImportCounters{Received: 9, Inserted: 5, Duplicates: 2, Invalid: 1, Unsupported: 1}, got.Report.Total)
	require.Len(t, env.samples(t, "HKQuantityTypeIdentifierHeartRate"), 3, "heartRates(2) repeats two of the first samples")
}

// Given a context cancelled between batches, then the pipeline stops at the next batch boundary,
// and the committed data stays.
func TestRun_StopsAtTheNextBatchBoundaryWhenCancelled(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)
	imp := env.running(t, store.OriginNative)
	ctx, cancel := context.WithCancel(t.Context())

	var read int

	seq := func(yield func(ingest.Record, error) bool) {
		for r, err := range recordsOf(heartRates(6)...) {
			if r.Position == 2 {
				cancel()
			}

			read++

			if !yield(r, err) {
				return
			}
		}
	}

	err := env.pipeline(func(cfg *ingest.Config) { cfg.BatchSize = 2 }).Run(ctx, env.user.ID, imp.ID, seq)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 4, read, "the batch under way is read to its boundary, and no further")

	got := env.importOf(t, imp.ID)
	require.Equal(t, int64(2), got.Position)
	require.Equal(t, store.ImportCounters{Received: 2, Inserted: 2}, got.Report.Total)
	require.Len(t, env.samples(t, "HKQuantityTypeIdentifierHeartRate"), 2)
}

// deletion returns a record of the deletion of uuid.
func deletion(uuid string) ingest.Record {
	return ingest.Record{Deletion: &ingest.Deletion{UUID: uuid}}
}

// Given samples of a uuid of any origin, and workouts of a uuid of the origins native and xml, when
// a native payload sends a sample and deletes uuids after its samples and workouts, its own sample
// included, then the samples of those uuids and the native workout with its route are removed,
// whatever the case of the uuid, while the xml workout stays. Each removal counts as deleted under
// its type key or the workouts, the data availability follows, and the ranges of the samples
// removed are recorded as dirty.
func TestRun_DeletionsRemoveTheRecordsOfTheirUUID(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)
	ctx := t.Context()

	kept := quantitySample()
	kept.UUID, kept.Start, kept.End = "", "2026-11-10T10:00:00+01:00", "2026-11-10T10:00:00+01:00"

	env.run(t, store.OriginHAE, quantity(quantitySample()), quantity(kept))
	env.run(t, store.OriginXML, workoutRecord(workout()))
	env.run(t, store.OriginNative, workoutRecord(workout()))

	workouts := env.workouts(t)
	require.Len(t, workouts, 2)

	native := workouts[slices.IndexFunc(workouts, func(w store.Workout) bool { return w.Origin == store.OriginNative })]
	_, err := env.store.Workouts().GetRoute(ctx, env.user.ID, native.ID)
	require.NoError(t, err)

	dirty := &dirtyFake{}
	imp := env.running(t, store.OriginNative)
	pipeline := env.pipeline(func(cfg *ingest.Config) { cfg.DirtyRanges = dirty })
	require.NoError(t, pipeline.Run(ctx, env.user.ID, imp.ID, recordsOf(
		category(categorySample()),
		deletion(strings.ToLower(quantitySample().UUID)),
		deletion(workout().UUID),
		deletion(strings.ToLower(categorySample().UUID)),
	)))

	require.Equal(t, store.ImportReport{
		Total: store.ImportCounters{Received: 1, Inserted: 1, Deleted: 3},
		Types: map[string]store.ImportCounters{
			"heart_rate":     {Deleted: 1},
			"sleep_analysis": {Received: 1, Inserted: 1, Deleted: 1},
		},
		Workouts: store.ImportCounters{Deleted: 1},
	}, env.importOf(t, imp.ID).Report)

	hr := env.samples(t, "HKQuantityTypeIdentifierHeartRate")
	require.Len(t, hr, 1)
	require.Empty(t, hr[0].UUID, "the sample without uuid stays")
	require.Empty(t, env.samples(t, "HKCategoryTypeIdentifierSleepAnalysis"), "a sample sent and deleted in one payload ends up deleted")

	workouts = env.workouts(t)
	require.Len(t, workouts, 1)
	require.Equal(t, store.OriginXML, workouts[0].Origin, "a deletion removes the native workouts only")

	_, err = env.store.Workouts().GetRoute(ctx, env.user.ID, native.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	hrType := env.metricType(t, "HKQuantityTypeIdentifierHeartRate")
	sleepType := env.metricType(t, "HKCategoryTypeIdentifierSleepAnalysis")

	stats, err := env.store.Samples().Stats(ctx, env.user.ID)
	require.NoError(t, err)
	require.Equal(t, []store.SampleStats{{
		TypeID:  hrType.ID,
		FirstAt: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC),
		LastAt:  time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC),
		Count:   1,
	}}, stats)

	sleep := ingest.DirtyRange{
		TypeID: sleepType.ID,
		From:   time.Date(2026, 11, 10, 0, 12, 0, 0, time.UTC),
		To:     time.Date(2026, 11, 10, 1, 3, 30, 250_000_000, time.UTC),
	}
	require.Equal(t, []ingest.DirtyRange{
		sleep,
		{TypeID: hrType.ID, From: time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)},
		sleep,
	}, dirty.ranges)
}

// Given uuids that match no record, a uuid deleted twice included, when a native payload deletes
// them, then each counts as notFound in the total only, nothing is removed, and no block rule
// sees them, so that none can be created.
func TestRun_DeletionOfAnUnknownUUIDCountsNotFound(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)
	ctx := t.Context()

	env.run(t, store.OriginNative, quantity(quantitySample()))

	blocks := &blockFake{}
	dirty := &dirtyFake{}
	imp := env.running(t, store.OriginNative)
	pipeline := env.pipeline(func(cfg *ingest.Config) { cfg.BlockRules, cfg.DirtyRanges = blocks, dirty })
	require.NoError(t, pipeline.Run(ctx, env.user.ID, imp.ID, recordsOf(
		deletion("5A4B3C2D-1E0F-4A9B-8C7D-6E5F4A3B2C1D"),
		deletion(quantitySample().UUID),
		deletion(quantitySample().UUID),
	)))

	require.Equal(t, store.ImportReport{
		Total: store.ImportCounters{Deleted: 1, NotFound: 2},
		Types: map[string]store.ImportCounters{"heart_rate": {Deleted: 1}},
	}, env.importOf(t, imp.ID).Report)
	require.Empty(t, blocks.candidates)
	require.Len(t, dirty.ranges, 1, "only the removal records a dirty range")
}

// Given a sample and a workout of a uuid of another user, when a native payload of the user deletes
// that uuid, then nothing of the other user is removed, and the uuid counts as notFound.
func TestRun_DeletionNeverRemovesTheRecordsOfAnotherUser(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)
	ctx := t.Context()

	other, err := env.store.Users().Create(ctx, store.NewUser{Username: "bob", Role: store.RoleMember})
	require.NoError(t, err)

	theirs, err := env.store.Imports().Create(ctx, store.NewImport{UserID: other.ID, Origin: store.OriginNative})
	require.NoError(t, err)

	_, err = env.store.Imports().SetStatus(ctx, other.ID, theirs.ID, store.ImportRunning)
	require.NoError(t, err)

	w := workout()
	w.UUID = quantitySample().UUID
	require.NoError(t, env.pipeline().Run(ctx, other.ID, theirs.ID, recordsOf(quantity(quantitySample()), workoutRecord(w))))

	imp := env.run(t, store.OriginNative, deletion(quantitySample().UUID))
	require.Equal(t, store.ImportReport{Total: store.ImportCounters{NotFound: 1}}, imp.Report)

	samples, err := env.store.Samples().Stats(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, samples, 1)
	require.Equal(t, int64(1), samples[0].Count)

	workouts, err := env.store.Workouts().List(ctx, other.ID, store.WorkoutsQuery{
		From:  time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, workouts, 1)
}

// Given records that their adapter found invalid while reading them, then each counts as invalid
// with its reason: a sample under the type key of a stored type, and in the total only without
// type or of a type not stored yet, which it does not create; a workout under the workouts; a
// deletion as invalid in the total only, never as received. A sample of an unsupported type counts
// as unsupported, as type mapping comes first.
func TestRun_RecordsThatTheAdapterFoundInvalid(t *testing.T) {
	t.Parallel()

	env := newPipelineEnv(t)

	invalid := func(of ingest.RecordKind, typ, reason string) ingest.Record {
		return ingest.Record{Invalid: &ingest.Invalid{Of: of, Type: typ, Reason: reason}}
	}

	imp := env.run(t, store.OriginNative,
		invalid(ingest.SampleRecord, "HKQuantityTypeIdentifierStepCount", "value is not a number"),
		invalid(ingest.SampleRecord, "", "type is missing"),
		invalid(ingest.WorkoutRecord, "", "uuid is missing"),
		invalid(ingest.DeletionRecord, "", "deletion is not a uuid"),
		invalid(ingest.SampleRecord, "HKQuantityTypeIdentifierSomethingNew", "unit is missing"),
		invalid(ingest.SampleRecord, "HKDataTypeIdentifierElectrocardiogram", "start is missing"),
	)

	require.Equal(t, store.ImportReport{
		Total: store.ImportCounters{Received: 5, Invalid: 5, Unsupported: 1},
		Types: map[string]store.ImportCounters{
			"step_count":                            {Received: 1, Invalid: 1},
			"HKDataTypeIdentifierElectrocardiogram": {Received: 1, Unsupported: 1},
		},
		Workouts: store.ImportCounters{Received: 1, Invalid: 1},
		Errors: []string{
			`record 0 (HKQuantityTypeIdentifierStepCount): value is not a number`,
			`record 1: type is missing`,
			`record 2 (workout): uuid is missing`,
			`record 3 (deletion): deletion is not a uuid`,
			`record 4 (HKQuantityTypeIdentifierSomethingNew): unit is missing`,
		},
	}, imp.Report)

	_, err := env.store.MetricTypes().GetByIdentifier(t.Context(), "HKQuantityTypeIdentifierSomethingNew")
	require.ErrorIs(t, err, store.ErrNotFound, "an invalid record creates no type")
}
