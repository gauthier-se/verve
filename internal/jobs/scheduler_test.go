package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/logging/loggingtest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

const hourlyName = "test.hourly"

var errSetSchedule = errors.New("set schedule failed")

// hourly returns a schedule that runs every hour.
func hourly() Schedule {
	return Schedule{Name: hourlyName, Kind: "test.scheduled", Every: time.Hour, Payload: json.RawMessage(`{"size":1}`)}
}

func newTestScheduler(s store.Store, clk clock.Clock, opts Options) (*Scheduler, *loggingtest.Buffer) {
	w, logs := newTestWorker(s, NewRegistry(), clk, opts)

	return w.scheduler, logs
}

func storedSchedules(ctx context.Context, t *testing.T, s store.Store) map[string]time.Time {
	t.Helper()

	schedules, err := s.Jobs().ListSchedules(ctx)
	require.NoError(t, err)

	return schedules
}

// drainJobs claims and completes the due jobs, and returns them. Completing each one lets the next
// job of its serial key be claimed.
func drainJobs(ctx context.Context, t *testing.T, s store.Store) []store.Job {
	t.Helper()

	var drained []store.Job

	for {
		job, err := s.Jobs().Claim(ctx, time.Minute)
		if errors.Is(err, store.ErrNotFound) {
			return drained
		}

		require.NoError(t, err)
		require.NoError(t, s.Jobs().Complete(ctx, job.ID, job.Claims))

		drained = append(drained, job)
	}
}

// Given a schedule that is not stored yet, then its first run is computed from now, and no job is
// enqueued.
func TestScheduler_TickStoresTheFirstRunOfANewScheduleWithoutEnqueuing(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	scheduler, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{hourly()}})

	require.NoError(t, scheduler.Tick(ctx))

	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow.Add(time.Hour)}, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))
}

// Given a recurring job declared with an interval, when its next run is due, then one job is
// enqueued and the next run is stored.
func TestScheduler_TickEnqueuesOneJobWhenTheNextRunIsDue(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	scheduler, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{hourly()}})

	require.NoError(t, scheduler.Tick(ctx))

	clk.Advance(time.Hour - time.Millisecond)
	require.NoError(t, scheduler.Tick(ctx))
	require.Empty(t, drainJobs(ctx, t, s), "the next run is not due yet")

	clk.Advance(time.Millisecond)
	require.NoError(t, scheduler.Tick(ctx))
	require.NoError(t, scheduler.Tick(ctx))

	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow.Add(2 * time.Hour)}, storedSchedules(ctx, t, s))

	due := drainJobs(ctx, t, s)
	require.Len(t, due, 1)
	require.Equal(t, "test.scheduled", due[0].Kind)
	require.JSONEq(t, `{"size":1}`, string(due[0].Payload))
	require.Equal(t, "schedule:"+hourlyName, due[0].SerialKey)
}

// Given a cron expression, then the next run is computed in the location of the worker, and not in
// the UTC time of the clock.
func TestScheduler_TickReadsCronExpressionsInTheLocationOfTheWorker(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	daily := Schedule{Name: "test.daily", Kind: "test.scheduled", Cron: "0 3 * * *", Payload: json.RawMessage(`{}`)}
	scheduler, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{daily}, Location: parisLocation(t)})

	require.NoError(t, scheduler.Tick(ctx))

	// 03:00 in Paris, which is one hour ahead of UTC in November.
	want := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	require.Equal(t, map[string]time.Time{"test.daily": want}, storedSchedules(ctx, t, s))
}

// Given no location, then cron expressions are read in the local time of the process.
func TestNewWorker_ReadsCronExpressionsInLocalTimeByDefault(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	scheduler, _ := newTestScheduler(s, clk, Options{})

	require.Same(t, time.Local, scheduler.location)
}

// Given several runs missed during downtime, then one job is enqueued after startup and the next
// run is computed from now.
func TestScheduler_TickEnqueuesOneJobForSeveralMissedRuns(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))

	clk.Advance(5*time.Hour + 30*time.Minute)

	scheduler, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{hourly()}})
	require.NoError(t, scheduler.Tick(ctx))

	require.Equal(t, map[string]time.Time{hourlyName: clk.Now().Add(time.Hour)}, storedSchedules(ctx, t, s))
	require.Len(t, drainJobs(ctx, t, s), 1)
}

