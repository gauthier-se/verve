package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// RunInProcess runs job in this process, for a command such as verve import xml that runs its job
// itself instead of leaving it to the server. While the serial key of job is held, it waits and
// tries again at every wait interval, and calls onWait once, when the wait starts. It then creates
// and claims the job in one write, and runs it under a lease that the heartbeat renews, as a worker
// does. If the process dies, the lease expires and the server resumes the job.
//
// The run is final, because the command reports its end: a failed attempt fails the job without a
// retry, and a cancelled ctx cancels the job. RunInProcess returns nil once the job completed, and
// otherwise why it did not: the error of the handler, the context error, errCancelled when the
// cancellation of the job was requested, or the error of a lost lease or of a write that failed.
func (w *Worker) RunInProcess(ctx context.Context, job store.NewJob, onWait func()) error {
	claimed, err := w.claimInProcess(ctx, job, onWait)
	if err != nil {
		return err
	}

	end := w.run(ctx, claimed, true)

	err = end.record(ctx)
	if err != nil && ctx.Err() != nil {
		// No Run writes the end again after a shutdown: it is written here, under the same deadline.
		finishCtx, cancel := w.finishContext(ctx)
		defer cancel()

		err = end.record(finishCtx)
	}

	w.logEnd(ctx, end, err)

	switch {
	case err != nil:
		return err
	case end.status == string(store.JobCancelled) && ctx.Err() != nil:
		return ctx.Err()
	case end.status == string(store.JobCancelled):
		return errCancelled
	}

	// Nil once completed, and the error of the handler or of the lost lease otherwise.
	return end.err
}

// claimInProcess creates and claims job, waiting while its serial key is held. It calls onWait
// once, when the wait starts.
func (w *Worker) claimInProcess(ctx context.Context, job store.NewJob, onWait func()) (store.Job, error) {
	waiting := false

	for {
		claimed, err := w.store.Jobs().EnqueueClaimed(ctx, job, w.opts.Lease)
		if !errors.Is(err, store.ErrConflict) {
			return claimed, err
		}

		if !waiting {
			waiting = true

			onWait()
		}

		select {
		case <-time.After(w.opts.WaitInterval):
		case <-ctx.Done():
			return store.Job{}, ctx.Err()
		}
	}
}
