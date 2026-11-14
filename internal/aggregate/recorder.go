package aggregate

import (
	"context"
	"fmt"

	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// JobQueue enqueues a job through repos, the store or the repositories of a WithTx, so that the job
// commits with the ranges that queue it.
type JobQueue interface {
	Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error)
}

// storeQueue writes jobs to the store without waking a worker, which finds them at its next poll.
type storeQueue struct{}

func (storeQueue) Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error) {
	return repos.Jobs().Enqueue(ctx, job)
}

// Recorder records the dirty ranges of the samples that change, and queues their recomputation
// (FR-9 and FR-10 of Storage and aggregates). It is the dirty range recorder of the pipeline.
type Recorder struct {
	queue JobQueue
}

var _ ingest.DirtyRanges = Recorder{}

// NewRecorder returns a recorder that queues the recompute jobs on queue. A nil queue writes them to
// the store without waking a worker.
func NewRecorder(queue JobQueue) Recorder {
	if queue == nil {
		queue = storeQueue{}
	}

	return Recorder{queue: queue}
}

// Record records ranges as dirty ranges of userID through tx, the transaction of their batch, and
// queues the recompute job of the user unless one is queued and not running yet: that job reads
// the ranges when it starts, while a running one may have read them already.
func (r Recorder) Record(ctx context.Context, tx store.Repositories, userID int64, ranges []ingest.DirtyRange) error {
	if len(ranges) == 0 {
		return nil
	}

	dirty := make([]store.DirtyRange, 0, len(ranges))
	for _, rg := range ranges {
		dirty = append(dirty, store.DirtyRange{TypeID: rg.TypeID, From: rg.From, To: rg.To})
	}

	if err := tx.Aggregates().RecordDirty(ctx, userID, dirty); err != nil {
		return err
	}

	return r.queueRecompute(ctx, tx, userID)
}

// RecomputeAll deletes the aggregates of userID and records a range of each of their types through
// tx, the transaction that stores their new timezone, so that no row of the old timezone is read as
// one of the new one, and queues the recompute job, which computes them in the new timezone
// (FR-10 of Storage and aggregates).
func (r Recorder) RecomputeAll(ctx context.Context, tx store.Repositories, userID int64) error {
	if err := tx.Aggregates().DeleteAll(ctx, userID); err != nil {
		return err
	}

	if err := tx.Aggregates().RecordAllDirty(ctx, userID); err != nil {
		return err
	}

	return r.queueRecompute(ctx, tx, userID)
}

// queueRecompute queues the recompute job of userID through tx, unless one is queued.
func (r Recorder) queueRecompute(ctx context.Context, tx store.Repositories, userID int64) error {
	active, err := tx.Jobs().ActiveJobs(ctx, SerialKey(userID))
	if err != nil {
		return fmt.Errorf("find recompute job of user %d: %w", userID, err)
	}

	for _, job := range active {
		if job.Status == store.JobQueued {
			return nil
		}
	}

	if _, err := r.queue.Enqueue(ctx, tx, recomputeJob(userID)); err != nil {
		return fmt.Errorf("queue recompute job of user %d: %w", userID, err)
	}

	return nil
}
