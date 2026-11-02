package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/logging/loggingtest"
	"github.com/gauthier-se/verve/internal/store"
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
			require.Contains(t, logs.String(), `"msg":"recurring job skipped"`)
			require.Contains(t, logs.String(), `"name":"`+hourlyName+`"`)
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

// failingNextRuns is a store whose transactions fail to store a next run.
type failingNextRuns struct {
	store.Store
}

func (s failingNextRuns) WithTx(ctx context.Context, fn func(ctx context.Context, tx store.Repositories) error) error {
	return s.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		return fn(ctx, failingNextRunRepositories{Repositories: tx})
	})
}

type failingNextRunRepositories struct {
	store.Repositories
}

func (r failingNextRunRepositories) Jobs() store.JobsRepository {
	return failingNextRunJobs{JobsRepository: r.Repositories.Jobs()}
}

type failingNextRunJobs struct {
	store.JobsRepository
}

func (failingNextRunJobs) SetSchedule(context.Context, string, time.Time) error {
	return errSetSchedule
}

// Given the next run cannot be stored, then the job of that run is not enqueued either, so that the
// next tick does not enqueue it twice.
func TestScheduler_TickEnqueuesTheJobAndStoresTheNextRunAtomically(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	require.NoError(t, s.Jobs().SetSchedule(ctx, hourlyName, workerTestNow))

	scheduler, _ := newTestScheduler(failingNextRuns{Store: s}, clk, Options{Schedules: []Schedule{hourly()}})

	require.ErrorIs(t, scheduler.Tick(ctx), errSetSchedule)

	require.Equal(t, map[string]time.Time{hourlyName: workerTestNow}, storedSchedules(ctx, t, s))
	require.Empty(t, drainJobs(ctx, t, s))
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
