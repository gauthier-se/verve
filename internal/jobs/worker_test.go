package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/logging/loggingtest"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

const (
	// shortInterval is a real heartbeat or poll interval that tests can wait for.
	shortInterval = 5 * time.Millisecond
	// noPoll is a poll interval that no test waits for.
	noPoll = time.Hour
	// waitFor bounds the waits of the tests.
	waitFor = 5 * time.Second
)

var workerTestNow = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

// newTestStore returns a store on a migrated database in a temporary directory, on a fake clock.
// The store is closed at the end of the test.
func newTestStore(t *testing.T) (store.Store, *clock.Fake) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(t.Context(), db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	clk := clock.NewFake(workerTestNow)
	s := sqlite.New(db, clk)

	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	return s, clk
}

func newTestWorker(s store.Store, reg *Registry, clk clock.Clock, opts Options) (*Worker, *loggingtest.Buffer) {
	var logs loggingtest.Buffer

	return NewWorker(s, reg, clk, slog.New(slog.NewJSONHandler(&logs, nil)), opts), &logs
}

// enqueue writes a job directly through the store, as another process does.
func enqueue(ctx context.Context, t *testing.T, s store.Store, kind Kind[testPayload], name string) store.Job {
	t.Helper()

	queued, err := kind.New(testPayload{Name: name})
	require.NoError(t, err)

	job, err := s.Jobs().Enqueue(ctx, queued)
	require.NoError(t, err)

	return job
}

func getJob(ctx context.Context, t *testing.T, s store.Store, id int64) store.Job {
	t.Helper()

	job, err := s.Jobs().Get(ctx, id)
	require.NoError(t, err)

	return job
}

// requireStatus waits for the job of id to reach want.
func requireStatus(ctx context.Context, t *testing.T, s store.Store, id int64, want store.JobStatus) {
	t.Helper()

	require.Eventually(t, func() bool {
		job, err := s.Jobs().Get(ctx, id)

		return err == nil && job.Status == want
	}, waitFor, time.Millisecond, "job %d must become %s", id, want)
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(waitFor):
		t.Fatal(what)
	}
}

// startWorker runs w in the background. It returns a function that stops it and waits for Run to
// return, which also runs at the end of the test.
func startWorker(t *testing.T, w *Worker) func() {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		w.Run(ctx)
	}()

	stop := func() {
		t.Helper()
		cancel()
		waitClosed(t, done, "Run did not return")
	}

	t.Cleanup(stop)

	return stop
}

// tickInBackground calls Tick in the background and returns a channel closed once it returns.
func tickInBackground(ctx context.Context, t *testing.T, w *Worker) <-chan struct{} {
	t.Helper()

	done := make(chan struct{})

	go func() {
		defer close(done)

		claimed, err := w.Tick(ctx)
		if err != nil || !claimed {
			t.Errorf("Tick: claimed %t, error %v", claimed, err)
		}
	}()

	return done
}

// finishedLines returns the lines logged for the end of a job.
func finishedLines(t *testing.T, logs *loggingtest.Buffer) []map[string]any {
	t.Helper()

	var lines []map[string]any

	for _, line := range logs.Lines(t) {
		if line["msg"] == "job finished" {
			lines = append(lines, line)
		}
	}

	return lines
}

