package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// aggregatesTime is the first hour of the suite.
var aggregatesTime = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// aggregatesTest holds a store with two users, a source of each, and two types.
type aggregatesTest struct {
	store   store.Store
	user    store.User
	other   store.User
	source  store.Source
	foreign store.Source
	heart   store.MetricType
	steps   store.MetricType
}

func newAggregatesTest(t *testing.T, newStore NewStoreFunc) aggregatesTest {
	t.Helper()

	s := newStore(t, clock.NewFake(aggregatesTime))
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s.Users(), newUser("camille", ""))
	other := mustCreateUser(ctx, t, s.Users(), newUser("dominique", ""))

	heart, err := s.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierHeartRate", catalog.KindQuantity, "count/min")
	require.NoError(t, err)

	steps, err := s.MetricTypes().EnsureUncurated(ctx, "HKQuantityTypeIdentifierStepCount", catalog.KindQuantity, "count")
	require.NoError(t, err)

	return aggregatesTest{
		store:   s,
		user:    user,
		other:   other,
		source:  mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, aggregatesTime),
		foreign: mustResolve(ctx, t, s.Sources(), other.ID, watchInfo, store.SourceWatch, aggregatesTime),
		heart:   heart,
		steps:   steps,
	}
}

// hour returns the start of the hour hours after aggregatesTime.
func hour(hours int) time.Time {
	return aggregatesTime.Add(time.Duration(hours) * time.Hour)
}

// discreteRow returns an hourly row of a discrete type starting hours after aggregatesTime, of the
// source of sourceID.
func discreteRow(hours int, sum float64, sourceID int64) store.AggregateRow {
	start := hour(hours)

	return store.AggregateRow{
		Start: start, Sum: sum, Count: 2, Min: ptr(sum / 4), Max: ptr(sum * 3 / 4), Last: ptr(sum / 2),
		LastAt: start.Add(42*time.Minute + 123*time.Millisecond), Selection: store.SelectedSource, SourceID: sourceID,
	}
}

// cumulativeRow returns an hourly row of a cumulative type starting hours after aggregatesTime.
func cumulativeRow(hours int, sum float64, selection store.Selection, sourceID int64) store.AggregateRow {
	return store.AggregateRow{Start: hour(hours), Sum: sum, Count: 3, Selection: selection, SourceID: sourceID}
}

func (at aggregatesTest) mustReplaceHourly(ctx context.Context, t *testing.T, userID, typeID int64, from, to time.Time,
	rows ...store.AggregateRow,
) {
	t.Helper()

	require.NoError(t, at.store.Aggregates().ReplaceHourly(ctx, userID, typeID, from, to, rows))
}

func (at aggregatesTest) mustListHourly(ctx context.Context, t *testing.T, userID, typeID int64) []store.AggregateRow {
	t.Helper()

	rows, err := at.store.Aggregates().ListHourly(ctx, userID, typeID, hour(-1000), hour(1000))
	require.NoError(t, err)

	return rows
}

func (at aggregatesTest) mustListDaily(ctx context.Context, t *testing.T, userID, typeID int64) []store.AggregateRow {
	t.Helper()

	rows, err := at.store.Aggregates().ListDaily(ctx, userID, typeID, hour(-1000), hour(1000))
	require.NoError(t, err)

	return rows
}

func (at aggregatesTest) mustListDirty(ctx context.Context, t *testing.T, userID int64) []store.DirtyRange {
	t.Helper()

	ranges, err := at.store.Aggregates().ListDirty(ctx, userID)
	require.NoError(t, err)

	return ranges
}

// rangesWithoutIDs returns ranges without their ids.
func rangesWithoutIDs(ranges []store.DirtyRange) []store.DirtyRange {
	stripped := make([]store.DirtyRange, 0, len(ranges))

	for _, r := range ranges {
		r.ID = 0
		stripped = append(stripped, r)
	}

	return stripped
}

// RunAggregates runs the AggregatesRepository contract against the stores returned by newStore.
func RunAggregates(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runAggregatesRows(t, newStore)
	runAggregatesRowsRefused(t, newStore)
	runAggregatesDirty(t, newStore)
	runAggregatesDelete(t, newStore)
}