// Given a restart before the next run, then the stored next run is kept and no extra job is
// enqueued.
func TestScheduler_TickKeepsTheStoredNextRunAcrossARestart(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	opts := Options{Schedules: []Schedule{hourly()}}

	beforeRestart, _ := newTestScheduler(s, clk, opts)
	require.NoError(t, beforeRestart.Tick(ctx))

	clk.Advance(10 * time.Minute)

	afterRestart, _ := newTestScheduler(s, clk, opts)
	require.NoError(t, afterRestart.Tick(ctx))

	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow.Add(time.Hour)}, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))
}

// Given a recurring job still queued or running at its next run, then no second job is enqueued, the
// run is logged as skipped and the next run moves on. Given the job ended, then the following run
// enqueues again.
func TestScheduler_TickSkipsARunWhileTheLastJobIsActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare leaves the job of the first run queued or running. The function it returns ends that
		// job, and returns it.
		prepare func(ctx context.Context, t *testing.T, s store.Store) (finish func() []store.Job)
	}{
		{
			name: "Queued",
			prepare: func(ctx context.Context, t *testing.T, s store.Store) func() []store.Job {
				t.Helper()

				return func() []store.Job { return drainJobs(ctx, t, s) }
			},
		},
		{
			name: "Running",
			prepare: func(ctx context.Context, t *testing.T, s store.Store) func() []store.Job {
				t.Helper()

				job, err := s.Jobs().Claim(ctx, 24*time.Hour)
				require.NoError(t, err)

				return func() []store.Job {
					require.NoError(t, s.Jobs().Complete(ctx, job.ID, job.Claims))

					return []store.Job{job}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			scheduler, logs := newTestScheduler(s, clk, Options{Schedules: []Schedule{hourly()}})

			require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))
			require.NoError(t, scheduler.Tick(ctx))

			finish := tt.prepare(ctx, t, s)

			clk.Advance(time.Hour)
			require.NoError(t, scheduler.Tick(ctx))

			require.Equal(t, map[string]time.Time{hourlyName: clk.Now().Add(time.Hour)}, storedSchedules(ctx, t, s))

			skipped := messageLines(t, logs, "recurring job skipped")
			require.Len(t, skipped, 1)
			require.Equal(t, hourlyName, skipped[0]["name"])

			require.Len(t, finish(), 1)
			require.Empty(t, drainJobs(ctx, t, s), "no second job was enqueued")

			clk.Advance(time.Hour)
			require.NoError(t, scheduler.Tick(ctx))
			require.Len(t, drainJobs(ctx, t, s), 1)
		})
	}
}

// Given a recurring job disabled by its configuration, then it is not enqueued and its stored state
// is removed. Given it is enabled again, then its first run is computed from now.
func TestScheduler_TickRemovesTheStoredStateOfADisabledSchedule(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()

	disabled := hourly()
	disabled.Every = 0

	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow.Add(-time.Hour)))

	scheduler, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{disabled}})
	require.NoError(t, scheduler.Tick(ctx))
	require.Empty(t, storedSchedules(ctx, t, s))

	require.NoError(t, scheduler.Tick(ctx), "there is nothing left to remove")
	require.Empty(t, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))

	enabled, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{hourly()}})
	require.NoError(t, enabled.Tick(ctx))
	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow.Add(time.Hour)}, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))
}

// Given a schedule that cannot run, then Tick reports it by name and still handles the others.
func TestScheduler_TickReportsAScheduleThatCannotRunAndHandlesTheOthers(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	broken := Schedule{Name: "test.broken", Kind: "test.scheduled", Cron: "not a cron expression", Payload: json.RawMessage(`{}`)}
	scheduler, _ := newTestScheduler(s, clk, Options{Schedules: []Schedule{broken, hourly()}})

	require.ErrorContains(t, scheduler.Tick(ctx), `schedule "test.broken"`)
	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow.Add(time.Hour)}, storedSchedules(ctx, t, s))
}

// nextRunSpy is a store that passes the next runs to setSchedule instead of storing them.
type nextRunSpy struct {
	store.Store
	setSchedule func(nextRun time.Time) error
}

func (s nextRunSpy) Jobs() store.JobsRepository {
	return nextRunSpyJobs{JobsRepository: s.Store.Jobs(), setSchedule: s.setSchedule}
}

func (s nextRunSpy) WithTx(ctx context.Context, fn func(ctx context.Context, tx store.Repositories) error) error {
	return s.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		return fn(ctx, nextRunSpyRepositories{Repositories: tx, setSchedule: s.setSchedule})
	})
}

type nextRunSpyRepositories struct {
	store.Repositories
	setSchedule func(nextRun time.Time) error
}

func (r nextRunSpyRepositories) Jobs() store.JobsRepository {
	return nextRunSpyJobs{JobsRepository: r.Repositories.Jobs(), setSchedule: r.setSchedule}
}

