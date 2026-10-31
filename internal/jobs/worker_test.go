package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
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

	return newTestStoreAt(t, filepath.Join(t.TempDir(), "verve.db"))
}

// newTestStoreAt is newTestStore with the database at path.
func newTestStoreAt(t *testing.T, path string) (store.Store, *clock.Fake) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), path)
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

// requireCompleted waits for the job of id to end completed.
func requireCompleted(ctx context.Context, t *testing.T, s store.Store, id int64) {
	t.Helper()

	require.Eventually(t, func() bool {
		job, err := s.Jobs().Get(ctx, id)

		return err == nil && job.Status == store.JobCompleted
	}, waitFor, time.Millisecond, "job %d must complete", id)
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

// messageLines returns the lines logged with the message msg.
func messageLines(t *testing.T, logs *loggingtest.Buffer, msg string) []map[string]any {
	t.Helper()

	var lines []map[string]any

	for _, line := range logs.Lines(t) {
		if line["msg"] == msg {
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

// requireTick calls Tick and checks that it claimed a job.
func requireTick(ctx context.Context, t *testing.T, w *Worker) {
	t.Helper()

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
}

// requireNoTick calls Tick and checks that it found no due job.
func requireNoTick(ctx context.Context, t *testing.T, w *Worker) {
	t.Helper()

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.False(t, claimed)
}

// Given zero options, then a worker takes the defaults of the spec.
func TestNewWorker_ZeroOptions_TakeTheDefaults(t *testing.T) {
	t.Parallel()

	w, _ := newTestWorker(nil, NewRegistry(), clock.NewFake(workerTestNow), Options{})

	require.Equal(t, Options{
		Workers:           2,
		Lease:             60 * time.Second,
		HeartbeatInterval: 20 * time.Second,
		PollInterval:      5 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		FinishTimeout:     time.Second,
	}, w.opts)
}

// Given a handler that returns an error, then the job returns to queued with attempts incremented,
// last_error set and run_at at now plus 30 seconds doubled per attempt.
func TestTick_HandlerFails_RetriesWithBackoff(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return errors.New("handler failed")
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, _ := newTestWorker(s, reg, clk, Options{})

	for attempt, delay := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		requireTick(ctx, t, w)

		job := getJob(ctx, t, s, queued.ID)
		require.Equal(t, store.JobQueued, job.Status)
		require.Equal(t, int64(attempt+1), job.Attempts)
		require.Equal(t, "handler failed", job.LastError)
		require.Equal(t, clk.Now().Add(delay), job.RunAt)
		require.True(t, job.LockedUntil.IsZero())

		requireNoTick(ctx, t, w)

		clk.Advance(delay)
	}
}

// Given a job that reached the maximum attempts of its kind, then it is marked failed and does not
// run again.
func TestTick_JobThatReachedItsMaximumAttempts_IsMarkedFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts KindOptions
		want int64
	}{
		{name: "default", want: 5},
		{name: "of the kind", opts: KindOptions{MaxAttempts: 2}, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			reg := NewRegistry()

			var runs int64

			kind := Register(reg, "test.kind", tt.opts, func(context.Context, Job[testPayload]) error {
				runs++

				return errors.New("handler failed")
			})
			queued := enqueue(ctx, t, s, kind, "first")
			w, _ := newTestWorker(s, reg, clk, Options{})

			for range tt.want {
				requireTick(ctx, t, w)
				clk.Advance(time.Hour)
			}

			requireNoTick(ctx, t, w)
			require.Equal(t, tt.want, runs)

			job := getJob(ctx, t, s, queued.ID)
			require.Equal(t, store.JobFailed, job.Status)
			require.Equal(t, tt.want, job.Attempts)
			require.Equal(t, "handler failed", job.LastError)
			require.False(t, job.FinishedAt.IsZero())
		})
	}
}

// Given a handler that returns jobs.Permanent(err), even wrapped, then the job is marked failed at
// once.
func TestTick_PermanentError_MarksTheJobFailedAtOnce(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return fmt.Errorf("read archive: %w", Permanent(errors.New("corrupted file")))
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, _ := newTestWorker(s, reg, clk, Options{})

	requireTick(ctx, t, w)

	job := getJob(ctx, t, s, queued.ID)
	require.Equal(t, store.JobFailed, job.Status)
	require.Equal(t, int64(1), job.Attempts)
	require.Equal(t, "read archive: corrupted file", job.LastError)
}

