package store

import (
	"context"
	"time"
)

// Selection tells which sources an aggregate row holds the samples of (FR-7 of Storage and
// aggregates).
type Selection string

// The selections of an aggregate row.
const (
	// SelectedSource is a row of the samples of one source, its SourceID.
	SelectedSource Selection = "source"
	// SelectedCombined is a row of every source that is not ignored, for a type in combine mode
	// (FR-7 of Deduplication and source priority).
	SelectedCombined Selection = "combined"
	// SelectedMixed is a daily row whose hours selected different sources.
	SelectedMixed Selection = "mixed"
)

// Known reports whether s is one of the selections of an aggregate row.
func (s Selection) Known() bool {
	switch s {
	case SelectedSource, SelectedCombined, SelectedMixed:
		return true
	default:
		return false
	}
}

// AggregateRow is an hourly or daily aggregate of a user and type, after source selection (FR-7 of
// Storage and aggregates). Times are in UTC with millisecond precision.
type AggregateRow struct {
	// Start is the start of the bucket: a local hour, or the local midnight that starts a day.
	Start time.Time
	Sum   float64
	Count int64
	// Min, Max and Last are nil for the rows of cumulative and category types.
	Min  *float64
	Max  *float64
	Last *float64
	// LastAt is the start of the sample of Last, zero when Last is nil.
	LastAt    time.Time
	Selection Selection
	// SourceID is the selected source, 0 unless Selection is SelectedSource.
	SourceID int64
	// Hours is the number of hours with data of a daily row, 0 for an hourly row.
	Hours int64
}

// DirtyRange is a time range of a type of a user whose aggregates wait for their recomputation
// (FR-9 of Storage and aggregates). Times are in UTC with millisecond precision.
type DirtyRange struct {
	// ID is set by the store, and ignored by RecordDirty.
	ID     int64
	TypeID int64
	// From is the start of the first sample of the range, and To the end of the last one.
	From time.Time
	To   time.Time
}

// AggregatesRepository stores the hourly and daily aggregates of each user, and the dirty ranges
// that wait for their recomputation. A row or a range of another user is never returned or
// changed.
type AggregatesRepository interface {
	// RecordDirty stores ranges as dirty ranges of userID. It returns ErrInvalid, and stores none of
	// them, when a range ends before it starts.
	RecordDirty(ctx context.Context, userID int64, ranges []DirtyRange) error
	// RecordAllDirty records, for each type that userID has samples of, a dirty range from the first
	// start of their samples to the last start widened by their longest duration, which holds every
	// sample of the type, for a full recomputation (FR-10 of Storage and aggregates).
	RecordAllDirty(ctx context.Context, userID int64) error
	// ListDirty returns the dirty ranges of userID, sorted by type id, then From, then id, or an
	// empty list.
	ListDirty(ctx context.Context, userID int64) ([]DirtyRange, error)
	// DeleteDirtyThrough removes the dirty ranges of userID whose ids are at most lastID: the ranges
	// that a ListDirty listed, when lastID is the largest id it returned, since the ranges recorded
	// later have larger ids. The ranges of other users are left alone.
	DeleteDirtyThrough(ctx context.Context, userID, lastID int64) error
	// AnyDirty reports whether a dirty range of userID and of the type of typeID overlaps [from, to),
	// the end of a range included since it can be the instant of a sample, so that a read of the
	// period knows that its aggregates are being recomputed.
	AnyDirty(ctx context.Context, userID, typeID int64, from, to time.Time) (bool, error)
	// ReplaceHourly replaces the hourly rows of userID and of the type of typeID that start in
	// [from, to) with rows, in one transaction. It returns ErrInvalid, and writes nothing, when a row
	// starts outside [from, to), two rows start at the same time, or a row is not one of an hour:
	// a count below 1, hours other than 0, an unknown selection, or a source id with any selection
	// but SelectedSource, or without it.
	ReplaceHourly(ctx context.Context, userID, typeID int64, from, to time.Time, rows []AggregateRow) error
	// ReplaceDaily is ReplaceHourly for the daily rows, whose hours are at least 1.
	ReplaceDaily(ctx context.Context, userID, typeID int64, from, to time.Time, rows []AggregateRow) error
	// ListHourly returns the hourly rows of userID and of the type of typeID that start in
	// [from, to), sorted by start, or an empty list.
	ListHourly(ctx context.Context, userID, typeID int64, from, to time.Time) ([]AggregateRow, error)
	// ListDaily is ListHourly for the daily rows.
	ListDaily(ctx context.Context, userID, typeID int64, from, to time.Time) ([]AggregateRow, error)
	// DeleteAll removes every hourly and daily row of userID, in one transaction, such as the rows
	// of the old timezone of a timezone change (FR-10 of Storage and aggregates). The dirty ranges
	// stay.
	DeleteAll(ctx context.Context, userID int64) error
	// DeleteByUser removes at most limit rows of the hourly and daily rows and the dirty ranges of
	// userID, and returns how many it removed, for an account deletion. It returns ErrInvalid when
	// limit is below 1.
	DeleteByUser(ctx context.Context, userID int64, limit int) (int64, error)
}
