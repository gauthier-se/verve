package aggregate_test

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/preferences"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// jobTime is the current time of the job tests.
var jobTime = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// watch is the source of the samples of the job tests.
var watch = store.SourceInfo{Name: "Apple Watch", DeviceModel: "Watch"}

// The identifiers of the types of the job tests.
const (
	stepCount = "HKQuantityTypeIdentifierStepCount"
	heartRate = "HKQuantityTypeIdentifierHeartRate"
)

// txHook is a store whose WithTx calls before, when set, ahead of each transaction, outside it.
type txHook struct {
	store.Store

	mu     sync.Mutex
	before func(ctx context.Context)
	txs    int
}

func (h *txHook) WithTx(ctx context.Context, fn func(ctx context.Context, tx store.Repositories) error) error {
	h.mu.Lock()
	before := h.before
	h.txs++
	h.mu.Unlock()

	if before != nil {
		before(ctx)
	}

	return h.Store.WithTx(ctx, fn)
}

// set sets before and resets the count of transactions.
func (h *txHook) set(before func(ctx context.Context)) {
	h.mu.Lock()
	h.before, h.txs = before, 0
	h.mu.Unlock()
}

// count returns the number of transactions since set.
func (h *txHook) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.txs
}

// jobEnv is a migrated store with the shipped catalog and a user, a pipeline of batches of one
// record whose dirty ranges the aggregates record, and a worker that runs the aggregate.recompute
// jobs on a hooked store, all on one fake clock.
type jobEnv struct {
	store    store.Store
	hook     *txHook
	user     store.User
	pipeline *ingest.Pipeline
	worker   *jobs.Worker
	recorder aggregate.Recorder
}

func newJobEnv(t *testing.T) *jobEnv {
	t.Helper()

	ctx := t.Context()
	clk := clock.NewFake(jobTime)

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(ctx, db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clk)
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	cat, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, s.MetricTypes().Sync(ctx, cat))

	activities, err := catalog.ShippedActivities()
	require.NoError(t, err)

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	hook := &txHook{Store: s}
	registry := jobs.NewRegistry()
	aggregate.Register(registry, hook)

	worker := jobs.NewWorker(s, registry, clk, slog.New(slog.DiscardHandler), jobs.Options{})
	recorder := aggregate.NewRecorder(worker)

	return &jobEnv{
		store: s,
		hook:  hook,
		user:  user,
		pipeline: ingest.NewPipeline(ingest.Config{
			Store: s, Catalog: cat, Activities: activities, DirtyRanges: recorder, BatchSize: 1,
		}),
		worker:   worker,
		recorder: recorder,
	}
}

// setTimezone stores name as the timezone of the user, without recomputing anything.
func (e *jobEnv) setTimezone(t *testing.T, name string) {
	t.Helper()

	_, err := e.store.Preferences().SetTimezone(t.Context(), e.user.ID, name)
	require.NoError(t, err)
}

// testingT is what the helpers need of a *testing.T or a *rapid.T.
type testingT interface {
	require.TestingT
	Helper()
}

// importRecords runs records through the pipeline, in a new running import of the user.
func (e *jobEnv) importRecords(ctx context.Context, t testingT, records ...ingest.Record) {
	t.Helper()

	e.importRecordsOf(ctx, t, e.user.ID, records...)
}

// importRecordsOf runs records through the pipeline, in a new running import of the user of userID.
func (e *jobEnv) importRecordsOf(ctx context.Context, t testingT, userID int64, records ...ingest.Record) {
	t.Helper()

	imp, err := e.store.Imports().Create(ctx, store.NewImport{UserID: userID, Origin: store.OriginNative})
	require.NoError(t, err)

	_, err = e.store.Imports().SetStatus(ctx, userID, imp.ID, store.ImportRunning)
	require.NoError(t, err)

	err = e.pipeline.Run(ctx, userID, imp.ID, func(yield func(ingest.Record, error) bool) {
		for i, r := range records {
			r.Position = int64(i)
			if !yield(r, nil) {
				return
			}
		}
	})
	require.NoError(t, err)
}

// runJobs runs the due jobs until none is left, and returns how many ran.
func (e *jobEnv) runJobs(t *testing.T) int {
	t.Helper()

	return e.runJobsOn(t.Context(), t)
}

