package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// testSerialKey is the serial key of the in-process jobs of the tests, the import lock of a user.
const testSerialKey = "import:7"

// newKeyedJob returns a job of kind for the user and serial key of the tests.
func newKeyedJob(t *testing.T, kind Kind[testPayload], name string) store.NewJob {
	t.Helper()

	job, err := kind.New(testPayload{Name: name})
	require.NoError(t, err)

	job.UserID = 7
	job.SerialKey = testSerialKey

	return job
}

// holdSerialKey enqueues a job of kind with the serial key of the tests and claims it, as the
// worker of the server does, and returns it.
func holdSerialKey(ctx context.Context, t *testing.T, s store.Store, kind Kind[testPayload]) store.Job {
	t.Helper()

	_, err := s.Jobs().Enqueue(ctx, newKeyedJob(t, kind, "holder"))
	require.NoError(t, err)

	holder, err := s.Jobs().Claim(ctx, time.Minute)
	require.NoError(t, err)

	return holder
}

// runInProcessInBackground calls RunInProcess in the background, and returns a channel that
// receives its error.
func runInProcessInBackground(ctx context.Context, w *Worker, job store.NewJob, onWait func()) <-chan error {
	done := make(chan error, 1)

	go func() { done <- w.RunInProcess(ctx, job, onWait) }()

	return done
}

// waitResult waits for the error of a RunInProcess started by runInProcessInBackground.
func waitResult(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(waitFor):
		t.Fatal("RunInProcess did not return")

		return nil
	}
}

// receive waits for a value on ch.
func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(waitFor):
		t.Fatal(what)

		var zero T

		return zero
	}
}

// Given a free serial key, then RunInProcess creates and claims the job atomically, runs its
// handler, renews the lease and writes the final status.
func TestRunInProcess_FreeSerialKey_RunsTheJobUnderItsLease(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()
	started := make(chan Job[testPayload], 1)
	release := make(chan struct{})
	kind := Register(reg, "test.kind", KindOptions{}, func(_ context.Context, job Job[testPayload]) error {
		started <- job
		<-release

		return nil
	})

	var waits atomic.Int64

	w, logs := newTestWorker(s, reg, clk, Options{Lease: lease, HeartbeatInterval: shortInterval})
	done := runInProcessInBackground(ctx, w, newKeyedJob(t, kind, "first"), func() { waits.Add(1) })

	handled := receive(t, started, "the handler did not start")
	require.Equal(t, testPayload{Name: "first"}, handled.Payload)
	require.Equal(t, int64(7), handled.UserID)

	// Claimed once, when it was created: no other claim could take it before.
	running := getJob(ctx, t, s, handled.ID)
	require.Equal(t, store.JobRunning, running.Status)
	require.Equal(t, int64(1), running.Claims)
	require.Equal(t, testSerialKey, running.SerialKey)

	clk.Advance(20 * time.Second)

	want := clk.Now().Add(lease)

	require.Eventually(t, func() bool {
		job, err := s.Jobs().Get(ctx, handled.ID)

		return err == nil && job.LockedUntil.Equal(want)
	}, waitFor, time.Millisecond, "a heartbeat must renew the lease from now")

	close(release)
	require.NoError(t, waitResult(t, done))

	job := getJob(ctx, t, s, handled.ID)
	require.Equal(t, store.JobCompleted, job.Status)
	require.Equal(t, clk.Now(), job.FinishedAt)
	require.Zero(t, waits.Load(), "a free serial key is no wait")

	lines := messageLines(t, logs, "job finished")
	require.Len(t, lines, 1)
	require.Equal(t, "completed", lines[0]["status"])
}

// Given a held serial key, then RunInProcess waits, calls its wait callback once, and runs when the
// key is free.
func TestRunInProcess_HeldSerialKey_WaitsAndRunsOnceTheKeyIsFree(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	ctx := t.Context()
	reg := NewRegistry()

	var ran atomic.Bool

	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		ran.Store(true)

		return nil
	})
	holder := holdSerialKey(ctx, t, s, kind)

	var waits atomic.Int64

	w, _ := newTestWorker(s, reg, clk, Options{WaitInterval: shortInterval})
	done := runInProcessInBackground(ctx, w, newKeyedJob(t, kind, "first"), func() { waits.Add(1) })

	require.Eventually(t, func() bool { return waits.Load() == 1 }, waitFor, time.Millisecond)
	require.Never(t, func() bool { return ran.Load() || waits.Load() > 1 }, 50*time.Millisecond, time.Millisecond,
		"the job must wait for the key, and the callback must run once over several polls")

	require.NoError(t, s.Jobs().Complete(ctx, holder.ID, holder.Claims))
	require.NoError(t, waitResult(t, done))

	require.True(t, ran.Load())
	require.Equal(t, int64(1), waits.Load())
}