// Given a very long error, then last_error keeps at most 1,000 characters, whether the job is
// retried or failed.
func TestTick_VeryLongError_KeepsAtMost1000Characters(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 1500)

	tests := []struct {
		name       string
		handlerErr error
		wantStatus store.JobStatus
	}{
		{name: "retried", handlerErr: errors.New(long), wantStatus: store.JobQueued},
		{name: "failed", handlerErr: Permanent(errors.New(long)), wantStatus: store.JobFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			reg := NewRegistry()
			kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
				return tt.handlerErr
			})
			queued := enqueue(ctx, t, s, kind, "first")
			w, _ := newTestWorker(s, reg, clk, Options{})

			requireTick(ctx, t, w)

			job := getJob(ctx, t, s, queued.ID)
			require.Equal(t, tt.wantStatus, job.Status)
			require.Equal(t, strings.Repeat("é", 1000), job.LastError)
		})
	}
}

// Given a queued job that is cancelled, then it never runs.
func TestTick_CancelledQueuedJob_NeverRuns(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	called := false
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		called = true

		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, _ := newTestWorker(s, reg, clk, Options{})

	require.NoError(t, s.Jobs().Cancel(ctx, queued.ID))

	requireNoTick(ctx, t, w)
	require.False(t, called)
	require.Equal(t, store.JobCancelled, getJob(ctx, t, s, queued.ID).Status)
}

// Given a running job that is cancelled, then its context is cancelled within one heartbeat and the
// job ends cancelled, not retried, whatever the handler returns.
func TestTick_CancelledRunningJob_CancelsTheHandlerAndEndsCancelled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result func(ctx context.Context) error
	}{
		{name: "handler returns the context error", result: context.Cause},
		{name: "handler returns nil", result: func(context.Context) error { return nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx := t.Context()
			reg := NewRegistry()
			started := make(chan struct{})
			kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, _ Job[testPayload]) error {
				close(started)
				<-ctx.Done()

				return tt.result(ctx)
			})
			queued := enqueue(ctx, t, s, kind, "first")

			w, logs := newTestWorker(s, reg, clk, Options{HeartbeatInterval: shortInterval})
			done := tickInBackground(ctx, t, w)

			waitClosed(t, started, "the handler did not start")
			require.NoError(t, s.Jobs().Cancel(ctx, queued.ID))
			waitClosed(t, done, "the handler context must be cancelled within one heartbeat")

			job := getJob(ctx, t, s, queued.ID)
			require.Equal(t, store.JobCancelled, job.Status)
			require.Zero(t, job.Attempts)
			require.False(t, job.FinishedAt.IsZero())

			lines := messageLines(t, logs, "job finished")
			require.Len(t, lines, 1)
			require.Equal(t, "INFO", lines[0]["level"])
			require.Equal(t, "cancelled", lines[0]["status"])
		})
	}
}

// Given a cancellation requested while the handler fails, before a heartbeat reports it, then the
// job ends cancelled and does not run again.
func TestTick_CancelRequestedWhileTheHandlerFails_NeverRunsAgain(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	runs := 0
	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, job Job[testPayload]) error {
		runs++

		if err := s.Jobs().Cancel(ctx, job.ID); err != nil {
			return err
		}

		return errors.New("handler failed")
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, _ := newTestWorker(s, reg, clk, Options{})

	requireTick(ctx, t, w)
	clk.Advance(time.Hour)
	requireNoTick(ctx, t, w)

	require.Equal(t, 1, runs)
	require.Equal(t, store.JobCancelled, getJob(ctx, t, s, queued.ID).Status)
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
		requireCompleted(ctx, t, s, job.ID)
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
	requireCompleted(ctx, t, s, first.ID)

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

	requireCompleted(ctx, t, s, job.ID)
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

	requireCompleted(ctx, t, s, job.ID)
}