// runAggregatesRows runs the cases of the hourly and daily rows.
func runAggregatesRows(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given hourly rows of each selection, with and without min, max and last, then ListHourly
	// returns them with every field, sorted by start, and only those of the user and type asked for.
	// Given ReplaceHourly of a range again, then the rows of that range are replaced, the rows left
	// out included, and the rows outside it stay.
	t.Run("ReplaceHourlyReplacesTheRowsOfTheRange", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		at.mustReplaceHourly(ctx, t, at.user.ID, at.heart.ID, hour(0), hour(4),
			discreteRow(2, 280, at.source.ID), discreteRow(0, 300, at.source.ID),
			store.AggregateRow{Start: hour(3), Sum: 90, Count: 1, Min: ptr(90), Max: ptr(90), Last: ptr(90), LastAt: hour(3), Selection: store.SelectedCombined},
		)
		at.mustReplaceHourly(ctx, t, at.user.ID, at.steps.ID, hour(0), hour(4), cumulativeRow(1, 1234.5, store.SelectedSource, at.source.ID))
		at.mustReplaceHourly(ctx, t, at.other.ID, at.heart.ID, hour(0), hour(4), discreteRow(1, 400, at.foreign.ID))

		require.Equal(t, []store.AggregateRow{
			discreteRow(0, 300, at.source.ID),
			discreteRow(2, 280, at.source.ID),
			{Start: hour(3), Sum: 90, Count: 1, Min: ptr(90), Max: ptr(90), Last: ptr(90), LastAt: hour(3), Selection: store.SelectedCombined},
		}, at.mustListHourly(ctx, t, at.user.ID, at.heart.ID))
		require.Equal(t, []store.AggregateRow{cumulativeRow(1, 1234.5, store.SelectedSource, at.source.ID)},
			at.mustListHourly(ctx, t, at.user.ID, at.steps.ID))

		at.mustReplaceHourly(ctx, t, at.user.ID, at.heart.ID, hour(2), hour(4), discreteRow(2, 100, at.source.ID))

		require.Equal(t, []store.AggregateRow{discreteRow(0, 300, at.source.ID), discreteRow(2, 100, at.source.ID)},
			at.mustListHourly(ctx, t, at.user.ID, at.heart.ID))

		rows, err := at.store.Aggregates().ListHourly(ctx, at.user.ID, at.heart.ID, hour(1), hour(2))
		require.NoError(t, err)
		require.Empty(t, rows)
		require.NotNil(t, rows)

		require.Equal(t, []store.AggregateRow{discreteRow(1, 400, at.foreign.ID)}, at.mustListHourly(ctx, t, at.other.ID, at.heart.ID))
	})

	// Given daily rows, then ListDaily returns them with their hours, apart from the hourly rows,
	// and ReplaceDaily replaces those of its range.
	t.Run("ReplaceDailyReplacesTheRowsOfTheRange", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		day := func(days int, sum float64, selection store.Selection, sourceID int64) store.AggregateRow {
			row := cumulativeRow(24*days, sum, selection, sourceID)
			row.Hours = 5

			return row
		}

		require.NoError(t, at.store.Aggregates().ReplaceDaily(ctx, at.user.ID, at.steps.ID, hour(0), hour(72), []store.AggregateRow{
			day(0, 8000, store.SelectedSource, at.source.ID), day(1, 9000, store.SelectedMixed, 0), day(2, 7000, store.SelectedSource, at.source.ID),
		}))
		require.NoError(t, at.store.Aggregates().ReplaceDaily(ctx, at.user.ID, at.steps.ID, hour(24), hour(48), nil))

		require.Equal(t, []store.AggregateRow{day(0, 8000, store.SelectedSource, at.source.ID), day(2, 7000, store.SelectedSource, at.source.ID)},
			at.mustListDaily(ctx, t, at.user.ID, at.steps.ID))
		require.Empty(t, at.mustListHourly(ctx, t, at.user.ID, at.steps.ID))
	})
}