// Given a cancelled context while waiting, then RunInProcess returns the context error and leaves
// no job behind.
func TestRunInProcess_ContextCancelledWhileWaiting_ReturnsTheContextErrorAndLeavesNoJob(t *testing.T) {
	t.Parallel()

	s, clk := newTestStore(t)
	reg := NewRegistry()

	var ran atomic.Bool

	kind := Register(reg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		ran.Store(true)

		return nil
	})
	holder := holdSerialKey(t.Context(), t, s, kind)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Only the context can end the wait.
	w, _ := newTestWorker(s, reg, clk, Options{WaitInterval: noPoll})

	err := w.RunInProcess(ctx, newKeyedJob(t, kind, "first"), cancel)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, ran.Load())

	require.NoError(t, s.Jobs().Complete(t.Context(), holder.ID, holder.Claims))
	requireNoTick(t.Context(), t, w)
}

// Given an in-process job with a valid lease, then the worker of the server claims neither the job
// nor a queued job of its serial key.
func TestRunInProcess_ValidLease_KeepsTheJobAndItsKeyFromTheServer(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

	path := filepath.Join(t.TempDir(), "verve.db")
	cli, cliClk := newTestStoreAt(t, path)
	server, serverClk := newTestStoreAt(t, path)
	ctx := t.Context()

	started := make(chan struct{})
	release := make(chan struct{})
	cliReg := NewRegistry()
	kind := Register(cliReg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		close(started)
		<-release

		return nil
	})

	serverReg := NewRegistry()
	Register(serverReg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return nil
	})

	w, _ := newTestWorker(cli, cliReg, cliClk, Options{Lease: lease, HeartbeatInterval: noPoll})
	done := runInProcessInBackground(ctx, w, newKeyedJob(t, kind, "first"), func() {})

	waitClosed(t, started, "the handler did not start")

	queued, err := server.Jobs().Enqueue(ctx, newKeyedJob(t, kind, "second"))
	require.NoError(t, err)

	serverWorker, _ := newTestWorker(server, serverReg, serverClk, Options{})

	serverClk.Advance(lease - time.Millisecond)
	requireNoTick(ctx, t, serverWorker)

	close(release)
	require.NoError(t, waitResult(t, done))

	requireTick(ctx, t, serverWorker)
	require.Equal(t, store.JobCompleted, getJob(ctx, t, server, queued.ID).Status)
}

// Given an in-process job whose process died, then the lease expires and the worker of the server
// finishes the job. The process that died can no longer write its result.
func TestRunInProcess_ProcessDied_TheServerFinishesTheJobOnceTheLeaseExpires(t *testing.T) {
	t.Parallel()

	const lease = 60 * time.Second

	s, clk := newTestStore(t)
	ctx := t.Context()

	started := make(chan int64, 1)
	release := make(chan struct{})
	cliReg := NewRegistry()
	kind := Register(cliReg, "test.kind", KindOptions{}, func(_ context.Context, job Job[testPayload]) error {
		started <- job.ID
		<-release

		return nil
	})

	serverReg := NewRegistry()
	Register(serverReg, "test.kind", KindOptions{}, func(context.Context, Job[testPayload]) error {
		return nil
	})

	// A process that died renews nothing: its heartbeat never beats, and its handler is stuck.
	w, _ := newTestWorker(s, cliReg, clk, Options{Lease: lease, HeartbeatInterval: noPoll})
	done := runInProcessInBackground(ctx, w, newKeyedJob(t, kind, "first"), func() {})

	id := receive(t, started, "the handler did not start")
	server, _ := newTestWorker(s, serverReg, clk, Options{})

	clk.Advance(lease - time.Millisecond)
	requireNoTick(ctx, t, server)

	clk.Advance(time.Millisecond)
	requireTick(ctx, t, server)

	job := getJob(ctx, t, s, id)
	require.Equal(t, store.JobCompleted, job.Status)
	require.Equal(t, int64(2), job.Claims)
	require.Equal(t, int64(1), job.LostLeases)

	close(release)
	require.ErrorIs(t, waitResult(t, done), store.ErrConflict)
	require.Equal(t, job, getJob(ctx, t, s, id))
}

