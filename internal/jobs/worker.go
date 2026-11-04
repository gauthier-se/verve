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
	defaultWaitInterval      = time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultFinishTimeout     = time.Second
)

// Log statuses of a run that does not end its job.
const (
	statusRetrying    = "retrying"
	statusInterrupted = "interrupted"
	statusLeaseLost   = "lease_lost"
	statusResultLost  = "result_lost"
)

var (
	// errLeaseLost is the cause of the cancellation of a handler that lost its lease.
	errLeaseLost = errors.New("job lease lost")
	// errCancelled is the cause of the cancellation of a handler whose job was cancelled.
	errCancelled = errors.New("job cancelled")
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
	// WaitInterval is how often RunInProcess tries again to claim a job whose serial key is held.
	WaitInterval time.Duration
	// ShutdownTimeout bounds the wait for the handlers during a shutdown. The lease recovers the job
	// of a handler that ignores its context for longer.
	ShutdownTimeout time.Duration
	// FinishTimeout bounds, in total, the writes that end the jobs stopped by a shutdown.
	FinishTimeout time.Duration
	// Schedules declares the recurring jobs that Run enqueues.
	Schedules []Schedule
	// Location is the time zone of the cron expressions of Schedules. It defaults to the local time
	// of the process.
	Location *time.Location
}

// Worker is a pool that runs the jobs of a store with the handlers of a registry. A command that
// runs its own job calls RunInProcess, and never Run.
type Worker struct {
	store     store.Store
	registry  *Registry
	clk       clock.Clock
	logger    *slog.Logger
	opts      Options
	scheduler *Scheduler
	// wake holds one signal per worker, so that jobs enqueued together wake up the whole pool.
	wake chan struct{}

	mu sync.Mutex
	// stopped holds the ends of the jobs stopped by a shutdown, until Run writes them.
	stopped []jobEnd
}

// jobEnd is how a run of a job ended.
type jobEnd struct {
	job      store.Job
	status   string
	duration time.Duration
	// err is the error to log, if any.
	err error
	// write records the end, or is nil when there is nothing to record.
	write func(ctx context.Context) error
}

// NewWorker returns a worker. Run starts it.
func NewWorker(s store.Store, registry *Registry, clk clock.Clock, logger *slog.Logger, opts Options) *Worker {
	opts.Workers = cmp.Or(opts.Workers, defaultWorkers)
	opts.Lease = cmp.Or(opts.Lease, defaultLease)
	opts.HeartbeatInterval = cmp.Or(opts.HeartbeatInterval, defaultHeartbeatInterval)
	opts.PollInterval = cmp.Or(opts.PollInterval, defaultPollInterval)
	opts.WaitInterval = cmp.Or(opts.WaitInterval, defaultWaitInterval)
	opts.ShutdownTimeout = cmp.Or(opts.ShutdownTimeout, defaultShutdownTimeout)
	opts.FinishTimeout = cmp.Or(opts.FinishTimeout, defaultFinishTimeout)

	w := &Worker{
		store:    s,
		registry: registry,
		clk:      clk,
		logger:   logger,
		opts:     opts,
		wake:     make(chan struct{}, opts.Workers),
	}
	w.scheduler = &Scheduler{worker: w, schedules: opts.Schedules, location: cmp.Or(opts.Location, time.Local)}

	return w
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

// Run runs jobs and enqueues the recurring ones until ctx is cancelled. It then stops claiming,
// cancels the running handlers, waits for them up to the shutdown timeout, writes how the stopped
// jobs ended and returns.
func (w *Worker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "worker started", slog.Int("workers", w.opts.Workers))

	var wg sync.WaitGroup

	wg.Go(func() { w.scheduler.loop(ctx) })

	for range w.opts.Workers {
		wg.Go(func() { w.loop(ctx) })
	}

	handlersStopped := make(chan struct{})

	go func() {
		wg.Wait()
		close(handlersStopped)
	}()

	<-ctx.Done()

	select {
	case <-handlersStopped:
	case <-time.After(w.opts.ShutdownTimeout):
		w.logger.WarnContext(ctx, "job handlers did not stop", slog.Duration("timeout", w.opts.ShutdownTimeout))
	}

	w.finishStopped(ctx)

	w.logger.InfoContext(ctx, "worker stopped")
}

// finishStopped writes the ends of the jobs stopped by the shutdown, under one deadline that starts
// once the handlers have stopped. The write lock of another process cannot stall the shutdown: a
// job that misses the deadline stays running, and its lease recovers it.
func (w *Worker) finishStopped(ctx context.Context) {
	ctx, cancel := w.finishContext(ctx)
	defer cancel()

	w.mu.Lock()
	ends := w.stopped
	w.stopped = nil
	w.mu.Unlock()

	for _, end := range ends {
		w.logEnd(ctx, end, end.record(ctx))
	}
}