// runJobsOn is runJobs for any test.
func (e *jobEnv) runJobsOn(ctx context.Context, t testingT) int {
	t.Helper()

	ran := 0

	for {
		claimed, err := e.worker.Tick(ctx)
		require.NoError(t, err)

		if !claimed {
			return ran
		}

		ran++
	}
}

// activeJobs returns the queued and running aggregate.recompute jobs of the user.
func (e *jobEnv) activeJobs(t *testing.T) []store.Job {
	t.Helper()

	active, err := e.store.Jobs().ActiveJobs(t.Context(), aggregate.SerialKey(e.user.ID))
	require.NoError(t, err)

	return active
}

// typeOf returns the stored type of identifier.
func (e *jobEnv) typeOf(t *testing.T, identifier string) store.MetricType {
	t.Helper()

	mt, err := e.store.MetricTypes().GetByIdentifier(t.Context(), identifier)
	require.NoError(t, err)

	return mt
}

// sourceOf returns the stored source of the user named name.
func (e *jobEnv) sourceOf(t *testing.T, name string) store.Source {
	t.Helper()

	sources, err := e.store.Sources().List(t.Context(), e.user.ID)
	require.NoError(t, err)

	i := slices.IndexFunc(sources, func(s store.Source) bool { return s.Name == name })
	require.GreaterOrEqual(t, i, 0, "source %s", name)

	return sources[i]
}

// hourly returns every hourly row of the user and the type of identifier.
func (e *jobEnv) hourly(t *testing.T, identifier string) []store.AggregateRow {
	t.Helper()

	rows, err := e.store.Aggregates().ListHourly(t.Context(), e.user.ID, e.typeOf(t, identifier).ID, time.Unix(0, 0), jobTime.AddDate(10, 0, 0))
	require.NoError(t, err)

	return rows
}

// daily returns every daily row of the user and the type of identifier.
func (e *jobEnv) daily(t *testing.T, identifier string) []store.AggregateRow {
	t.Helper()

	rows, err := e.store.Aggregates().ListDaily(t.Context(), e.user.ID, e.typeOf(t, identifier).ID, time.Unix(0, 0), jobTime.AddDate(10, 0, 0))
	require.NoError(t, err)

	return rows
}

// dirty returns the dirty ranges of the user.
func (e *jobEnv) dirty(t *testing.T) []store.DirtyRange {
	t.Helper()

	ranges, err := e.store.Aggregates().ListDirty(t.Context(), e.user.ID)
	require.NoError(t, err)

	return ranges
}

// steps returns a step count record of the watch from start to end, RFC 3339 times, of value.
func steps(start, end string, value float64) ingest.Record {
	return ingest.Record{Quantity: &ingest.QuantitySample{
		Type: stepCount, Start: start, End: end, Value: value, Unit: "count", Source: watch,
	}}
}

// utc returns the UTC instant of the RFC 3339 time value.
func utc(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)

	return parsed.UTC()
}

// Given cumulative samples of one source spanning several local hours of Asia/Kolkata, imported
// in two batches, then the transaction of each batch records the ranges of its samples, and queues
// one aggregate.recompute job of the user with the serial key aggregate:<user id>, the second batch
// finding it queued. When the job runs, then each local hour holds the prorated sum and the number
// of samples that overlap it, the local day the sums of its hours and the number of hours with
// data, and the ranges are gone.
func TestRecompute_ProratesCumulativeSamplesOverLocalHours(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()
	e.setTimezone(t, "Asia/Kolkata")

	e.importRecords(ctx, t,
		steps("2026-11-10T09:00:00Z", "2026-11-10T11:00:00Z", 1200),
		steps("2026-11-10T10:45:00Z", "2026-11-10T10:50:00Z", 50),
	)

	stepsType := e.typeOf(t, stepCount)
	require.Equal(t, []store.DirtyRange{
		{TypeID: stepsType.ID, From: utc(t, "2026-11-10T09:00:00Z"), To: utc(t, "2026-11-10T11:00:00Z")},
		{TypeID: stepsType.ID, From: utc(t, "2026-11-10T10:45:00Z"), To: utc(t, "2026-11-10T10:50:00Z")},
	}, withoutIDs(e.dirty(t)))

	active := e.activeJobs(t)
	require.Len(t, active, 1)
	require.Equal(t, aggregate.KindRecompute, active[0].Kind)
	require.Equal(t, store.JobQueued, active[0].Status)
	require.Equal(t, e.user.ID, active[0].UserID)
	require.Equal(t, fmt.Sprintf("aggregate:%d", e.user.ID), active[0].SerialKey)

	require.Equal(t, 1, e.runJobs(t))

	source := e.sourceOf(t, watch.Name).ID
	requireRows(t, []store.AggregateRow{
		{Start: utc(t, "2026-11-10T08:30:00Z"), Sum: 300, Count: 1, Selection: store.SelectedSource, SourceID: source},
		{Start: utc(t, "2026-11-10T09:30:00Z"), Sum: 600, Count: 1, Selection: store.SelectedSource, SourceID: source},
		{Start: utc(t, "2026-11-10T10:30:00Z"), Sum: 350, Count: 2, Selection: store.SelectedSource, SourceID: source},
	}, e.hourly(t, stepCount))
	requireRows(t, []store.AggregateRow{
		{Start: utc(t, "2026-11-09T18:30:00Z"), Sum: 1250, Count: 4, Selection: store.SelectedSource, SourceID: source, Hours: 3},
	}, e.daily(t, stepCount))

	require.Empty(t, e.dirty(t))
	require.Empty(t, e.activeJobs(t))
}

