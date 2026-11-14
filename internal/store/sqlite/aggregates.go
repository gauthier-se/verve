package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// aggregates implements store.AggregatesRepository.
type aggregates struct {
	q queries
}

func (r aggregates) RecordDirty(ctx context.Context, userID int64, ranges []store.DirtyRange) error {
	for i, dirty := range ranges {
		if dirty.To.Before(dirty.From) {
			return fmt.Errorf("record dirty ranges of user %d: range %d ends before it starts: %w", userID, i, store.ErrInvalid)
		}
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		for _, dirty := range ranges {
			err := q.InsertAggregateDirtyRange(ctx, sqlcgen.InsertAggregateDirtyRangeParams{
				UserID: userID,
				TypeID: dirty.TypeID,
				FromAt: toMillis(dirty.From),
				ToAt:   toMillis(dirty.To),
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("record dirty ranges of user %d: %w", userID, err)
	}

	return nil
}

func (r aggregates) ListDirty(ctx context.Context, userID int64) ([]store.DirtyRange, error) {
	rows, err := r.q.read().ListUserAggregateDirtyRanges(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list dirty ranges of user %d: %w", userID, err)
	}

	ranges := make([]store.DirtyRange, 0, len(rows))
	for _, row := range rows {
		ranges = append(ranges, store.DirtyRange{ID: row.ID, TypeID: row.TypeID, From: fromMillis(row.FromAt), To: fromMillis(row.ToAt)})
	}

	return ranges, nil
}

func (r aggregates) DeleteDirty(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.DeleteUserAggregateDirtyRanges(ctx, sqlcgen.DeleteUserAggregateDirtyRangesParams{UserID: userID, Ids: ids})
	})
	if err != nil {
		return fmt.Errorf("delete dirty ranges of user %d: %w", userID, err)
	}

	return nil
}

func (r aggregates) ReplaceHourly(ctx context.Context, userID, typeID int64, from, to time.Time, rows []store.AggregateRow) error {
	if err := checkAggregateRows(from, to, rows, false); err != nil {
		return fmt.Errorf("replace hourly aggregates of user %d: %w", userID, err)
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		err := q.DeleteUserAggHourlyRange(ctx, sqlcgen.DeleteUserAggHourlyRangeParams{
			UserID: userID, TypeID: typeID, FromAt: toMillis(from), ToAt: toMillis(to),
		})
		if err != nil {
			return err
		}

		for _, row := range rows {
			err := q.InsertAggHourly(ctx, sqlcgen.InsertAggHourlyParams{
				UserID:      userID,
				TypeID:      typeID,
				BucketStart: toMillis(row.Start),
				Sum:         row.Sum,
				Count:       row.Count,
				Min:         nullFloat(row.Min),
				Max:         nullFloat(row.Max),
				Last:        nullFloat(row.Last),
				LastAt:      nullMillis(row.LastAt),
				Selection:   string(row.Selection),
				SourceID:    nullSourceID(row.SourceID),
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("replace hourly aggregates of user %d: %w", userID, err)
	}

	return nil
}

func (r aggregates) ReplaceDaily(ctx context.Context, userID, typeID int64, from, to time.Time, rows []store.AggregateRow) error {
	if err := checkAggregateRows(from, to, rows, true); err != nil {
		return fmt.Errorf("replace daily aggregates of user %d: %w", userID, err)
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		err := q.DeleteUserAggDailyRange(ctx, sqlcgen.DeleteUserAggDailyRangeParams{
			UserID: userID, TypeID: typeID, FromAt: toMillis(from), ToAt: toMillis(to),
		})
		if err != nil {
			return err
		}

		for _, row := range rows {
			err := q.InsertAggDaily(ctx, sqlcgen.InsertAggDailyParams{
				UserID:      userID,
				TypeID:      typeID,
				BucketStart: toMillis(row.Start),
				Sum:         row.Sum,
				Count:       row.Count,
				Min:         nullFloat(row.Min),
				Max:         nullFloat(row.Max),
				Last:        nullFloat(row.Last),
				LastAt:      nullMillis(row.LastAt),
				Selection:   string(row.Selection),
				SourceID:    nullSourceID(row.SourceID),
				Hours:       row.Hours,
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("replace daily aggregates of user %d: %w", userID, err)
	}

	return nil
}

func (r aggregates) ListHourly(ctx context.Context, userID, typeID int64, from, to time.Time) ([]store.AggregateRow, error) {
	rows, err := r.q.read().ListUserAggHourly(ctx, sqlcgen.ListUserAggHourlyParams{
		UserID: userID, TypeID: typeID, FromAt: toMillis(from), ToAt: toMillis(to),
	})
	if err != nil {
		return nil, fmt.Errorf("list hourly aggregates of user %d: %w", userID, err)
	}

	list := make([]store.AggregateRow, 0, len(rows))
	for _, row := range rows {
		list = append(list, aggregateRowOf(row.BucketStart, row.Sum, row.Count, row.Min, row.Max, row.Last, row.LastAt,
			row.Selection, row.SourceID, 0))
	}

	return list, nil
}

func (r aggregates) ListDaily(ctx context.Context, userID, typeID int64, from, to time.Time) ([]store.AggregateRow, error) {
	rows, err := r.q.read().ListUserAggDaily(ctx, sqlcgen.ListUserAggDailyParams{
		UserID: userID, TypeID: typeID, FromAt: toMillis(from), ToAt: toMillis(to),
	})
	if err != nil {
		return nil, fmt.Errorf("list daily aggregates of user %d: %w", userID, err)
	}

	list := make([]store.AggregateRow, 0, len(rows))
	for _, row := range rows {
		list = append(list, aggregateRowOf(row.BucketStart, row.Sum, row.Count, row.Min, row.Max, row.Last, row.LastAt,
			row.Selection, row.SourceID, row.Hours))
	}

	return list, nil
}

func (r aggregates) DeleteAll(ctx context.Context, userID int64) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		if err := q.DeleteAllUserAggHourly(ctx, userID); err != nil {
			return err
		}

		return q.DeleteAllUserAggDaily(ctx, userID)
	})
	if err != nil {
		return fmt.Errorf("delete aggregates of user %d: %w", userID, err)
	}

	return nil
}

// DeleteByUser removes the hourly rows first, then the daily rows, then the dirty ranges, each
// table taking what the batch has left of limit.
func (r aggregates) DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete aggregates of user %d: limit %d below 1: %w", userID, limit, store.ErrInvalid)
	}

	var deleted int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		batches := []func(left int64) (int64, error){
			func(left int64) (int64, error) {
				return q.DeleteUserAggHourlyBatch(ctx, sqlcgen.DeleteUserAggHourlyBatchParams{UserID: userID, BatchSize: left})
			},
			func(left int64) (int64, error) {
				return q.DeleteUserAggDailyBatch(ctx, sqlcgen.DeleteUserAggDailyBatchParams{UserID: userID, BatchSize: left})
			},
			func(left int64) (int64, error) {
				return q.DeleteUserAggregateDirtyRangesBatch(ctx, sqlcgen.DeleteUserAggregateDirtyRangesBatchParams{
					UserID: userID, BatchSize: left,
				})
			},
		}

		for _, batch := range batches {
			removed, err := batch(int64(limit) - deleted)
			if err != nil {
				return err
			}

			deleted += removed

			if deleted == int64(limit) {
				return nil
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("delete aggregates of user %d: %w", userID, err)
	}

	return deleted, nil
}

// checkAggregateRows returns ErrInvalid when a row of rows starts outside [from, to), two rows
// start at the same time, or a row is not one of an hour, or of a day when daily is true.
func checkAggregateRows(from, to time.Time, rows []store.AggregateRow, daily bool) error {
	starts := make(map[int64]bool, len(rows))

	for i, row := range rows {
		start := toMillis(row.Start)

		switch {
		case row.Start.Before(from) || !row.Start.Before(to):
			return fmt.Errorf("row %d starts out of its range: %w", i, store.ErrInvalid)
		case starts[start]:
			return fmt.Errorf("row %d starts with another one: %w", i, store.ErrInvalid)
		case row.Count < 1:
			return fmt.Errorf("row %d has a count below 1: %w", i, store.ErrInvalid)
		case daily && row.Hours < 1, !daily && row.Hours != 0:
			return fmt.Errorf("row %d has %d hours: %w", i, row.Hours, store.ErrInvalid)
		case !row.Selection.Known():
			return fmt.Errorf("row %d has the selection %q: %w", i, row.Selection, store.ErrInvalid)
		case (row.Selection == store.SelectedSource) != (row.SourceID != 0):
			return fmt.Errorf("row %d has the selection %q with the source %d: %w", i, row.Selection, row.SourceID, store.ErrInvalid)
		}

		starts[start] = true
	}

	return nil
}

// nullSourceID returns the source id of an aggregate row, NULL for 0.
func nullSourceID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id != 0}
}

// aggregateRowOf returns the aggregate row of the columns of agg_hourly or agg_daily.
func aggregateRowOf(start int64, sum float64, count int64, low, high, last sql.NullFloat64, lastAt sql.NullInt64,
	selection string, sourceID sql.NullInt64, hours int64,
) store.AggregateRow {
	return store.AggregateRow{
		Start:     fromMillis(start),
		Sum:       sum,
		Count:     count,
		Min:       fromNullFloat(low),
		Max:       fromNullFloat(high),
		Last:      fromNullFloat(last),
		LastAt:    fromNullMillis(lastAt),
		Selection: store.Selection(selection),
		SourceID:  sourceID.Int64,
		Hours:     hours,
	}
}

func (r aggregates) RecordAllDirty(ctx context.Context, userID int64) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		return q.InsertUserAggregateDirtyRangesFromStats(ctx, userID)
	})
	if err != nil {
		return fmt.Errorf("record dirty ranges of every type of user %d: %w", userID, err)
	}

	return nil
}
