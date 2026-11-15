package ingest_test

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts"
)

// The sources of the merge tests: the watch of the user under its old name, and under its new one.
var (
	oldWatch = store.SourceInfo{Name: "Apple Watch de Camille", DeviceModel: "Watch"}
	newWatch = store.SourceInfo{Name: "Montre de Camille", DeviceModel: "Watch"}
)

// mergeEnv is a pipeline environment whose pipeline and merges record dirty ranges, flag duplicate
// workouts and recompute aggregates as the server does, and whose jobs run in the in-process
// worker.
type mergeEnv struct {
	*pipelineEnv

	pipeline *ingest.Pipeline
	merger   *ingest.Merger
}

// newMergeEnv returns a merge environment whose merges move batchSize records per transaction, for
// a user of the timezone Europe/Paris.
func newMergeEnv(t *testing.T, batchSize int) *mergeEnv {
	t.Helper()

	env := newPipelineEnv(t)

	_, err := env.store.Preferences().SetTimezone(t.Context(), env.user.ID, "Europe/Paris")
	require.NoError(t, err)

	recorder := aggregate.NewRecorder(nil)
	detector := workouts.NewDetector(env.activities)

	return &mergeEnv{
		pipelineEnv: env,
		pipeline: env.pipeline(func(cfg *ingest.Config) {
			cfg.DirtyRanges = recorder
			cfg.Duplicates = detector
		}),
		merger: ingest.NewMerger(ingest.MergeConfig{
			Store:       env.store,
			DirtyRanges: recorder,
			Duplicates:  detector,
			Recompute:   recorder,
			BatchSize:   batchSize,
		}),
	}
}

// importRecords runs records through the pipeline in a new running import of origin, and returns
// the import once the run is over.
func (e *mergeEnv) importRecords(t *testing.T, origin store.Origin, records ...ingest.Record) store.Import {
	t.Helper()

	imp := e.running(t, origin)
	require.NoError(t, e.pipeline.Run(t.Context(), e.user.ID, imp.ID, recordsOf(records...)))

	return e.importOf(t, imp.ID)
}

// startMerge merges sources into target as the API does, and returns its payload.
func (e *mergeEnv) startMerge(t *testing.T, target store.Source, sources ...store.Source) ingest.MergePayload {
	t.Helper()

	payload := ingest.MergePayload{TargetID: target.ID}
	for _, source := range sources {
		payload.SourceIDs = append(payload.SourceIDs, source.ID)
	}

	require.NoError(t, ingest.StartMerge(t.Context(), e.store, nil, e.user.ID, payload.TargetID, payload.SourceIDs))

	return payload
}

// runJobs runs the queued jobs of the merges and of the aggregates with the in-process worker until
// none is left, and checks that none is left to retry.
func (e *mergeEnv) runJobs(t *testing.T) {
	t.Helper()

	registry := jobs.NewRegistry()
	ingest.RegisterMerge(registry, e.merger)
	aggregate.Register(registry, e.store)

	worker := jobs.NewWorker(e.store, registry, clock.NewFake(pipelineTime), slog.New(slog.DiscardHandler), jobs.Options{})

	for {
		claimed, err := worker.Tick(t.Context())
		require.NoError(t, err)

		if !claimed {
			break
		}
	}

	for _, key := range []string{ingest.SerialKey(e.user.ID), aggregate.SerialKey(e.user.ID)} {
		active, err := e.store.Jobs().ActiveJobs(t.Context(), key)
		require.NoError(t, err)
		require.Empty(t, active, key)
	}
}