// Given a cumulative sample that lasts the day when daylight saving time ends in Europe/Paris, then
// its daily row holds 25 hours, and the sample its whole value.
func TestRecompute_DaylightSavingDayHolds25Hours(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()
	e.setTimezone(t, "Europe/Paris")

	e.importRecords(ctx, t, steps("2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00", 2500))
	e.runJobs(t)

	require.Len(t, e.hourly(t, stepCount), 25)

	days := e.daily(t, stepCount)
	require.Len(t, days, 1)
	require.Equal(t, utc(t, "2026-10-24T22:00:00Z"), days[0].Start)
	require.Equal(t, int64(25), days[0].Hours)
	require.InDelta(t, 2500, days[0].Sum, 1e-6)
}

// Given a queued job, when another batch records ranges, then no job is added. Given a running job,
// then the batch queues a new one, since the running job may have read the ranges before.
func TestRecorder_QueuesAJobUnlessOneIsQueuedAndNotRunning(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()

	e.importRecords(ctx, t, steps("2026-11-10T09:00:00Z", "2026-11-10T09:10:00Z", 10))
	require.Len(t, e.activeJobs(t), 1)

	e.importRecords(ctx, t, steps("2026-11-10T10:00:00Z", "2026-11-10T10:10:00Z", 10))
	require.Len(t, e.activeJobs(t), 1)

	claimed, _, err := e.store.Jobs().Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, aggregate.KindRecompute, claimed.Kind)

	e.importRecords(ctx, t, steps("2026-11-10T11:00:00Z", "2026-11-10T11:10:00Z", 10))

	active := e.activeJobs(t)
	require.Len(t, active, 2)
	require.Equal(t, store.JobRunning, active[0].Status)
	require.Equal(t, store.JobQueued, active[1].Status)
}

// Given ranges over three local months, when the recompute job runs, then it commits one local
// month per transaction, and deletes the ranges it read. Given a range recorded while it runs, then
// that range stays with a queued job, which recomputes it.
func TestRecompute_CommitsOneLocalMonthPerTransactionAndLeavesNewerRanges(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()
	e.setTimezone(t, "Europe/Paris")

	e.importRecords(ctx, t,
		steps("2026-08-31T23:30:00+02:00", "2026-09-01T00:30:00+02:00", 60),
		steps("2026-10-15T08:00:00+02:00", "2026-10-15T09:00:00+02:00", 500),
	)
	require.Len(t, e.dirty(t), 2)

	once := sync.Once{}
	e.hook.set(func(ctx context.Context) {
		once.Do(func() { e.importRecords(ctx, t, steps("2026-11-02T08:00:00+01:00", "2026-11-02T08:30:00+01:00", 70)) })
	})

	claimed, err := e.worker.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	// One transaction for each of August, September and October.
	require.Equal(t, 3, e.hook.count())

	stepsType := e.typeOf(t, stepCount)
	require.Equal(t, []store.DirtyRange{
		{TypeID: stepsType.ID, From: utc(t, "2026-11-02T07:00:00Z"), To: utc(t, "2026-11-02T07:30:00Z")},
	}, withoutIDs(e.dirty(t)))

	active := e.activeJobs(t)
	require.Len(t, active, 1)
	require.Equal(t, store.JobQueued, active[0].Status)

	days := e.daily(t, stepCount)
	require.Len(t, days, 3)
	require.Equal(t, []time.Time{utc(t, "2026-08-30T22:00:00Z"), utc(t, "2026-08-31T22:00:00Z"), utc(t, "2026-10-14T22:00:00Z")},
		[]time.Time{days[0].Start, days[1].Start, days[2].Start})

	e.hook.set(nil)
	require.Equal(t, 1, e.runJobs(t))
	require.Len(t, e.daily(t, stepCount), 4)
	require.Empty(t, e.dirty(t))
}