// Given a job written by another process, then it is picked up within the poll interval.
func TestRun_JobWrittenByAnotherProcess_IsPickedUpByThePoll(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	_, kind := idleWorker(ctx, t, s, clk, shortInterval)

	job := enqueue(ctx, t, s, kind, "second")

	requireCompleted(ctx, t, s, job.ID)
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

	lines := messageLines(t, logs, "job finished")
	require.Len(t, lines, 1)
	require.Equal(t, "lease_lost", lines[0]["status"])
}

// Given a handler that panics, then the worker survives, logs the panic and records the attempt as
// failed: the job is retried like one whose handler returned an error.
func TestTick_HandlerPanics_LogsItAndRecordsTheFailedAttempt(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()

	const secret = "panic value that must stay out of the logs"

	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		panic(secret)
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, logs := newTestWorker(s, reg, clk, Options{})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	job := getJob(ctx, t, s, queued.ID)
	require.Equal(t, store.JobQueued, job.Status)
	require.Equal(t, int64(1), job.Attempts)
	require.Equal(t, "handler panicked", job.LastError)

	panics := messageLines(t, logs, "job handler panicked")
	require.Len(t, panics, 1)
	require.Equal(t, "ERROR", panics[0]["level"])
	require.Equal(t, "string", panics[0]["panic_type"])
	require.Equal(t, "test.kind", panics[0]["kind"])
	require.NotEmpty(t, panics[0]["stack"])
	require.NotContains(t, fmt.Sprint(logs.Lines(t)), secret)
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
		wantStatus string
		wantLevel  string
	}{
		{name: "completed", wantStatus: "completed", wantLevel: "INFO"},
		{name: "retried", handlerErr: errors.New("handler failed"), wantStatus: "retrying", wantLevel: "WARN"},
		{name: "failed", handlerErr: Permanent(errors.New("handler failed")), wantStatus: "failed", wantLevel: "WARN"},
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

			lines := messageLines(t, logs, "job finished")
			require.Len(t, lines, 1)
			require.Equal(t, tt.wantLevel, lines[0]["level"])
			require.Equal(t, "test.kind", lines[0]["kind"])
			require.InEpsilon(t, float64(queued.ID), lines[0]["id"], 0)
			require.Equal(t, tt.wantStatus, lines[0]["status"])
			require.InEpsilon(t, float64(3*time.Second), lines[0]["duration"], 0)
			require.NotContains(t, fmt.Sprint(logs.Lines(t)), secret)
		})
	}
}

// Given shutdown, then the worker claims no more jobs, cancels the running handler and returns once
// it stopped. The interrupted job returns to queued with its attempts unchanged.
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
	require.Equal(t, store.JobQueued, job.Status)
	require.Zero(t, job.Attempts)
	require.Zero(t, job.LostLeases)
	require.Equal(t, store.JobQueued, getJob(ctx, t, s, waiting.ID).Status)

	lines := messageLines(t, logs, "job finished")
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

// Given shutdown and a handler that stops long after the other one, then both jobs return to
// queued: the deadline of the final writes starts once the handlers have stopped, not at the first
// write.
func TestRun_Shutdown_HandlerThatStopsLate_StillReturnsItsJobToQueued(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()

	var running atomic.Int64

	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, job Job[testPayload]) error {
		running.Add(1)
		<-ctx.Done()

		if job.Payload.Name == "slow" {
			time.Sleep(100 * time.Millisecond)
		}

		return ctx.Err()
	})
	fast := enqueue(ctx, t, s, kind, "fast")
	slow := enqueue(ctx, t, s, kind, "slow")

	w, _ := newTestWorker(s, reg, clk, Options{Workers: 2, PollInterval: shortInterval, FinishTimeout: 20 * time.Millisecond})
	stop := startWorker(t, w)

	require.Eventually(t, func() bool { return running.Load() == 2 }, waitFor, time.Millisecond)
	stop()

	require.Equal(t, store.JobQueued, getJob(ctx, t, s, fast.ID).Status)
	require.Equal(t, store.JobQueued, getJob(ctx, t, s, slow.ID).Status)
}