// runAggregatesRowsRefused runs the cases of the rows that the store refuses.
func runAggregatesRowsRefused(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a row that is not one of its range or of its table, then ReplaceHourly and ReplaceDaily
	// return store.ErrInvalid and change nothing, the rows of the range included.
	t.Run("ReplaceRefusesInvalidRows", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		kept := discreteRow(1, 300, at.source.ID)
		at.mustReplaceHourly(ctx, t, at.user.ID, at.heart.ID, hour(0), hour(4), kept)

		valid := discreteRow(2, 100, at.source.ID)
		invalid := map[string]store.AggregateRow{
			"before the range":     discreteRow(-1, 100, at.source.ID),
			"at the end":           discreteRow(4, 100, at.source.ID),
			"count below 1":        {Start: hour(2), Selection: store.SelectedSource, SourceID: at.source.ID},
			"unknown selection":    {Start: hour(2), Count: 1, Selection: "best"},
			"source without id":    {Start: hour(2), Count: 1, Selection: store.SelectedSource},
			"combined with source": {Start: hour(2), Count: 1, Selection: store.SelectedCombined, SourceID: at.source.ID},
			"hours on an hour":     {Start: hour(2), Count: 1, Selection: store.SelectedCombined, Hours: 1},
		}

		for name, row := range invalid {
			err := at.store.Aggregates().ReplaceHourly(ctx, at.user.ID, at.heart.ID, hour(0), hour(4), []store.AggregateRow{valid, row})
			require.ErrorIs(t, err, store.ErrInvalid, name)
		}

		err := at.store.Aggregates().ReplaceHourly(ctx, at.user.ID, at.heart.ID, hour(0), hour(4), []store.AggregateRow{valid, valid})
		require.ErrorIs(t, err, store.ErrInvalid, "two rows of one hour")

		err = at.store.Aggregates().ReplaceDaily(ctx, at.user.ID, at.heart.ID, hour(0), hour(4), []store.AggregateRow{valid})
		require.ErrorIs(t, err, store.ErrInvalid, "day without hours")

		require.Equal(t, []store.AggregateRow{kept}, at.mustListHourly(ctx, t, at.user.ID, at.heart.ID))
		require.Empty(t, at.mustListDaily(ctx, t, at.user.ID, at.heart.ID))
	})
}