// Given a registered kind and a queued job, then a worker runs its handler with the decoded payload
// and the job ends completed with finished_at set.
func TestTick_QueuedJob_RunsHandlerAndCompletes(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()

	var got testPayload

	kind := Register(reg, "test.kind", KindOptions{}, func(_ context.Context, job Job[testPayload]) error {
		got = job.Payload

		clk.Advance(time.Second)

		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, _ := newTestWorker(s, reg, clk, Options{})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, testPayload{Name: "first"}, got)

	job := getJob(ctx, t, s, queued.ID)
	require.Equal(t, store.JobCompleted, job.Status)
	require.Equal(t, workerTestNow.Add(time.Second), job.FinishedAt)
}

// Given no due job, then Tick claims nothing.
func TestTick_NoDueJob_ClaimsNothing(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	w, _ := newTestWorker(s, NewRegistry(), clk, Options{})

	claimed, err := w.Tick(t.Context())
	require.NoError(t, err)
	require.False(t, claimed)
}

// Given a claim that fails, then Tick returns the error.
func TestTick_ClaimFails_ReturnsTheError(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	w, _ := newTestWorker(s, NewRegistry(), clk, Options{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	claimed, err := w.Tick(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, claimed)
}

// Given a handler that returns an error, then the attempt is recorded as failed with that error.
func TestTick_HandlerFails_RecordsTheFailedAttempt(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return errors.New("handler failed")
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, _ := newTestWorker(s, reg, clk, Options{})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	job := getJob(ctx, t, s, queued.ID)
	require.Equal(t, store.JobFailed, job.Status)
	require.Equal(t, int64(1), job.Attempts)
	require.Equal(t, "handler failed", job.LastError)
}

// Given 2 workers and 3 slow jobs, then at most 2 run at the same time, and all of them end.
func TestRun_TwoWorkersAndThreeSlowJobs_RunAtMostTwoAtOnce(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()

	var running, highest atomic.Int64

	release := make(chan struct{})

	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		now := running.Add(1)
		for {
			seen := highest.Load()
			if now <= seen || highest.CompareAndSwap(seen, now) {
				break
			}
		}

		<-release
		running.Add(-1)

		return nil
	})

	queued := []store.Job{
		enqueue(ctx, t, s, kind, "first"),
		enqueue(ctx, t, s, kind, "second"),
		enqueue(ctx, t, s, kind, "third"),
	}

	w, _ := newTestWorker(s, reg, clk, Options{Workers: 2, PollInterval: shortInterval})
	startWorker(t, w)

	require.Eventually(t, func() bool { return running.Load() == 2 }, waitFor, time.Millisecond)
	require.Never(t, func() bool { return running.Load() > 2 }, 50*time.Millisecond, time.Millisecond)

	close(release)

	for _, job := range queued {
		requireStatus(ctx, t, s, job.ID, store.JobCompleted)
	}

	require.Equal(t, int64(2), highest.Load())
}

// idleWorker returns a running worker that has finished a job and has nothing left to do, so that
// only a wake up or a poll brings it another job. Its one worker runs kind.
func idleWorker(ctx context.Context, t *testing.T, s store.Store, clk clock.Clock, poll time.Duration) (*Worker, Kind[testPayload]) {
	t.Helper()

	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return nil
	})
	first := enqueue(ctx, t, s, kind, "first")

	w, _ := newTestWorker(s, reg, clk, Options{Workers: 1, PollInterval: poll})
	startWorker(t, w)
	requireStatus(ctx, t, s, first.ID, store.JobCompleted)

	return w, kind
}

// Given a job enqueued through the worker's enqueuer, then a worker wakes up without waiting for
// the poll interval.
func TestEnqueue_WakesAWorkerWithoutWaitingForThePoll(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	w, kind := idleWorker(ctx, t, s, clk, noPoll)

	queued, err := kind.New(testPayload{Name: "second"})
	require.NoError(t, err)

	job, err := w.Enqueue(ctx, s, queued)
	require.NoError(t, err)

	requireStatus(ctx, t, s, job.ID, store.JobCompleted)
}

// Given a job enqueued on the repositories of a WithTx, then a worker wakes up once it is committed.
func TestEnqueue_OnTheRepositoriesOfAWithTx_WakesAWorkerAfterTheCommit(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	w, kind := idleWorker(ctx, t, s, clk, noPoll)

	queued, err := kind.New(testPayload{Name: "second"})
	require.NoError(t, err)

	var job store.Job

	err = s.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		var err error

		job, err = w.Enqueue(ctx, tx, queued)

		return err
	})
	require.NoError(t, err)

	requireStatus(ctx, t, s, job.ID, store.JobCompleted)
}