// source returns the source of info, resolving it as a record of it would.
func (e *mergeEnv) source(t *testing.T, info store.SourceInfo) store.Source {
	t.Helper()

	source, err := e.store.Sources().Resolve(t.Context(), e.user.ID, info, store.SourceWatch, time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	return source
}

// allWorkouts returns every stored workout of the user, duplicates included, by id.
func (e *mergeEnv) allWorkouts(t *testing.T) []store.Workout {
	t.Helper()

	all, err := e.store.Workouts().List(t.Context(), e.user.ID, store.WorkoutsQuery{
		From:           time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit:          1000,
		WithDuplicates: true,
	})
	require.NoError(t, err)

	slices.SortFunc(all, func(a, b store.Workout) int { return cmp.Compare(a.ID, b.ID) })

	return all
}

// rawHeartRate returns a heart rate of source without uuid, as an XML export gives it, at start.
func rawHeartRate(source store.SourceInfo, start string, value float64) ingest.Record {
	s := quantitySample()
	s.UUID, s.Start, s.End, s.Value, s.Source, s.Metadata = "", start, start, value, source, nil

	return quantity(s)
}

// oldExport returns the records of the watch under its old name, as an XML export gives them: raw
// heart rates, steps with a uuid, a pre-aggregated point of steps, and a run without uuid nor route.
func oldExport() []ingest.Record {
	steps := quantitySample()
	steps.UUID = "0A1B2C3D-4E5F-4A6B-8C7D-9E0F1A2B3C4D"
	steps.Type, steps.Unit, steps.Value, steps.Metadata = "HKQuantityTypeIdentifierStepCount", "count", 500, nil
	steps.Start, steps.End, steps.Source = "2026-11-10T08:00:00+01:00", "2026-11-10T08:10:00+01:00", oldWatch

	point := steps
	point.UUID, point.PreAggregated, point.Value = "", true, 1200
	point.Start, point.End = "2026-11-10T09:00:00+01:00", "2026-11-10T10:00:00+01:00"

	run := workout()
	run.UUID, run.Route, run.Source = "", nil, oldWatch

	return []ingest.Record{
		rawHeartRate(oldWatch, "2026-11-10T08:00:00+01:00", 62),
		rawHeartRate(oldWatch, "2026-11-10T08:05:00+01:00", 64),
		rawHeartRate(oldWatch, "2026-11-10T08:10:00+01:00", 66),
		quantity(steps),
		quantity(point),
		workoutRecord(run),
	}
}

// newRecords returns the records of the watch under its new name, as the native app gives them: a
// raw heart rate that the old export also holds, and the same run, with its uuid and route.
func newRecords() []ingest.Record {
	run := workout()
	run.Source = newWatch

	return []ingest.Record{rawHeartRate(newWatch, "2026-11-10T08:10:00+01:00", 66), workoutRecord(run)}
}

// mergeScenario imports the old export and the new records, renames the new watch, stores the
// priorities of the heart rate with the old watch first, and returns the old and the new watch.
func (e *mergeEnv) mergeScenario(t *testing.T) (store.Source, store.Source) {
	t.Helper()

	e.importRecords(t, store.OriginXML, oldExport()...)
	e.importRecords(t, store.OriginNative, newRecords()...)

	old, target := e.source(t, oldWatch), e.source(t, newWatch)

	_, err := e.store.Sources().Rename(t.Context(), e.user.ID, target.ID, "Ma montre")
	require.NoError(t, err)

	heart := e.metricType(t, "HKQuantityTypeIdentifierHeartRate")
	require.NoError(t, e.store.SourcePriorities().Set(t.Context(), e.user.ID, heart.ID,
		store.SourcePriorities{Order: []int64{old.ID, target.ID}}))

	e.runJobs(t)

	return old, target
}

// Given the old export and the new records of a watch renamed, the run of the old name kept and the
// run of the new name its duplicate, when the old watch is merged into the new one and the job has
// run, then every sample and workout is on the new watch, the raw heart rate that both had is stored
// once, the counts follow, the merged source is an alias of the new watch and has left the
// priorities, the new watch keeps its name and kind, its run with a route is now kept, and the
// aggregates of the heart rate are computed from the new watch alone.
func TestMerge_MovesTheRecordsOfTheMergedSourcesToTheTarget(t *testing.T) {
	t.Parallel()

	e := newMergeEnv(t, ingest.DefaultBatchSize)
	ctx := t.Context()
	old, target := e.mergeScenario(t)

	before := e.allWorkouts(t)
	require.Len(t, before, 2)
	require.Equal(t, before[0].ID, before[1].DuplicateOf, "the run of the source seen first is kept")

	e.startMerge(t, target, old)
	e.runJobs(t)

	sources, err := e.store.Sources().List(ctx, e.user.ID)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, target.ID, sources[0].ID)
	require.Equal(t, "Ma montre", sources[0].DisplayName)
	require.Equal(t, store.SourceWatch, sources[0].Kind)
	require.Zero(t, sources[0].MergedInto)

	require.Equal(t, target.ID, e.source(t, oldWatch).ID, "the old identity is an alias of the target")

	heartRates := e.samples(t, "HKQuantityTypeIdentifierHeartRate")
	steps := e.samples(t, "HKQuantityTypeIdentifierStepCount")

	for _, s := range slices.Concat(heartRates, steps) {
		require.Equal(t, target.ID, s.SourceID)
	}

	values := make([]float64, 0, len(heartRates))
	for _, s := range heartRates {
		values = append(values, s.Value)
	}

	require.Equal(t, []float64{62, 64, 66}, values, "the heart rate that both had is stored once")
	require.Len(t, steps, 2)

	counts, err := e.store.Sources().SampleCounts(ctx, e.user.ID)
	require.NoError(t, err)
	require.Equal(t, map[int64]int64{target.ID: 5}, counts)

	stats, err := e.store.Samples().Stats(ctx, e.user.ID)
	require.NoError(t, err)

	byType := make(map[int64]int64, len(stats))
	for _, s := range stats {
		byType[s.TypeID] = s.Count
	}

	heart := e.metricType(t, "HKQuantityTypeIdentifierHeartRate")
	require.Equal(t, int64(3), byType[heart.ID])

	priorities, err := e.store.SourcePriorities().Get(ctx, e.user.ID, heart.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{target.ID}, priorities.Order)

	after := e.allWorkouts(t)
	require.Len(t, after, 2)

	for _, w := range after {
		require.Equal(t, target.ID, w.SourceID)
	}

	require.Equal(t, after[1].ID, after[0].DuplicateOf, "the run with a route is kept once both are of one source")
	require.Zero(t, after[1].DuplicateOf)

	hours, err := e.store.Aggregates().ListHourly(ctx, e.user.ID, heart.ID,
		time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, hours, 1)
	require.Equal(t, store.SelectedSource, hours[0].Selection)
	require.Equal(t, target.ID, hours[0].SourceID)
	require.Equal(t, int64(3), hours[0].Count)
	require.InDelta(t, 192, hours[0].Sum, 1e-9)
}

