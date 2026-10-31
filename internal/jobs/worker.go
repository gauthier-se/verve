package jobs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// Default options of a Worker.
const (
	defaultWorkers           = 2
	defaultLease             = 60 * time.Second
	defaultHeartbeatInterval = 20 * time.Second
	defaultPollInterval      = 5 * time.Second
)

// Log statuses of a job whose end is not recorded as a job status. The job stays running and its
// lease recovers it.
const (
	statusLeaseLost   = "lease_lost"
	statusInterrupted = "interrupted"
	statusResultLost  = "result_lost"
)

var (
	// errLeaseLost is the cause of the cancellation of a handler that lost its lease.
	errLeaseLost = errors.New("job lease lost")
	// errPanicked is the last error of a job whose handler panicked. It is a fixed text, because a
	// job error is shown to admins and a panic value can hold anything.
	errPanicked = errors.New("handler panicked")
)

// Options configures a Worker. Zero values take their defaults.
type Options struct {
	Workers           int
	Lease             time.Duration
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
}

// Worker is a pool that runs the jobs of a store with the handlers of a registry.
type Worker struct {
	store    store.Store
	registry *Registry
	clk      clock.Clock
	logger   *slog.Logger
	opts     Options
	// wake holds one signal per worker, so that jobs enqueued together wake up the whole pool.
	wake chan struct{}
}

// NewWorker returns a worker. Run starts it.
func NewWorker(s store.Store, registry *Registry, clk clock.Clock, logger *slog.Logger, opts Options) *Worker {
	opts.Workers = cmp.Or(opts.Workers, defaultWorkers)
	opts.Lease = cmp.Or(opts.Lease, defaultLease)
	opts.HeartbeatInterval = cmp.Or(opts.HeartbeatInterval, defaultHeartbeatInterval)
	opts.PollInterval = cmp.Or(opts.PollInterval, defaultPollInterval)

	return &Worker{
		store:    s,
		registry: registry,
		clk:      clk,
		logger:   logger,
		opts:     opts,
		wake:     make(chan struct{}, opts.Workers),
	}
}

// Enqueue writes job through repos and wakes up a worker once it is committed, so repos is either
// the store or the repositories of a WithTx.
func (w *Worker) Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error) {
	queued, err := repos.Jobs().Enqueue(ctx, job)
	if err != nil {
		return store.Job{}, err
	}

	repos.AfterCommit(func() {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	})

	return queued, nil
}

// Run runs jobs until ctx is cancelled. It then stops claiming, cancels the running handlers and
// returns once they have returned.
func (w *Worker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "worker started", slog.Int("workers", w.opts.Workers))

	var wg sync.WaitGroup

	for range w.opts.Workers {
		wg.Go(func() { w.loop(ctx) })
	}

	wg.Wait()

	w.logger.InfoContext(ctx, "worker stopped")
}

// loop claims jobs until ctx is cancelled. After a job it asks for the next one at once, otherwise
// it waits for a job enqueued by this process or for the next poll, which finds the others.
func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		claimed, err := w.Tick(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.WarnContext(ctx, "claim job", slog.Any("error", err))
		}

		if claimed {
			continue
		}

		select {
		case <-w.wake:
		case <-time.After(w.opts.PollInterval):
		case <-ctx.Done():
		}
	}
}

// Tick claims the oldest due job and runs it until it ends. It reports whether it claimed a job,
// and returns an error when the claim fails.
func (w *Worker) Tick(ctx context.Context) (claimed bool, err error) {
	job, err := w.store.Jobs().Claim(ctx, w.opts.Lease)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	start := w.clk.Now()

	handlerCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	stopHeartbeat := w.startHeartbeat(ctx, job, cancel)
	handlerErr := w.invoke(handlerCtx, job)

	stopHeartbeat()

	status, endErr := w.record(ctx, job, handlerErr, context.Cause(handlerCtx))
	w.logEnd(ctx, job, status, endErr, start)

	return true, nil
}

// startHeartbeat renews the lease of job until the returned function is called, which waits for it
// to stop. When the job is no longer held, it calls cancel with errLeaseLost, so that the job does
// not run twice. A heartbeat that fails for another reason, for example behind the write lock of
// another process, is tried again at the next beat, until the lease runs out.
func (w *Worker) startHeartbeat(ctx context.Context, job store.Job, cancel context.CancelCauseFunc) func() {
	// A shutdown does not stop it: the lease must hold until the handler has returned.
	ctx, stop := context.WithCancel(context.WithoutCancel(ctx))

	var wg sync.WaitGroup

	wg.Go(func() {
		ticker := time.NewTicker(w.opts.HeartbeatInterval)
		defer ticker.Stop()

		leaseEnd := job.LockedUntil

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			now := w.clk.Now()
			err := w.store.Jobs().Heartbeat(ctx, job.ID, job.Claims, w.opts.Lease)

			switch {
			case err == nil:
				leaseEnd = now.Add(w.opts.Lease)
			case ctx.Err() != nil:
				return
			case isLost(err), !now.Before(leaseEnd):
				cancel(fmt.Errorf("%w: %w", errLeaseLost, err))

				return
			default:
				w.logger.WarnContext(ctx, "renew job lease",
					slog.String("kind", job.Kind),
					slog.Int64("id", job.ID),
					slog.Any("error", err))
			}
		}
	})

	return func() {
		stop()
		wg.Wait()
	}
}

// isLost reports whether err means that the job is no longer held under its claim.
func isLost(err error) bool {
	return errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound)
}

// invoke runs the handler of job. A panic is logged and returned as errPanicked. The log carries
// the type of the panic value, never the value, which can hold anything.
func (w *Worker) invoke(ctx context.Context, job store.Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.ErrorContext(ctx, "job handler panicked",
				slog.String("kind", job.Kind),
				slog.Int64("id", job.ID),
				slog.String("panic_type", fmt.Sprintf("%T", r)),
				slog.String("stack", string(debug.Stack())))

			err = errPanicked
		}
	}()

	return w.registry.run(ctx, job)
}

// record writes how the handler of job ended. It returns the status to log, with the error to log
// if any. cause is the cancellation cause of the handler.
func (w *Worker) record(ctx context.Context, job store.Job, handlerErr, cause error) (string, error) {
	switch {
	case errors.Is(cause, errLeaseLost):
		return statusLeaseLost, cause
	case handlerErr != nil && ctx.Err() != nil:
		return statusInterrupted, nil
	}

	// A job that ends during a shutdown is recorded all the same.
	ctx = context.WithoutCancel(ctx)

	var (
		status = store.JobCompleted
		err    error
	)

	if handlerErr == nil {
		err = w.store.Jobs().Complete(ctx, job.ID, job.Claims)
	} else {
		status = store.JobFailed
		err = w.store.Jobs().Fail(ctx, job.ID, job.Claims, handlerErr.Error())
	}

	switch {
	case isLost(err):
		return statusLeaseLost, err
	case err != nil:
		return statusResultLost, err
	}

	return string(status), handlerErr
}

// logEnd writes the one log line of the end of job. It never carries the payload.
func (w *Worker) logEnd(ctx context.Context, job store.Job, status string, err error, start time.Time) {
	level := slog.LevelInfo
	attrs := []slog.Attr{
		slog.String("kind", job.Kind),
		slog.Int64("id", job.ID),
		slog.String("status", status),
		slog.Duration("duration", w.clk.Now().Sub(start)),
	}

	if err != nil {
		level = slog.LevelWarn
		attrs = append(attrs, slog.Any("error", err))
	}

	w.logger.LogAttrs(ctx, level, "job finished", attrs...)
}
