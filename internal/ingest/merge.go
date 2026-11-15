package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/gauthier-se/verve/internal/jobs"
	"github.com/gauthier-se/verve/internal/store"
)

// KindMergeSources is the kind of the job that moves the records of merged sources to their target
// (FR-3 of Deduplication and source priority).
const KindMergeSources = "sources.merge"

// The errors of StartMerge for merges it refuses. Their messages are the stable codes that the API
// answers with.
var (
	// ErrTargetAmongMerged is returned for a merge whose target is among the sources merged into it.
	ErrTargetAmongMerged = errors.New("target_among_merged")
	// ErrMergeInProgress is returned for a merge of a source that a merge in progress holds, as
	// target or merged.
	ErrMergeInProgress = errors.New("merge_in_progress")
)

// MergePayload is the payload of a merge job: the target and the sources merged into it.
type MergePayload struct {
	TargetID  int64   `json:"targetId"`
	SourceIDs []int64 `json:"sourceIds"`
}

// JobQueue enqueues a job through repos, the repositories of a WithTx, so that the job commits with
// the writes that queue it.
type JobQueue interface {
	Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error)
}

// storeQueue writes jobs to the store without waking a worker, which finds them at its next poll.
type storeQueue struct{}

func (storeQueue) Enqueue(ctx context.Context, repos store.Repositories, job store.NewJob) (store.Job, error) {
	return repos.Jobs().Enqueue(ctx, job)
}

// StartMerge sets the sources of sourceIDs of userID as merged into the source of targetID, and
// queues the merge job through queue, in one transaction of st, with the serial key of the imports
// of the user, so that no import runs meanwhile. A nil queue writes the job to the store without
// waking a worker. From then on, the records of the merged sources land in the target.
//
// It returns ErrTargetAmongMerged when sourceIDs holds targetID, store.ErrNotFound when userID has no
// source of one of the ids, ErrMergeInProgress when one of the sources is in a merge in progress,
// and store.ErrInvalid when sourceIDs is empty or holds an id twice. Nothing changes then.
func StartMerge(ctx context.Context, st store.Store, queue JobQueue, userID, targetID int64, sourceIDs []int64) error {
	if slices.Contains(sourceIDs, targetID) {
		return fmt.Errorf("merge sources of user %d: %w", userID, ErrTargetAmongMerged)
	}

	if queue == nil {
		queue = storeQueue{}
	}

	payload, err := json.Marshal(MergePayload{TargetID: targetID, SourceIDs: sourceIDs})
	if err != nil {
		return fmt.Errorf("merge sources of user %d: %w", userID, err)
	}

	err = st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		err := tx.Sources().StartMerge(ctx, userID, targetID, sourceIDs)
		if errors.Is(err, store.ErrConflict) {
			return ErrMergeInProgress
		}

		if err != nil {
			return err
		}

		_, err = queue.Enqueue(ctx, tx, store.NewJob{
			Kind:      KindMergeSources,
			Payload:   payload,
			UserID:    userID,
			SerialKey: SerialKey(userID),
		})

		return err
	})
	if err != nil {
		return fmt.Errorf("merge sources of user %d: %w", userID, err)
	}

	return nil
}

// Recomputer recomputes in full the aggregates of the types whose source selection changed.
type Recomputer interface {
	// RecomputeTypes records, through tx, the recomputation of the aggregates of userID of each type
	// of typeIDs.
	RecomputeTypes(ctx context.Context, tx store.Repositories, userID int64, typeIDs ...int64) error
}

// NoRecompute recomputes nothing, for a merger whose aggregates nobody reads, such as in tests. The
// server recomputes with aggregate.Recorder.
type NoRecompute struct{}

// RecomputeTypes does nothing.
func (NoRecompute) RecomputeTypes(context.Context, store.Repositories, int64, ...int64) error {
	return nil
}

// MergeConfig configures a Merger.
type MergeConfig struct {
	Store store.Store
	// DirtyRanges records the ranges of the samples that a merge removes as duplicates,
	// NoDirtyRanges when nil.
	DirtyRanges DirtyRanges
	// Duplicates flags the duplicate workouts of each batch, NoWorkoutDuplicates when nil.
	Duplicates WorkoutDuplicates
	// Recompute recomputes the aggregates of the types of the target once a merge ends, NoRecompute
	// when nil.
	Recompute Recomputer
	// BatchSize is the most records that a transaction moves, DefaultBatchSize when 0.
	BatchSize int
}

// Merger runs the merge jobs (FR-3 of Deduplication and source priority). It is safe for concurrent
// use.
type Merger struct {
	cfg MergeConfig
}

// NewMerger returns a merger of cfg.
func NewMerger(cfg MergeConfig) *Merger {
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultBatchSize
	}

	if cfg.DirtyRanges == nil {
		cfg.DirtyRanges = NoDirtyRanges{}
	}

	if cfg.Duplicates == nil {
		cfg.Duplicates = NoWorkoutDuplicates{}
	}

	if cfg.Recompute == nil {
		cfg.Recompute = NoRecompute{}
	}

	return &Merger{cfg: cfg}
}

// RegisterMerge adds the merge job kind to registry, running on m, and returns it.
func RegisterMerge(registry *jobs.Registry, m *Merger) jobs.Kind[MergePayload] {
	return jobs.Register(registry, KindMergeSources, jobs.KindOptions{}, func(ctx context.Context, job jobs.Job[MergePayload]) error {
		return m.Run(ctx, job.UserID, job.Payload)
	})
}