// finishContext returns the context of the writes that end the jobs stopped by a shutdown: it
// outlives ctx, up to the finish timeout.
func (w *Worker) finishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), w.opts.FinishTimeout)
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
	job, abandoned, err := w.store.Jobs().Claim(ctx, w.opts.Lease)
	for _, a := range abandoned {
		w.logAbandoned(ctx, a)
	}

	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	end := w.run(ctx, job, false)

	// A shutdown cuts the write short, and Run writes the end again under its deadline.
	err = end.record(ctx)
	if err != nil && ctx.Err() != nil {
		w.mu.Lock()
		w.stopped = append(w.stopped, end)
		w.mu.Unlock()

		return true, nil
	}

	w.logEnd(ctx, end, err)

	return true, nil
}

// run runs the handler of a claimed job under its heartbeat, and returns how the run ended, for the
// caller to record. A final run ends its job for good, see endOf.
func (w *Worker) run(ctx context.Context, job store.Job, final bool) jobEnd {
	start := w.clk.Now()

	handlerCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	stopHeartbeat := w.startHeartbeat(ctx, job, cancel)
	handlerErr := w.invoke(handlerCtx, job)

	stopHeartbeat()

	end := w.endOf(ctx, job, handlerErr, context.Cause(handlerCtx), final)
	end.duration = w.clk.Now().Sub(start)

	return end
}

// startHeartbeat renews the lease of job until the returned function is called, which waits for it
// to stop. When the job is no longer held, it calls cancel with errLeaseLost, so that the job does
// not run twice, and when its cancellation was requested, it calls cancel with errCancelled. A
// heartbeat that fails for another reason, for example behind the write lock of another process,
// is tried again at the next beat, until the lease runs out.
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
			cancelRequested, err := w.store.Jobs().Heartbeat(ctx, job.ID, job.Claims, w.opts.Lease)

			switch {
			case err == nil:
				leaseEnd = now.Add(w.opts.Lease)

				if cancelRequested {
					cancel(errCancelled)
				}
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

// endOf decides how a run of job ended. cause is the cancellation cause of its handler. A final run
// is one whose command reports its end, such as a run in process: instead of leaving its job to a
// worker, it fails the job on a failed attempt, and cancels it when the process stops.
func (w *Worker) endOf(ctx context.Context, job store.Job, handlerErr, cause error, final bool) jobEnd {
	jobs := w.store.Jobs()
	cancelled := jobEnd{job: job, status: string(store.JobCancelled), write: func(ctx context.Context) error {
		return jobs.ConfirmCancel(ctx, job.ID, job.Claims)
	}}

	switch {
	case errors.Is(cause, errLeaseLost):
		return jobEnd{job: job, status: statusLeaseLost, err: cause}
	case errors.Is(cause, errCancelled):
		return cancelled
	case handlerErr == nil:
		return jobEnd{job: job, status: string(store.JobCompleted), write: func(ctx context.Context) error {
			return jobs.Complete(ctx, job.ID, job.Claims)
		}}
	case !isPermanent(handlerErr) && ctx.Err() != nil && final:
		return cancelled
	case !isPermanent(handlerErr) && ctx.Err() != nil:
		return jobEnd{job: job, status: statusInterrupted, write: func(ctx context.Context) error {
			return jobs.Release(ctx, job.ID, job.Claims)
		}}
	}

	attempts := job.Attempts + 1
	message := lastError(handlerErr)

	if final || isPermanent(handlerErr) || attempts >= w.registry.maxAttempts(job.Kind) {
		return jobEnd{job: job, status: string(store.JobFailed), err: handlerErr, write: func(ctx context.Context) error {
			return jobs.Fail(ctx, job.ID, job.Claims, message)
		}}
	}

	runAt := w.clk.Now().Add(backoff(attempts))

	return jobEnd{job: job, status: statusRetrying, err: handlerErr, write: func(ctx context.Context) error {
		return jobs.Retry(ctx, job.ID, job.Claims, message, runAt)
	}}
}

// record writes the end of the run. It does nothing when there is nothing to write.
func (e jobEnd) record(ctx context.Context) error {
	if e.write == nil {
		return nil
	}

	return e.write(ctx)
}

// logEnd writes the one log line of a run, whose end was recorded with the result writeErr. It
// never carries the payload. When the write failed, the job stays as it is, and the lease recovers
// a job still running.
func (w *Worker) logEnd(ctx context.Context, end jobEnd, writeErr error) {
	switch {
	case isLost(writeErr):
		end.status, end.err = statusLeaseLost, writeErr
	case writeErr != nil:
		end.status, end.err = statusResultLost, writeErr
	}

	level := slog.LevelInfo
	attrs := []slog.Attr{
		slog.String("kind", end.job.Kind),
		slog.Int64("id", end.job.ID),
		slog.String("status", end.status),
		slog.Duration("duration", end.duration),
	}

	if end.err != nil {
		level = slog.LevelWarn
		attrs = append(attrs, slog.Any("error", end.err))
	}

	w.logger.LogAttrs(ctx, level, "job finished", attrs...)
}

// logAbandoned writes the one log line of a job that a claim abandoned after its lost leases, as
// failed. It has no duration: none of its runs ended in a process that could time it. It never
// carries the payload.
func (w *Worker) logAbandoned(ctx context.Context, job store.Job) {
	w.logger.LogAttrs(ctx, slog.LevelWarn, "job finished",
		slog.String("kind", job.Kind),
		slog.Int64("id", job.ID),
		slog.String("status", string(job.Status)),
		slog.String("error", job.LastError),
	)
}
