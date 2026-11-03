package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// RunInProcess runs job in this process, for a command such as verve import xml that runs its job
// itself instead of leaving it to the server. While another job holds the serial key of job, it
// waits and tries again at every wait interval, and calls onWait once, when the wait starts. It then
// creates and claims the job in one write, and runs it as a worker does: under a lease that the
// heartbeat renews, ending and logging the run the same way. If the process dies, the lease
// expires and the server resumes the job.
//
// It returns nil once the job completed. Otherwise the job ended as on a worker, and RunInProcess
// returns why: the error of the handler, whether the job failed or waits for a retry that the
// server runs, the context error when ctx was cancelled, after which the job is queued again for
// the server, or the error of a lost lease or of a write that failed.
func (w *Worker) RunInProcess(ctx context.Context, job store.NewJob, onWait func()) error {
	claimed, err := w.claimInProcess(ctx, job, onWait)
	if err != nil {
		return err
	}

	end, err := w.run(ctx, claimed)
	if err != nil && ctx.Err() != nil {
		// No Run writes the end again after a shutdown: it is written here, under the same deadline.
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.opts.FinishTimeout)
		defer cancel()

		err = end.record(finishCtx)
	}

	w.logEnd(ctx, end, err)

	switch {
	case err != nil:
		return err
	case end.status == string(store.JobCompleted):
		return nil
	case end.status == string(store.JobCancelled):
		return errCancelled
	case end.status == statusInterrupted:
		return ctx.Err()
	}

	return end.err
}

// claimInProcess creates and claims job, waiting while another job holds its serial key. It calls
// onWait once, when the wait starts.
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