// Given aggregates computed in UTC, when the user sets the timezone Asia/Kolkata, then the
// transaction that stores it deletes the aggregates of the user, records a range for each of their
// types from the first start of their samples to the end of the last one, and queues a job. When the
// job runs, then the aggregates are those of the local hours and days of Asia/Kolkata.
func TestSetTimezone_RecomputesTheAggregatesInTheNewTimezone(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()

	e.importRecords(ctx, t,
		steps("2026-11-10T09:00:00Z", "2026-11-10T11:00:00Z", 1200),
		ingest.Record{Quantity: &ingest.QuantitySample{
			Type: heartRate, Start: "2026-11-10T09:40:00Z", End: "2026-11-10T09:40:00Z", Value: 62, Unit: "count/min", Source: watch,
		}},
	)
	e.runJobs(t)
	require.Len(t, e.hourly(t, stepCount), 2)
	require.Len(t, e.daily(t, heartRate), 1)

	_, err := preferences.SetTimezone(ctx, e.store, e.recorder, e.user.ID, "Asia/Kolkata")
	require.NoError(t, err)

	require.Empty(t, e.hourly(t, stepCount))
	require.Empty(t, e.daily(t, stepCount))
	require.Empty(t, e.hourly(t, heartRate))
	require.ElementsMatch(t, []store.DirtyRange{
		{TypeID: e.typeOf(t, stepCount).ID, From: utc(t, "2026-11-10T09:00:00Z"), To: utc(t, "2026-11-10T11:00:00Z")},
		{TypeID: e.typeOf(t, heartRate).ID, From: utc(t, "2026-11-10T09:40:00Z"), To: utc(t, "2026-11-10T09:40:00Z")},
	}, withoutIDs(e.dirty(t)))
	require.Len(t, e.activeJobs(t), 1)

	require.Equal(t, 1, e.runJobs(t))

	starts := func(rows []store.AggregateRow) []time.Time {
		var got []time.Time
		for _, row := range rows {
			got = append(got, row.Start)
		}

		return got
	}

	require.Equal(t, []time.Time{utc(t, "2026-11-10T08:30:00Z"), utc(t, "2026-11-10T09:30:00Z"), utc(t, "2026-11-10T10:30:00Z")},
		starts(e.hourly(t, stepCount)))
	require.Equal(t, []time.Time{utc(t, "2026-11-10T09:30:00Z")}, starts(e.hourly(t, heartRate)))
	require.Equal(t, []time.Time{utc(t, "2026-11-09T18:30:00Z")}, starts(e.daily(t, heartRate)))

	_, err = preferences.SetTimezone(ctx, e.store, e.recorder, e.user.ID, "Asia/Kolkata")
	require.NoError(t, err)
	require.Len(t, e.hourly(t, stepCount), 3, "setting the same timezone again recomputes nothing")
	require.Empty(t, e.activeJobs(t))
}

// requireRows checks that got holds the rows of want, their sums within a rounding error.
func requireRows(t *testing.T, want, got []store.AggregateRow) {
	t.Helper()

	require.Len(t, got, len(want))

	for i := range want {
		require.InDelta(t, want[i].Sum, got[i].Sum, 1e-9, "row %d", i)
		got[i].Sum = want[i].Sum
	}

	require.Equal(t, want, got)
}

