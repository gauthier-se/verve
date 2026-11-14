package aggregate

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

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

// mergeGap is the largest gap between two ranges of a type of a batch that Record merges: ranges
// less than an hour apart recompute the same local hours or the next ones, so that a batch of
// frequent samples, such as heart rates, records a few ranges instead of one per sample.
const mergeGap = time.Hour

// Record records ranges as dirty ranges of userID through tx, the transaction of their batch, those
// of a type less than an hour apart merged, and queues the recompute job of the user unless one is
// queued and not running yet: that job reads the ranges when it starts, while a running one may
// have read them already.
func (r Recorder) Record(ctx context.Context, tx store.Repositories, userID int64, ranges []ingest.DirtyRange) error {
	if len(ranges) == 0 {
		return nil
	}

	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(a, b ingest.DirtyRange) int {
		return cmp.Or(cmp.Compare(a.TypeID, b.TypeID), a.From.Compare(b.From))
	})

	var dirty []store.DirtyRange

	for _, rg := range sorted {
		if n := len(dirty); n > 0 && dirty[n-1].TypeID == rg.TypeID && rg.From.Sub(dirty[n-1].To) < mergeGap {
			if rg.To.After(dirty[n-1].To) {
				dirty[n-1].To = rg.To
			}

			continue
		}

		dirty = append(dirty, store.DirtyRange{TypeID: rg.TypeID, From: rg.From, To: rg.To})
	}

	if err := tx.Aggregates().RecordDirty(ctx, userID, dirty); err != nil {
		return fmt.Errorf("record dirty ranges of user %d: %w", userID, err)
	}

	return r.queueRecompute(ctx, tx, userID)
}

// RecomputeAll deletes the aggregates of userID and records a range of each of their types through
// tx, the transaction that stores their new timezone, so that no row of the old timezone is read as
// one of the new one, and queues the recompute job, which computes them in the new timezone
// (FR-10 of Storage and aggregates).
func (r Recorder) RecomputeAll(ctx context.Context, tx store.Repositories, userID int64) error {
	if err := tx.Aggregates().DeleteAll(ctx, userID); err != nil {
		return fmt.Errorf("recompute every aggregate of user %d: %w", userID, err)
	}

	if err := tx.Aggregates().RecordAllDirty(ctx, userID); err != nil {
		return fmt.Errorf("recompute every aggregate of user %d: %w", userID, err)
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