// Given a job without a run time, then Enqueue makes it due from the current time, so that it
// does not jump ahead of the jobs already waiting.
func TestEnqueue_NoRunAt_IsDueFromNow(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	w, _ := newTestWorker(s, NewRegistry(), clk, Options{})

	job, err := w.Enqueue(t.Context(), s, store.NewJob{Kind: "test.kind", Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Equal(t, workerTestNow, job.RunAt)
}

// Given a job written by another process, then it is picked up within the poll interval.
func TestRun_JobWrittenByAnotherProcess_IsPickedUpByThePoll(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	_, kind := idleWorker(ctx, t, s, clk, shortInterval)

	job := enqueue(ctx, t, s, kind, "second")

	requireStatus(ctx, t, s, job.ID, store.JobCompleted)
}

// Given a handler running longer than the lease, then heartbeats keep its lease and no other worker
// claims it.
func TestTick_HandlerRunningLongerThanTheLease_HeartbeatsKeepIt(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		close(started)
		<-release

		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")

	w, _ := newTestWorker(s, reg, clk, Options{Lease: lease, HeartbeatInterval: shortInterval})
	done := tickInBackground(ctx, t, w)

	waitClosed(t, started, "the handler did not start")

	for range 4 {
		clk.Advance(20 * time.Second)

		want := clk.Now().Add(lease)

		require.Eventually(t, func() bool {
			job, err := s.Jobs().Get(ctx, queued.ID)

			return err == nil && job.LockedUntil.Equal(want)
		}, waitFor, time.Millisecond, "a heartbeat must renew the lease from now")
	}

	other, _ := newTestWorker(s, reg, clk, Options{})

	claimed, err := other.Tick(ctx)
	require.NoError(t, err)
	require.False(t, claimed, "the lease must keep the job from another worker")

	close(release)
	waitClosed(t, done, "Tick did not return")

	job := getJob(ctx, t, s, queued.ID)
	require.Equal(t, store.JobCompleted, job.Status)
	require.Equal(t, int64(1), job.Claims)
	require.Zero(t, job.LostLeases)
}

// Given a heartbeat that fails with store.ErrConflict, then the handler context is cancelled and
// its result is not written.
func TestTick_HeartbeatConflict_CancelsHandlerAndWritesNoResult(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan struct{})
	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, _ Job[testPayload]) error {
		close(started)
		<-ctx.Done()

		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")

	w, logs := newTestWorker(s, reg, clk, Options{HeartbeatInterval: shortInterval})
	done := tickInBackground(ctx, t, w)

	waitClosed(t, started, "the handler did not start")

	// Another claim would race the heartbeats for the expired lease. Releasing the job also takes
	// it away from this worker, and the next heartbeat gets store.ErrConflict.
	running := getJob(ctx, t, s, queued.ID)
	require.NoError(t, s.Jobs().Release(ctx, running.ID, running.Claims))

	waitClosed(t, done, "the handler context must be cancelled")

	require.Equal(t, store.JobQueued, getJob(ctx, t, s, queued.ID).Status)

	lines := finishedLines(t, logs)
	require.Len(t, lines, 1)
	require.Equal(t, "lease_lost", lines[0]["status"])
}

// Given a handler that panics, then the worker survives, logs the panic and records the attempt as
// failed.
func TestTick_HandlerPanics_LogsItAndRecordsTheFailedAttempt(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		panic("boom")
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, logs := newTestWorker(s, reg, clk, Options{})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	job := getJob(ctx, t, s, queued.ID)
	require.Equal(t, store.JobFailed, job.Status)
	require.Equal(t, int64(1), job.Attempts)
	require.Equal(t, "handler panicked", job.LastError)

	var panics []map[string]any

	for _, line := range logs.Lines(t) {
		if line["msg"] == "job handler panicked" {
			panics = append(panics, line)
		}
	}

	require.Len(t, panics, 1)
	require.Equal(t, "ERROR", panics[0]["level"])
	require.Equal(t, "boom", panics[0]["panic"])
	require.Equal(t, "test.kind", panics[0]["kind"])
	require.NotEmpty(t, panics[0]["stack"])
}

// Given a job that no handler can run, then it is marked failed with a clear error.
func TestTick_JobThatNoHandlerCanRun_IsMarkedFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		payload string
		want    string
	}{
		{name: "unknown kind", kind: "test.unknown", payload: `{}`, want: "unknown job kind"},
		{name: "malformed payload", kind: "test.kind", payload: `{"name":5}`, want: "malformed job payload"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			reg := NewRegistry()

			Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
				return nil
			})

			queued, err := s.Jobs().Enqueue(ctx, store.NewJob{Kind: tt.kind, Payload: json.RawMessage(tt.payload)})
			require.NoError(t, err)

			w, _ := newTestWorker(s, reg, clk, Options{})

			claimed, err := w.Tick(ctx)
			require.NoError(t, err)
			require.True(t, claimed)

			job := getJob(ctx, t, s, queued.ID)
			require.Equal(t, store.JobFailed, job.Status)
			require.Equal(t, tt.want, job.LastError)
		})
	}
}