// Given a handler that ignores its context during shutdown, then the worker gives up after the
// shutdown timeout and the lease recovers the job.
func TestRun_Shutdown_HandlerThatIgnoresItsContext_LeavesTheJobToTheLease(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		close(started)
		<-release

		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")

	w, logs := newTestWorker(s, reg, clk, Options{
		Workers:         1,
		Lease:           lease,
		PollInterval:    shortInterval,
		ShutdownTimeout: 20 * time.Millisecond,
	})
	stop := startWorker(t, w)

	waitClosed(t, started, "the handler did not start")
	stop()

	require.Equal(t, store.JobRunning, getJob(ctx, t, s, queued.ID).Status)

	warnings := messageLines(t, logs, "job handlers did not stop")
	require.Len(t, warnings, 1)
	require.Equal(t, "WARN", warnings[0]["level"])

	clk.Advance(lease)

	recovered, err := s.Jobs().Claim(ctx, lease)
	require.NoError(t, err)
	require.Equal(t, queued.ID, recovered.ID)
	require.Equal(t, int64(1), recovered.LostLeases)
}

// Given shutdown and another process holding the write lock, when interrupted jobs cannot return to
// queued, then the workers give up on their writes after 1 second in total, log a warning and no
// error, and the lease recovers the jobs.
func TestRun_Shutdown_WriteLockHeld_GivesUpAfterOneSecondInTotal(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

	path := filepath.Join(t.TempDir(), "verve.db")
	s, clk := newTestStoreAt(t, path)
	ctx := t.Context()
	reg := NewRegistry()

	var running atomic.Int64

	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, _ Job[testPayload]) error {
		running.Add(1)
		<-ctx.Done()

		return ctx.Err()
	})
	first := enqueue(ctx, t, s, kind, "first")
	second := enqueue(ctx, t, s, kind, "second")

	w, logs := newTestWorker(s, reg, clk, Options{Workers: 2, Lease: lease, PollInterval: shortInterval})
	stop := startWorker(t, w)

	require.Eventually(t, func() bool { return running.Load() == 2 }, waitFor, time.Millisecond)

	other, err := sqlite.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, other.CloseShared()) })

	holder, err := other.Write().BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Rollback() })

	start := time.Now()

	stop()
	require.Less(t, time.Since(start), 2*time.Second, "the waits of the workers must not add up")

	require.NoError(t, holder.Rollback())

	require.Equal(t, store.JobRunning, getJob(ctx, t, s, first.ID).Status)
	require.Equal(t, store.JobRunning, getJob(ctx, t, s, second.ID).Status)

	for _, line := range logs.Lines(t) {
		require.NotEqual(t, "ERROR", line["level"])
	}

	lines := messageLines(t, logs, "job finished")
	require.Len(t, lines, 2)

	for _, line := range lines {
		require.Equal(t, "WARN", line["level"])
		require.Equal(t, "result_lost", line["status"])
	}

	clk.Advance(lease)

	recovered, err := s.Jobs().Claim(ctx, lease)
	require.NoError(t, err)
	require.Equal(t, int64(1), recovered.LostLeases)
}

// lockedResults is a store that cannot write the end of a job, as when another process holds the
// write lock for longer than the busy timeout.
type lockedResults struct {
	store.Store
}

func (s lockedResults) Jobs() store.JobsRepository {
	return lockedResultsJobs{JobsRepository: s.Store.Jobs()}
}

type lockedResultsJobs struct {
	store.JobsRepository
}

func (lockedResultsJobs) Complete(context.Context, int64, int64) error {
	return errors.New("database is locked")
}

// Given a result that cannot be written, then the log line of the job says so, and the job stays
// running for its lease to recover.
func TestTick_ResultCannotBeWritten_LeavesTheJobToTheLease(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return nil
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, logs := newTestWorker(lockedResults{Store: s}, reg, clk, Options{})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	require.Equal(t, store.JobRunning, getJob(ctx, t, s, queued.ID).Status)

	lines := messageLines(t, logs, "job finished")
	require.Len(t, lines, 1)
	require.Equal(t, "WARN", lines[0]["level"])
	require.Equal(t, "result_lost", lines[0]["status"])
	require.Equal(t, "database is locked", lines[0]["error"])
}