// Given an export imported before a merge, when it is imported again after the merge, then the
// import inserts nothing, and neither do the records of the target imported again.
func TestMerge_ReimportingAnExportImportedBeforeTheMergeInsertsNothing(t *testing.T) {
	t.Parallel()

	e := newMergeEnv(t, ingest.DefaultBatchSize)
	old, target := e.mergeScenario(t)

	e.startMerge(t, target, old)
	e.runJobs(t)

	counts, err := e.store.Sources().SampleCounts(t.Context(), e.user.ID)
	require.NoError(t, err)

	again := e.importRecords(t, store.OriginXML, oldExport()...)
	require.Zero(t, again.Report.Total.Inserted)
	require.Equal(t, int64(len(oldExport())), again.Report.Total.Received)

	again = e.importRecords(t, store.OriginNative, newRecords()...)
	require.Zero(t, again.Report.Total.Inserted)

	after, err := e.store.Sources().SampleCounts(t.Context(), e.user.ID)
	require.NoError(t, err)
	require.Equal(t, counts, after)
	require.Len(t, e.allWorkouts(t), 2)
}

// cancellingStore cancels a context once a number of its transactions have committed, so that a
// merge stops at the next batch boundary as on shutdown.
type cancellingStore struct {
	store.Store

	commits int
	cancel  context.CancelFunc
}

func (s *cancellingStore) WithTx(ctx context.Context, fn func(context.Context, store.Repositories) error) error {
	if err := s.Store.WithTx(ctx, fn); err != nil {
		return err
	}

	if s.commits--; s.commits == 0 {
		s.cancel()
	}

	return nil
}

// mergeOutcome is what a merge leaves, as the tests compare it.
type mergeOutcome struct {
	Sources  []store.Source
	Samples  []store.Sample
	Workouts []store.Workout
	Counts   map[int64]int64
	Stats    []store.SampleStats
	Hours    []store.AggregateRow
}