// withoutIDs returns ranges without their ids.
func withoutIDs(ranges []store.DirtyRange) []store.DirtyRange {
	stripped := make([]store.DirtyRange, 0, len(ranges))

	for _, r := range ranges {
		r.ID = 0
		stripped = append(stripped, r)
	}

	return stripped
}

// typeOfRT is typeOf for any test.
func (e *jobEnv) typeOfRT(t testingT, identifier string) store.MetricType {
	t.Helper()

	mt, err := e.store.MetricTypes().GetByIdentifier(context.Background(), identifier)
	require.NoError(t, err)

	return mt
}

// Given the ranges of a batch, then the recorder stores those of a type that are less than an hour
// apart as one range, so that a batch of frequent samples records a few ranges, not one per sample,
// and keeps apart the ranges an hour or more apart and those of another type.
func TestRecorder_MergesTheRangesOfABatchLessThanAnHourApart(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()
	stepsType, heartType := e.typeOf(t, stepCount).ID, e.typeOf(t, heartRate).ID

	at := func(minutes int) time.Time { return jobTime.Add(time.Duration(minutes) * time.Minute) }

	require.NoError(t, e.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		return e.recorder.Record(ctx, tx, e.user.ID, []ingest.DirtyRange{
			{TypeID: heartType, From: at(30), To: at(31)},
			{TypeID: stepsType, From: at(0), To: at(10)},
			{TypeID: heartType, From: at(0), To: at(1)},
			{TypeID: heartType, From: at(91), To: at(92)},
			{TypeID: heartType, From: at(20), To: at(25)},
		})
	}))

	require.ElementsMatch(t, []store.DirtyRange{
		{TypeID: heartType, From: at(0), To: at(31)},
		{TypeID: heartType, From: at(91), To: at(92)},
		{TypeID: stepsType, From: at(0), To: at(10)},
	}, withoutIDs(e.dirty(t)))
	require.Len(t, e.activeJobs(t), 1)
}

// Given a user without a timezone, then the hourly buckets are the UTC hours, and the daily ones
// the UTC days.
func TestRecompute_UserWithoutTimezoneFollowsUTC(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)

	e.importRecords(t.Context(), t, steps("2026-11-10T09:10:00+05:30", "2026-11-10T10:10:00+05:30", 60))
	e.runJobs(t)

	source := e.sourceOf(t, watch.Name).ID
	requireRows(t, []store.AggregateRow{
		{Start: utc(t, "2026-11-10T03:00:00Z"), Sum: 20, Count: 1, Selection: store.SelectedSource, SourceID: source},
		{Start: utc(t, "2026-11-10T04:00:00Z"), Sum: 40, Count: 1, Selection: store.SelectedSource, SourceID: source},
	}, e.hourly(t, stepCount))
	requireRows(t, []store.AggregateRow{
		{Start: utc(t, "2026-11-10T00:00:00Z"), Sum: 60, Count: 2, Selection: store.SelectedSource, SourceID: source, Hours: 2},
	}, e.daily(t, stepCount))
}

// Given a recompute job whose user changes timezone before its first month, then the job stops
// without writing rows of the old timezone or deleting its ranges, which the job queued by the
// change recomputes in the new timezone.
func TestRecompute_StopsWhenTheTimezoneChangesMeanwhile(t *testing.T) {
	t.Parallel()

	e := newJobEnv(t)
	ctx := t.Context()

	e.importRecords(ctx, t, steps("2026-11-10T09:10:00Z", "2026-11-10T09:20:00Z", 60))

	once := sync.Once{}
	e.hook.set(func(ctx context.Context) {
		once.Do(func() {
			_, err := preferences.SetTimezone(ctx, e.store, e.recorder, e.user.ID, "Asia/Kolkata")
			require.NoError(t, err)
		})
	})

	claimed, err := e.worker.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	require.Empty(t, e.hourly(t, stepCount))
	require.NotEmpty(t, e.dirty(t))
	require.Len(t, e.activeJobs(t), 1)

	e.hook.set(nil)
	require.Equal(t, 1, e.runJobs(t))
	require.Equal(t, []store.AggregateRow{{
		Start: utc(t, "2026-11-10T08:30:00Z"), Sum: 60, Count: 1, Selection: store.SelectedSource, SourceID: e.sourceOf(t, watch.Name).ID,
	}}, e.hourly(t, stepCount))
	require.Empty(t, e.dirty(t))
}