// Given a job taken away before the handler returns and before any heartbeat notices, then the
// write of its result is refused and the log line says the lease is lost.
func TestTick_JobLostBeforeItsResultIsWritten_LogsTheLostLease(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, job Job[testPayload]) error {
		running, err := s.Jobs().Get(ctx, job.ID)
		if err != nil {
			return err
		}

		return s.Jobs().Release(ctx, running.ID, running.Claims)
	})
	queued := enqueue(ctx, t, s, kind, "first")
	w, logs := newTestWorker(s, reg, clk, Options{})

	claimed, err := w.Tick(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	require.Equal(t, store.JobQueued, getJob(ctx, t, s, queued.ID).Status)

	lines := messageLines(t, logs, "job finished")
	require.Len(t, lines, 1)
	require.Equal(t, "lease_lost", lines[0]["status"])
}

// failingHeartbeats is a store whose first heartbeats fail, as when another process holds the write
// lock for longer than the busy timeout.
type failingHeartbeats struct {
	store.Store

	failures int64
	calls    *atomic.Int64
}

func (s failingHeartbeats) Jobs() store.JobsRepository {
	return failingHeartbeatsJobs{JobsRepository: s.Store.Jobs(), store: s}
}

type failingHeartbeatsJobs struct {
	store.JobsRepository

	store failingHeartbeats
}

func (j failingHeartbeatsJobs) Heartbeat(ctx context.Context, id, claims int64, lease time.Duration) (bool, error) {
	if j.store.calls.Add(1) <= j.store.failures {
		return false, errors.New("database is locked")
	}

	return j.JobsRepository.Heartbeat(ctx, id, claims, lease)
}

// Given a heartbeat that fails although the job is still held, then the worker logs a warning and
// tries again at the next one, the handler keeps running and its result is written.
func TestTick_HeartbeatFailsOnce_KeepsTheHandlerRunning(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, _ Job[testPayload]) error {
		close(started)

		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	queued := enqueue(ctx, t, s, kind, "first")

	var calls atomic.Int64

	flaky := failingHeartbeats{Store: s, failures: 1, calls: &calls}
	w, logs := newTestWorker(flaky, reg, clk, Options{HeartbeatInterval: shortInterval})
	done := tickInBackground(ctx, t, w)

	waitClosed(t, started, "the handler did not start")
	require.Eventually(t, func() bool { return calls.Load() >= 3 }, waitFor, time.Millisecond,
		"the heartbeats must go on after the failure")

	close(release)
	waitClosed(t, done, "Tick did not return")

	require.Equal(t, store.JobCompleted, getJob(ctx, t, s, queued.ID).Status)

	warnings := messageLines(t, logs, "renew job lease")
	require.Len(t, warnings, 1)
	require.Equal(t, "WARN", warnings[0]["level"])
	require.Equal(t, "test.kind", warnings[0]["kind"])
	require.Equal(t, "database is locked", warnings[0]["error"])
}

// Given heartbeats that keep failing, then the handler keeps running while the lease holds, and is
// cancelled once it has run out, because another worker can then claim the job.
func TestTick_HeartbeatsKeepFailing_CancelsTheHandlerOnceTheLeaseRunsOut(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

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

	var calls atomic.Int64

	flaky := failingHeartbeats{Store: s, failures: math.MaxInt64, calls: &calls}
	w, logs := newTestWorker(flaky, reg, clk, Options{Lease: lease, HeartbeatInterval: shortInterval})
	done := tickInBackground(ctx, t, w)

	waitClosed(t, started, "the handler did not start")
	require.Eventually(t, func() bool { return calls.Load() >= 3 }, waitFor, time.Millisecond,
		"the handler must keep running while the lease holds")

	clk.Advance(lease)
	waitClosed(t, done, "the handler must be cancelled once the lease has run out")

	require.Equal(t, store.JobRunning, getJob(ctx, t, s, queued.ID).Status)

	lines := messageLines(t, logs, "job finished")
	require.Len(t, lines, 1)
	require.Equal(t, "lease_lost", lines[0]["status"])
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

	requireCompleted(ctx, t, s, queued.ID)
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
