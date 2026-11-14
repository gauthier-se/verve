package store

import (
	"context"
	"errors"
)

// ErrCombineNotDiscrete is returned when combine mode is set on a type that is not a discrete
// quantity (FR-7 of Deduplication and source priority). Its message is a stable code.
var ErrCombineNotDiscrete = errors.New("combine_not_discrete")

// SourcePriorities are the priorities of the sources of a user for a metric type, as the user set
// them (FR-4 to FR-7 of Deduplication and source priority). aggregate.ResolvePriorities places the
// sources they leave out.
type SourcePriorities struct {
	// Order holds the ids of the ordered sources, highest priority first.
	Order []int64
	// Ignored holds the ids of the ignored sources, which never contribute.
	Ignored []int64
	// Combine makes every source that is not ignored contribute, for a discrete quantity type.
	Combine bool
}

// SourcePrioritiesRepository stores the priorities that each user set, per metric type. A source
// that is deleted leaves the priorities it was in, and the priorities of a user are deleted with
// them.
type SourcePrioritiesRepository interface {
	// Get returns the priorities of userID for the type of typeID, or ErrNotFound when the user set
	// none.
	Get(ctx context.Context, userID, typeID int64) (SourcePriorities, error)
	// Set replaces the priorities of userID for the type of typeID with priorities, in one
	// transaction. It returns ErrNotFound when no user has userID or no type has typeID,
	// ErrCombineNotDiscrete when priorities combine sources of a type that is not a discrete
	// quantity, and ErrInvalid when they hold a source that userID does not have, or a source twice,
	// ordered and ignored included. It writes nothing when it fails.
	Set(ctx context.Context, userID, typeID int64, priorities SourcePriorities) error
}