// Given a worker on one store and RunInProcess on another store of the same file, then two jobs of
// one serial key never overlap, and all of them end.
func TestRunInProcess_WorkerOnAnotherStoreOfTheSameFile_JobsOfOneKeyNeverOverlap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "verve.db")
	server, serverClk := newTestStoreAt(t, path)
	cli, cliClk := newTestStoreAt(t, path)
	ctx := t.Context()

	var running, overlaps, ran atomic.Int64

	handler := func(context.Context, Job[testPayload]) error {
		if running.Add(1) > 1 {
			overlaps.Add(1)
		}

		time.Sleep(10 * time.Millisecond)
		running.Add(-1)
		ran.Add(1)

		return nil
	}

	serverReg := NewRegistry()
	kind := Register(serverReg, "test.kind", KindOptions{}, handler)
	cliReg := NewRegistry()
	Register(cliReg, "test.kind", KindOptions{}, handler)

	for _, name := range []string{"first", "second", "third", "fourth"} {
		_, err := server.Jobs().Enqueue(ctx, newKeyedJob(t, kind, name))
		require.NoError(t, err)
	}

	// Two workers, which would run two jobs of the server at once without the key.
	serverWorker, _ := newTestWorker(server, serverReg, serverClk, Options{Workers: 2, PollInterval: shortInterval})
	startWorker(t, serverWorker)

	w, _ := newTestWorker(cli, cliReg, cliClk, Options{WaitInterval: shortInterval})

	var wg sync.WaitGroup

	errs := make(chan error, 3)
	job := newKeyedJob(t, kind, "in-process")

	for range 3 {
		wg.Go(func() { errs <- w.RunInProcess(ctx, job, func() {}) })
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool { return ran.Load() == 7 }, waitFor, time.Millisecond)
	require.Zero(t, overlaps.Load(), "two jobs of one serial key ran at the same time")
}

// Given an in-process run that does not complete its job, then RunInProcess returns why, and the
// job ends as on a worker: a server worker retries a failed attempt, and resumes a job whose
// process stopped.
func TestRunInProcess_RunThatDoesNotComplete_ReturnsWhyAndEndsTheJobLikeAWorker(t *testing.T) {
	t.Parallel()

	errFailed := errors.New("handler failed")

	tests := []struct {
		name string
		// handler runs the job. stop cancels the context of RunInProcess, as a stopped process does.
		handler      func(ctx context.Context, stop context.CancelFunc, s store.Store, job Job[testPayload]) error
		wantErr      error
		wantStatus   store.JobStatus
		wantAttempts int64
	}{
		{
			name: "handler fails",
			handler: func(context.Context, context.CancelFunc, store.Store, Job[testPayload]) error {
				return errFailed
			},
			wantErr:      errFailed,
			wantStatus:   store.JobQueued,
			wantAttempts: 1,
		},
		{
			name: "handler fails for good",
			handler: func(context.Context, context.CancelFunc, store.Store, Job[testPayload]) error {
				return Permanent(errFailed)
			},
			wantErr:      errFailed,
			wantStatus:   store.JobFailed,
			wantAttempts: 1,
		},
		{
			name: "job cancelled",
			handler: func(ctx context.Context, _ context.CancelFunc, s store.Store, job Job[testPayload]) error {
				if err := s.Jobs().Cancel(ctx, job.ID); err != nil {
					return err
				}

				<-ctx.Done()

				return ctx.Err()
			},
			wantErr:    errCancelled,
			wantStatus: store.JobCancelled,
		},
		{
			name: "process stopped",
			handler: func(ctx context.Context, stop context.CancelFunc, _ store.Store, _ Job[testPayload]) error {
				stop()
				<-ctx.Done()

				return ctx.Err()
			},
			wantErr:    context.Canceled,
			wantStatus: store.JobQueued,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, clk := newTestStore(t)
			ctx, stop := context.WithCancel(t.Context())
			defer stop()

			reg := NewRegistry()
			ids := make(chan int64, 1)
			kind := Register(reg, "test.kind", KindOptions{}, func(ctx context.Context, job Job[testPayload]) error {
				ids <- job.ID

				return tt.handler(ctx, stop, s, job)
			})

			w, _ := newTestWorker(s, reg, clk, Options{HeartbeatInterval: shortInterval})

			err := w.RunInProcess(ctx, newKeyedJob(t, kind, "first"), func() {})
			require.ErrorIs(t, err, tt.wantErr)

			job := getJob(t.Context(), t, s, receive(t, ids, "the handler did not start"))
			require.Equal(t, tt.wantStatus, job.Status)
			require.Equal(t, tt.wantAttempts, job.Attempts)
		})
	}
}
