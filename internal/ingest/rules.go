package ingest

import (
	"context"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// BlockCandidate is a record that a block rule may drop, once its type and source are known.
type BlockCandidate struct {
	// TypeID is the metric type of a sample, 0 for a workout.
	TypeID   int64
	SourceID int64
	Start    time.Time
	// UUID is the uuid of the record, empty when it has none.
	UUID       string
	NaturalKey []byte
}

// BlockRules drops the records that an active block rule of their user matches (FR-2 stage 5 of
// Ingestion pipeline).
type BlockRules interface {
	// Blocks reports whether a rule of userID blocks c, reading through tx, the transaction of the
	// batch of c.
	Blocks(ctx context.Context, tx store.Repositories, userID int64, c BlockCandidate) (bool, error)
}

// NoBlockRules is the block rule set until Deletion and block rules: it blocks nothing.
type NoBlockRules struct{}

// Blocks reports false.
func (NoBlockRules) Blocks(context.Context, store.Repositories, int64, BlockCandidate) (bool, error) {
	return false, nil
}

// DirtyRange is a time range of a type whose aggregates a batch changed.
type DirtyRange struct {
	TypeID int64
	// From is the start of the first sample of the range, and To the end of the last one.
	From, To time.Time
}

// DirtyRanges records the ranges that a batch changed, so that their aggregates are recomputed
// once it is committed (FR-2 stage 8 of Ingestion pipeline).
type DirtyRanges interface {
	// Record records ranges of userID in tx, the transaction of their batch.
	Record(ctx context.Context, tx store.Repositories, userID int64, ranges []DirtyRange) error
}

// NoDirtyRanges records nothing, for a pipeline whose aggregates nobody reads, such as in tests.
// The server records with aggregate.Recorder.
type NoDirtyRanges struct{}

// Record does nothing.
func (NoDirtyRanges) Record(context.Context, store.Repositories, int64, []DirtyRange) error {
	return nil
}

// WorkoutChange is a workout that a batch stored, updated or removed.
type WorkoutChange struct {
	ID int64
	// Start and End are the times of the workout as stored, or as it was for a removed one.
	Start, End time.Time
}

// WorkoutDuplicates flags the duplicate workouts (FR-11 of Deduplication and source priority).
type WorkoutDuplicates interface {
	// Evaluate evaluates again, through tx, the transaction of their batch, the duplicate groups
	// that changes, the workouts of userID that the batch stored, updated or removed, overlap.
	Evaluate(ctx context.Context, tx store.Repositories, userID int64, changes []WorkoutChange) error
}

// NoWorkoutDuplicates flags nothing, for a pipeline whose workouts lists nobody reads, such as in
// tests. The server flags with workouts.Detector.
type NoWorkoutDuplicates struct{}

// Evaluate does nothing.
func (NoWorkoutDuplicates) Evaluate(context.Context, store.Repositories, int64, []WorkoutChange) error {
	return nil
}