// outcome returns what the merges of the environment left.
func (e *mergeEnv) outcome(t *testing.T) mergeOutcome {
	t.Helper()

	ctx := t.Context()

	sources, err := e.store.Sources().List(ctx, e.user.ID)
	require.NoError(t, err)

	counts, err := e.store.Sources().SampleCounts(ctx, e.user.ID)
	require.NoError(t, err)

	stats, err := e.store.Samples().Stats(ctx, e.user.ID)
	require.NoError(t, err)

	hours, err := e.store.Aggregates().ListHourly(ctx, e.user.ID, e.metricType(t, "HKQuantityTypeIdentifierHeartRate").ID,
		time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	return mergeOutcome{
		Sources:  sources,
		Samples:  append(e.samples(t, "HKQuantityTypeIdentifierHeartRate"), e.samples(t, "HKQuantityTypeIdentifierStepCount")...),
		Workouts: e.allWorkouts(t),
		Counts:   counts,
		Stats:    stats,
		Hours:    hours,
	}
}

// Given a merge stopped after each number of its transactions, when its job is claimed again, then
// it resumes and leaves what an uninterrupted merge leaves.
func TestMerge_ResumesAfterAStopAndEndsAsAnUninterruptedRun(t *testing.T) {
	t.Parallel()

	uninterrupted := newMergeEnv(t, 1)
	old, target := uninterrupted.mergeScenario(t)
	uninterrupted.startMerge(t, target, old)
	uninterrupted.runJobs(t)

	want := uninterrupted.outcome(t)

	// The old watch has 5 samples and a workout, which a batch of 1 moves in a transaction each.
	stops := 0

	for commits := 1; ; commits++ {
		e := newMergeEnv(t, 1)
		old, target := e.mergeScenario(t)
		payload := e.startMerge(t, target, old)

		ctx, cancel := context.WithCancel(t.Context())
		stopping := &cancellingStore{Store: e.store, commits: commits, cancel: cancel}
		stopped := ingest.NewMerger(ingest.MergeConfig{
			Store:       stopping,
			DirtyRanges: aggregate.NewRecorder(nil),
			Duplicates:  workouts.NewDetector(e.activities),
			Recompute:   aggregate.NewRecorder(nil),
			BatchSize:   1,
		})

		err := stopped.Run(ctx, e.user.ID, payload)
		cancel()

		e.runJobs(t)
		require.Equal(t, want, e.outcome(t), "stopped after %d transactions", commits)

		if err == nil {
			break
		}

		require.ErrorIs(t, err, context.Canceled, "stopped after %d transactions", commits)

		stops++
	}

	require.GreaterOrEqual(t, stops, 6, "the merge stops after each of its batches")
}

// Given a raw heart rate that the old and the new watch both hold, when the merge removes the moved
// one as a duplicate, then the transaction of its batch records its range as dirty, as any removal
// does, and the data availability of the heart rate counts one sample less.
func TestMerge_RemovingADuplicateRecordsItsDirtyRange(t *testing.T) {
	t.Parallel()

	e := newMergeEnv(t, ingest.DefaultBatchSize)
	ctx := t.Context()
	old, target := e.mergeScenario(t)
	payload := e.startMerge(t, target, old)

	heart := e.metricType(t, "HKQuantityTypeIdentifierHeartRate")
	statsOf := func() store.SampleStats {
		stats, err := e.store.Samples().Stats(ctx, e.user.ID)
		require.NoError(t, err)

		i := slices.IndexFunc(stats, func(s store.SampleStats) bool { return s.TypeID == heart.ID })
		require.GreaterOrEqual(t, i, 0)

		return stats[i]
	}

	before := statsOf()

	merger := ingest.NewMerger(ingest.MergeConfig{
		Store:       e.store,
		DirtyRanges: aggregate.NewRecorder(nil),
		Duplicates:  workouts.NewDetector(e.activities),
		Recompute:   ingest.NoRecompute{},
	})
	require.NoError(t, merger.Run(ctx, e.user.ID, payload))

	ranges, err := e.store.Aggregates().ListDirty(ctx, e.user.ID)
	require.NoError(t, err)
	require.Len(t, ranges, 1)

	removedAt := time.Date(2026, 11, 10, 7, 10, 0, 0, time.UTC)
	require.Equal(t, heart.ID, ranges[0].TypeID)
	require.Equal(t, removedAt, ranges[0].From)
	require.Equal(t, removedAt, ranges[0].To)

	after := statsOf()
	require.Equal(t, before.Count-1, after.Count)
	require.Equal(t, before.FirstAt, after.FirstAt)
	require.Equal(t, before.LastAt, after.LastAt)
}

// Given a merger without recomputer, when a merge ends, then it ends without recomputing.
func TestMerge_WithoutRecomputerEnds(t *testing.T) {
	t.Parallel()

	e := newMergeEnv(t, ingest.DefaultBatchSize)
	old, target := e.mergeScenario(t)
	payload := e.startMerge(t, target, old)

	merger := ingest.NewMerger(ingest.MergeConfig{Store: e.store})
	require.NoError(t, merger.Run(t.Context(), e.user.ID, payload))

	_, err := e.store.Sources().Get(t.Context(), e.user.ID, old.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}