// runAggregatesDirty runs the cases of the dirty ranges.
func runAggregatesDirty(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given dirty ranges of two types and two users, then ListDirty returns those of the user,
	// sorted by type, then start, then id, with their ids. Given DeleteDirtyThrough the largest id
	// listed, then the ranges of the user listed are removed, and the range recorded since and those
	// of another user, of smaller ids included, stay.
	t.Run("RecordDirtyStoresTheRangesUntilDeleteDirtyThrough", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		first, second := min(at.heart.ID, at.steps.ID), max(at.heart.ID, at.steps.ID)

		require.NoError(t, at.store.Aggregates().RecordDirty(ctx, at.other.ID, []store.DirtyRange{
			{TypeID: first, From: hour(0), To: hour(2)},
		}))
		require.NoError(t, at.store.Aggregates().RecordDirty(ctx, at.user.ID, []store.DirtyRange{
			{TypeID: second, From: hour(5), To: hour(6)},
			{TypeID: first, From: hour(3), To: hour(4)},
			{TypeID: first, From: hour(1), To: hour(1)},
			{TypeID: first, From: hour(3), To: hour(9)},
		}))

		ranges := at.mustListDirty(ctx, t, at.user.ID)
		require.Equal(t, []store.DirtyRange{
			{TypeID: first, From: hour(1), To: hour(1)},
			{TypeID: first, From: hour(3), To: hour(4)},
			{TypeID: first, From: hour(3), To: hour(9)},
			{TypeID: second, From: hour(5), To: hour(6)},
		}, rangesWithoutIDs(ranges))
		require.Less(t, ranges[1].ID, ranges[2].ID)

		foreign := at.mustListDirty(ctx, t, at.other.ID)
		require.Len(t, foreign, 1)

		require.NoError(t, at.store.Aggregates().RecordDirty(ctx, at.user.ID, []store.DirtyRange{
			{TypeID: first, From: hour(0), To: hour(1)},
		}))

		last := max(ranges[0].ID, ranges[1].ID, ranges[2].ID, ranges[3].ID)
		require.NoError(t, at.store.Aggregates().DeleteDirtyThrough(ctx, at.user.ID, last))

		left := at.mustListDirty(ctx, t, at.user.ID)
		require.Equal(t, []store.DirtyRange{{TypeID: first, From: hour(0), To: hour(1)}}, rangesWithoutIDs(left))
		require.Greater(t, left[0].ID, last)
		require.Equal(t, foreign, at.mustListDirty(ctx, t, at.other.ID))
	})

	// Given samples of two types, among them a long one that does not start last, then RecordAllDirty
	// records a range of each type from the first start of its samples to the last start widened by
	// the longest duration, which holds the end of every sample, and nothing for the other user.
	t.Run("RecordAllDirtyRecordsARangeOfEachTypeThatHoldsItsSamples", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		sample := func(key string, typeID, sourceID int64, start, end time.Time) store.NewSample {
			return store.NewSample{
				Sample: store.Sample{
					TypeID: typeID, SourceID: sourceID, Start: start, End: end, Value: 1, Origin: store.OriginXML,
					NaturalKey: []byte(key),
				},
				OnConflict: store.ConflictIgnore,
			}
		}

		_, err := at.store.Samples().Upsert(ctx, at.user.ID, []store.NewSample{
			sample("long", at.steps.ID, at.source.ID, hour(0), hour(5)),
			sample("short", at.steps.ID, at.source.ID, hour(2), hour(3)),
			sample("instant", at.heart.ID, at.source.ID, hour(1), hour(1)),
		})
		require.NoError(t, err)

		_, err = at.store.Samples().Upsert(ctx, at.other.ID, []store.NewSample{sample("foreign", at.heart.ID, at.foreign.ID, hour(0), hour(1))})
		require.NoError(t, err)

		require.NoError(t, at.store.Aggregates().RecordAllDirty(ctx, at.user.ID))

		require.ElementsMatch(t, []store.DirtyRange{
			{TypeID: at.steps.ID, From: hour(0), To: hour(7)},
			{TypeID: at.heart.ID, From: hour(1), To: hour(1)},
		}, rangesWithoutIDs(at.mustListDirty(ctx, t, at.user.ID)))
		require.Empty(t, at.mustListDirty(ctx, t, at.other.ID))
	})

	// Given a range that ends before it starts, then RecordDirty returns store.ErrInvalid and stores
	// none of the ranges.
	t.Run("RecordDirtyRefusesARangeEndingBeforeItStarts", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		err := at.store.Aggregates().RecordDirty(ctx, at.user.ID, []store.DirtyRange{
			{TypeID: at.heart.ID, From: hour(0), To: hour(1)},
			{TypeID: at.heart.ID, From: hour(2), To: hour(1)},
		})
		require.ErrorIs(t, err, store.ErrInvalid)
		require.Empty(t, at.mustListDirty(ctx, t, at.user.ID))
		require.NotNil(t, at.mustListDirty(ctx, t, at.user.ID))
	})

	// Given a dirty range of a user and type, then AnyDirty reports the periods [from, to) that it
	// overlaps, its end included since it can be the instant of a sample, and no period of another
	// type or another user.
	t.Run("AnyDirtyReportsTheRangesOverlappingAPeriod", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		require.NoError(t, at.store.Aggregates().RecordDirty(ctx, at.user.ID, []store.DirtyRange{
			{TypeID: at.steps.ID, From: hour(2), To: hour(4)},
		}))
		require.NoError(t, at.store.Aggregates().RecordDirty(ctx, at.other.ID, []store.DirtyRange{
			{TypeID: at.heart.ID, From: hour(0), To: hour(10)},
		}))

		for _, tc := range []struct {
			name     string
			userID   int64
			typeID   int64
			from, to int
			want     bool
		}{
			{"inside", at.user.ID, at.steps.ID, 3, 4, true},
			{"around", at.user.ID, at.steps.ID, 0, 10, true},
			{"at its end", at.user.ID, at.steps.ID, 4, 5, true},
			{"ending at its start", at.user.ID, at.steps.ID, 0, 2, false},
			{"after", at.user.ID, at.steps.ID, 5, 6, false},
			{"of another type", at.user.ID, at.heart.ID, 0, 10, false},
			{"of another user", at.other.ID, at.steps.ID, 0, 10, false},
		} {
			got, err := at.store.Aggregates().AnyDirty(ctx, tc.userID, tc.typeID, hour(tc.from), hour(tc.to))
			require.NoError(t, err)
			require.Equal(t, tc.want, got, tc.name)
		}
	})
}