type nextRunSpyJobs struct {
	store.JobsRepository
	setSchedule func(nextRun time.Time) error
}

func (j nextRunSpyJobs) SetSchedule(_ context.Context, _ string, nextRun time.Time) error {
	return j.setSchedule(nextRun)
}

// Given a location other than UTC, then the next runs of a new and of a due schedule still cross
// the interface of the store in UTC.
func TestScheduler_TickPassesTheNextRunsToTheStoreInUTC(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()

	due := hourly()
	require.NoError(t, s.Jobs().SetSchedule(ctx, due.Name, workerTestNow))

	added := hourly()
	added.Name = "test.added"

	var nextRuns []time.Time

	spy := nextRunSpy{Store: s, setSchedule: func(nextRun time.Time) error {
		nextRuns = append(nextRuns, nextRun)

		return nil
	}}

	scheduler, _ := newTestScheduler(spy, clk, Options{Schedules: []Schedule{due, added}, Location: parisLocation(t)})
	require.NoError(t, scheduler.Tick(ctx))

	require.Len(t, nextRuns, 2)

	for _, nextRun := range nextRuns {
		require.Equal(t, "UTC", nextRun.Location().String())
	}
}

// Given a process that uses a worker without Run, then nothing is scheduled: a due schedule stays
// as it is, and no job is enqueued.
func TestTick_SchedulesNothing(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))

	w, _ := newTestWorker(s, NewRegistry(), clk, Options{Schedules: []Schedule{hourly()}})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.False(t, claimed)

	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow}, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))
}

// Given the next run cannot be stored, then the job of that run is not enqueued either, so that the
// next tick does not enqueue it twice.
func TestScheduler_TickEnqueuesTheJobAndStoresTheNextRunAtomically(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))

	failing := nextRunSpy{Store: s, setSchedule: func(time.Time) error { return errSetSchedule }}
	scheduler, _ := newTestScheduler(failing, clk, Options{Schedules: []Schedule{hourly()}})

	require.ErrorIs(t, scheduler.Tick(ctx), errSetSchedule)

	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow}, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))
}

// Given a due schedule waiting for the write lock of another process when the shutdown comes, then
// the shutdown does not wait for the busy timeout, and the schedule stays due.
func TestRun_Shutdown_ScheduleWaitingForTheWriteLock_DoesNotStall(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "verve.db")
	s, clk := newTestStoreAt(t, path)
	ctx := t.Context()
	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))

	other, err := sqlite.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, other.CloseShared()) })

	holder, err := other.Write().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Rollback() })

	w, logs := newTestWorker(s, NewRegistry(), clk, Options{PollInterval: noPoll, Schedules: []Schedule{hourly()}})
	stop := startWorker(t, w)

	// Long enough for the scheduler to start waiting for the write lock.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()

	stop()
	require.Less(t, time.Since(start), 2*time.Second, "the shutdown must not wait for the busy timeout")

	require.NoError(t, holder.Rollback())
	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow}, storedSchedules(ctx, t, s))
	require.Empty(t, messageLines(t, logs, "schedule recurring jobs"), "a shutdown is not a failure")
}

// Given a schedule due at startup, then Run enqueues its job and wakes up a worker, without waiting
// for a poll.
func TestWorker_RunEnqueuesARecurringJobDueAtStartup(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))

	ran := make(chan struct{})
	reg := NewRegistry()
	Register(reg, "test.scheduled", KindOptions{}, func(context.Context, Job[json.RawMessage]) error {
		close(ran)

		return nil
	})

	w, _ := newTestWorker(s, reg, clk, Options{PollInterval: noPoll, Schedules: []Schedule{hourly()}})
	startWorker(t, w)

	waitClosed(t, ran, "the recurring job did not run")
}

// Given a running worker, when the next run comes, then it enqueues the job at the next poll.
func TestWorker_RunEnqueuesARecurringJobAtItsNextRun(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()

	ran := make(chan struct{})
	reg := NewRegistry()
	Register(reg, "test.scheduled", KindOptions{}, func(context.Context, Job[json.RawMessage]) error {
		close(ran)

		return nil
	})

	w, _ := newTestWorker(s, reg, clk, Options{PollInterval: shortInterval, Schedules: []Schedule{hourly()}})
	startWorker(t, w)

	require.Eventually(t, func() bool {
		schedules, err := s.Jobs().ListSchedules(ctx)

		return err == nil && len(schedules) == 1
	}, waitFor, time.Millisecond, "the first run must be stored")

	clk.Advance(time.Hour)

	waitClosed(t, ran, "the recurring job did not run")
}