// Run moves the samples, then the workouts, of each source of payload still merged into its target
// to the target, in batches of at most BatchSize records, one transaction each. The natural key of
// a moved record whose key holds its source is computed again, and a moved record whose new key is
// stored is removed as a duplicate. The transaction of each batch records the dirty ranges of the
// samples it removes, and evaluates again the duplicate groups of the workouts it moves or removes.
//
// Once the last batch is committed, one transaction replaces each merged source by an alias of its
// identity to the target, which removes it from the priorities, and recomputes the aggregates of
// the types of the target.
//
// A batch only finds the records still on a merged source, so that a run stopped at any point
// resumes where it stopped, and a merge that ended does nothing. It returns the error of ctx once it
// is done, at the next batch boundary: the batches committed before stay.
func (m *Merger) Run(ctx context.Context, userID int64, payload MergePayload) error {
	merged, err := m.merging(ctx, userID, payload)
	if err != nil {
		return err
	}

	for _, sourceID := range merged {
		if err := m.moveAll(ctx, userID, sourceID, payload.TargetID, m.moveSamples); err != nil {
			return err
		}

		if err := m.moveAll(ctx, userID, sourceID, payload.TargetID, m.moveWorkouts); err != nil {
			return err
		}
	}

	if len(merged) == 0 {
		return nil
	}

	return m.finish(ctx, userID, payload.TargetID, merged)
}

// merging returns the ids of the sources of payload that are still merged into its target.
func (m *Merger) merging(ctx context.Context, userID int64, payload MergePayload) ([]int64, error) {
	sources, err := m.cfg.Store.Sources().List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("merge sources of user %d: %w", userID, err)
	}

	var merged []int64

	for _, source := range sources {
		if source.MergedInto == payload.TargetID && slices.Contains(payload.SourceIDs, source.ID) {
			merged = append(merged, source.ID)
		}
	}

	return merged, nil
}

// moveBatch moves one batch of the records of a kind of the source of fromID to the source of toID
// through tx, and reports whether none was left.
type moveBatch func(ctx context.Context, tx store.Repositories, userID, fromID, toID int64) (bool, error)

// moveAll runs move in a transaction each, until it reports that none was left.
func (m *Merger) moveAll(ctx context.Context, userID, fromID, toID int64, move moveBatch) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop merge of source %d of user %d: %w", fromID, userID, err)
		}

		var done bool

		err := m.cfg.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
			var err error

			done, err = move(ctx, tx, userID, fromID, toID)

			return err
		})
		if err != nil {
			return fmt.Errorf("merge source %d of user %d into %d: %w", fromID, userID, toID, err)
		}

		if done {
			return nil
		}
	}
}

// moveSamples moves a batch of samples, and records the ranges of those it removed as dirty.
func (m *Merger) moveSamples(ctx context.Context, tx store.Repositories, userID, fromID, toID int64) (bool, error) {
	samples, err := tx.Samples().ListOfSource(ctx, userID, fromID, m.cfg.BatchSize)
	if err != nil || len(samples) == 0 {
		return true, err
	}

	moves := make([]store.Move, 0, len(samples))
	for _, s := range samples {
		moves = append(moves, store.Move{ID: s.ID, NaturalKey: movedSampleKey(s, toID)})
	}

	removed, err := tx.Samples().MoveSource(ctx, userID, fromID, toID, moves)
	if err != nil {
		return false, fmt.Errorf("move samples: %w", err)
	}

	if len(removed) == 0 {
		return false, nil
	}

	dirty := make([]DirtyRange, 0, len(removed))
	for _, s := range removed {
		dirty = append(dirty, DirtyRange{TypeID: s.TypeID, From: s.Start, To: s.End})
	}

	if err := m.cfg.DirtyRanges.Record(ctx, tx, userID, dirty); err != nil {
		return false, fmt.Errorf("record dirty ranges: %w", err)
	}

	return false, nil
}

// moveWorkouts moves a batch of workouts, and evaluates again the duplicate groups of those it moved
// or removed.
func (m *Merger) moveWorkouts(ctx context.Context, tx store.Repositories, userID, fromID, toID int64) (bool, error) {
	workouts, err := tx.Workouts().ListOfSource(ctx, userID, fromID, m.cfg.BatchSize)
	if err != nil || len(workouts) == 0 {
		return true, err
	}

	moves := make([]store.Move, 0, len(workouts))
	changes := make([]WorkoutChange, 0, len(workouts))

	for _, w := range workouts {
		moves = append(moves, store.Move{ID: w.ID, NaturalKey: movedWorkoutKey(w, toID)})
		changes = append(changes, WorkoutChange{ID: w.ID, Start: w.Start, End: w.End})
	}

	if _, err := tx.Workouts().MoveSource(ctx, userID, fromID, toID, moves); err != nil {
		return false, fmt.Errorf("move workouts: %w", err)
	}

	if err := m.cfg.Duplicates.Evaluate(ctx, tx, userID, changes); err != nil {
		return false, fmt.Errorf("evaluate duplicate workouts: %w", err)
	}

	return false, nil
}

// finish replaces the merged sources by aliases of the target of toID, and recomputes the aggregates
// of the types of the target, those of the moved samples among them, in one transaction.
func (m *Merger) finish(ctx context.Context, userID, toID int64, merged []int64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop merge of sources of user %d: %w", userID, err)
	}

	err := m.cfg.Store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		for _, sourceID := range merged {
			if err := tx.Sources().FinishMerge(ctx, userID, sourceID); err != nil {
				return fmt.Errorf("finish merge of source %d: %w", sourceID, err)
			}
		}

		typeIDs, err := tx.Samples().TypesOfSource(ctx, userID, toID)
		if err != nil {
			return err
		}

		return m.cfg.Recompute.RecomputeTypes(ctx, tx, userID, typeIDs...)
	})
	if err != nil {
		return fmt.Errorf("finish merge of sources of user %d into %d: %w", userID, toID, err)
	}

	return nil
}