// runAggregatesDelete runs the cases of the removal of the rows of a user.
func runAggregatesDelete(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// seed stores two hourly rows, a daily row and two dirty ranges of userID, of the source of
	// sourceID.
	seed := func(ctx context.Context, t *testing.T, at aggregatesTest, userID, sourceID int64) {
		t.Helper()

		at.mustReplaceHourly(ctx, t, userID, at.heart.ID, hour(0), hour(2), discreteRow(0, 100, sourceID), discreteRow(1, 200, sourceID))

		daily := discreteRow(0, 300, sourceID)
		daily.Hours = 2
		require.NoError(t, at.store.Aggregates().ReplaceDaily(ctx, userID, at.heart.ID, hour(0), hour(24), []store.AggregateRow{daily}))

		require.NoError(t, at.store.Aggregates().RecordDirty(ctx, userID, []store.DirtyRange{
			{TypeID: at.heart.ID, From: hour(0), To: hour(1)},
			{TypeID: at.steps.ID, From: hour(0), To: hour(1)},
		}))
	}

	// Given rows of two users, then DeleteAll removes the hourly and daily rows of the user, and
	// keeps their dirty ranges and the rows of the other user.
	t.Run("DeleteAllRemovesTheRowsOfTheUser", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		seed(ctx, t, at, at.user.ID, at.source.ID)
		seed(ctx, t, at, at.other.ID, at.foreign.ID)

		require.NoError(t, at.store.Aggregates().DeleteAll(ctx, at.user.ID))

		require.Empty(t, at.mustListHourly(ctx, t, at.user.ID, at.heart.ID))
		require.Empty(t, at.mustListDaily(ctx, t, at.user.ID, at.heart.ID))
		require.Len(t, at.mustListDirty(ctx, t, at.user.ID), 2)
		require.Len(t, at.mustListHourly(ctx, t, at.other.ID, at.heart.ID), 2)
		require.Len(t, at.mustListDaily(ctx, t, at.other.ID, at.heart.ID), 1)
	})

	// Given an account deletion, when DeleteByUser runs in batches of 2, then each batch removes at
	// most 2 rows of the hourly and daily rows and dirty ranges of the user, until a batch removes
	// fewer, and the rows of the other user stay. Given a limit below 1, then store.ErrInvalid.
	t.Run("DeleteByUserRemovesTheRowsAndRangesOfTheUserInBatches", func(t *testing.T) {
		t.Parallel()

		at := newAggregatesTest(t, newStore)
		ctx := t.Context()

		seed(ctx, t, at, at.user.ID, at.source.ID)
		seed(ctx, t, at, at.other.ID, at.foreign.ID)

		var batches []int64

		for {
			deleted, err := at.store.Aggregates().DeleteByUser(ctx, at.user.ID, 2)
			require.NoError(t, err)

			batches = append(batches, deleted)

			if deleted < 2 {
				break
			}
		}

		require.Equal(t, []int64{2, 2, 1}, batches)
		require.Empty(t, at.mustListHourly(ctx, t, at.user.ID, at.heart.ID))
		require.Empty(t, at.mustListDaily(ctx, t, at.user.ID, at.heart.ID))
		require.Empty(t, at.mustListDirty(ctx, t, at.user.ID))
		require.Len(t, at.mustListHourly(ctx, t, at.other.ID, at.heart.ID), 2)
		require.Len(t, at.mustListDaily(ctx, t, at.other.ID, at.heart.ID), 1)
		require.Len(t, at.mustListDirty(ctx, t, at.other.ID), 2)

		_, err := at.store.Aggregates().DeleteByUser(ctx, at.user.ID, 0)
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}