// Given a finished job, then one log line carries its kind, id, status and duration, and never its
// payload.
func TestTick_FinishedJob_LogsOneLineWithoutThePayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handlerErr error
		wantStatus store.JobStatus
		wantLevel  string
	}{
		{name: "completed", wantStatus: store.JobCompleted, wantLevel: "INFO"},
		{name: "failed", handlerErr: errors.New("handler failed"), wantStatus: store.JobFailed, wantLevel: "WARN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			const secret = "payload that must stay out of the logs"

			s, clk := newTestStore(t)
			ctx := t.Context()
			reg := NewRegistry()
			kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
				clk.Advance(3 * time.Second)

				return tt.handlerErr
			})
			queued := enqueue(ctx, t, s, kind, secret)
			w, logs := newTestWorker(s, reg, clk, Options{})

			_, err := w.Tick(ctx)
			require.NoError(t, err)

			lines := finishedLines(t, logs)
			require.Len(t, lines, 1)
			require.Equal(t, tt.wantLevel, lines[0]["level"])
			require.Equal(t, "test.kind", lines[0]["kind"])
			require.InEpsilon(t, float64(queued.ID), lines[0]["id"], 0)
			require.Equal(t, string(tt.wantStatus), lines[0]["status"])
			require.InEpsilon(t, float64(3*time.Second), lines[0]["duration"], 0)
			require.NotContains(t, fmt.Sprint(logs.Lines(t)), secret)
		})
	}
}

// Given shutdown, then the worker claims no more jobs, cancels the running handler and returns once
// it stopped. The interrupted job is not failed: the lease recovers it.
func TestRun_Shutdown_CancelsTheHandlerAndClaimsNoMoreJobs(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan struct{})
	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, _ Job[testPayload]) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	})
	interrupted := enqueue(ctx, t, s, kind, "first")
	waiting := enqueue(ctx, t, s, kind, "second")

	w, logs := newTestWorker(s, reg, clk, Options{Workers: 1, PollInterval: shortInterval})
	stop := startWorker(t, w)

	waitClosed(t, started, "the handler did not start")
	stop()

	job := getJob(ctx, t, s, interrupted.ID)
	require.Equal(t, store.JobRunning, job.Status)
	require.Zero(t, job.Attempts)
	require.Equal(t, store.JobQueued, getJob(ctx, t, s, waiting.ID).Status)

	lines := finishedLines(t, logs)
	require.Len(t, lines, 1)
	require.Equal(t, "interrupted", lines[0]["status"])
}

// Given shutdown and a handler that still finishes its work, then the job ends completed, because
// running it again would be wasted.
func TestRun_Shutdown_HandlerThatFinishes_CompletesTheJob(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan struct{})
	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, _ Job[testPayload]) error {
		close(started)
		<-ctx.Done()

		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")

	w, _ := newTestWorker(s, reg, clk, Options{Workers: 1, PollInterval: shortInterval})
	stop := startWorker(t, w)

	waitClosed(t, started, "the handler did not start")
	stop()

	require.Equal(t, store.JobCompleted, getJob(ctx, t, s, queued.ID).Status)
}

// flakyStore is a store whose first claim fails.
type flakyStore struct {
	store.Store

	failed atomic.Bool
}

func (s *flakyStore) Jobs() store.JobsRepository {
	return flakyJobs{JobsRepository: s.Store.Jobs(), store: s}
}

type flakyJobs struct {
	store.JobsRepository

	store *flakyStore
}

func (j flakyJobs) Claim(ctx context.Context, lease time.Duration) (store.Job, error) {
	if j.store.failed.CompareAndSwap(false, true) {
		return store.Job{}, errors.New("database is locked")
	}

	return j.JobsRepository.Claim(ctx, lease)
}

// Given a claim that fails, then Run logs a warning and tries again at the next poll.
func TestRun_ClaimFails_LogsAndTriesAgain(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")

	w, logs := newTestWorker(&flakyStore{Store: s}, reg, clk, Options{Workers: 1, PollInterval: shortInterval})
	stop := startWorker(t, w)

	requireStatus(ctx, t, s, queued.ID, store.JobCompleted)
	stop()

	var warnings int

	for _, line := range logs.Lines(t) {
		if line["level"] == "WARN" {
			require.Equal(t, "claim job", line["msg"])

			warnings++
		}
	}

	require.Equal(t, 1, warnings)
}
