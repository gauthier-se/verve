package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// sourcePriorities implements store.SourcePrioritiesRepository.
type sourcePriorities struct {
	q   queries
	clk clock.Clock
}

// Get reads the row of the type and its entries in one statement, so that a write meanwhile cannot
// mix two versions of the priorities.
func (r sourcePriorities) Get(ctx context.Context, userID, typeID int64) (store.SourcePriorities, error) {
	rows, err := r.q.read().GetSourcePriorities(ctx, sqlcgen.GetSourcePrioritiesParams{UserID: userID, TypeID: typeID})
	if err == nil && len(rows) == 0 {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.SourcePriorities{}, fmt.Errorf("get source priorities of user %d for type %d: %w", userID, typeID, err)
	}

	priorities := store.SourcePriorities{Combine: rows[0].Combine == 1}

	for _, row := range rows {
		switch {
		case !row.SourceID.Valid:
			// The single row of priorities that list no source.
		case row.Ignored.Int64 == 1:
			priorities.Ignored = append(priorities.Ignored, row.SourceID.Int64)
		default:
			priorities.Order = append(priorities.Order, row.SourceID.Int64)
		}
	}

	return priorities, nil
}

// Set replaces the row of the type and every entry, so that the sources left out leave the
// priorities. The foreign keys of the entries refuse a source that the user does not have.
func (r sourcePriorities) Set(ctx context.Context, userID, typeID int64, priorities store.SourcePriorities) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		if err := checkSourcePriorities(ctx, q, typeID, priorities); err != nil {
			return err
		}

		err := q.SetSourcePriorities(ctx, sqlcgen.SetSourcePrioritiesParams{
			UserID:  userID,
			TypeID:  typeID,
			Combine: sqliteBool(priorities.Combine),
		})

		// The type exists, so the failed foreign key is the user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		if err := q.DeleteSourcePriorityEntries(ctx, sqlcgen.DeleteSourcePriorityEntriesParams{UserID: userID, TypeID: typeID}); err != nil {
			return err
		}

		for ignored, ids := range [][]int64{priorities.Order, priorities.Ignored} {
			for position, id := range ids {
				err := q.CreateSourcePriorityEntry(ctx, sqlcgen.CreateSourcePriorityEntryParams{
					UserID:   userID,
					TypeID:   typeID,
					SourceID: id,
					Ignored:  int64(ignored),
					Position: int64(position),
				})

				// The priorities exist, so the failed foreign key is the source.
				if isForeignKeyError(err) {
					return store.ErrInvalid
				}

				if err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("set source priorities of user %d for type %d: %w", userID, typeID, err)
	}

	return nil
}

// checkSourcePriorities returns store.ErrNotFound when no type has typeID,
// store.ErrCombineNotDiscrete when priorities combine sources of a type that is not a discrete
// quantity, and store.ErrInvalid when they list a source twice.
func checkSourcePriorities(ctx context.Context, q *sqlcgen.Queries, typeID int64, priorities store.SourcePriorities) error {
	metricType, err := q.GetMetricType(ctx, typeID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}

	if err != nil {
		return err
	}

	if priorities.Combine && (catalog.Kind(metricType.Kind) != catalog.KindQuantity ||
		catalog.Aggregation(metricType.Aggregation) != catalog.AggregationDiscrete) {
		return store.ErrCombineNotDiscrete
	}

	seen := make(map[int64]bool, len(priorities.Order)+len(priorities.Ignored))

	for _, ids := range [][]int64{priorities.Order, priorities.Ignored} {
		for _, id := range ids {
			if seen[id] {
				return fmt.Errorf("source %d listed twice: %w", id, store.ErrInvalid)
			}

			seen[id] = true
		}
	}

	return nil
}

// sqliteBool returns the integer that stores b.
func sqliteBool(b bool) int64 {
	if b {
		return 1
	}

	return 0
}
