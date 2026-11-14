package aggregate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// The errors of ReadRaw, whose messages are stable codes.
var (
	// ErrRangeTooLong is a range longer than the longest of a raw read.
	ErrRangeTooLong = errors.New("range_too_long")
	// ErrMaxPointsWithCursor is a downsampled read after a position: a downsampled read is never
	// paginated.
	ErrMaxPointsWithCursor = errors.New("max_points_with_cursor")
	// ErrMaxPointsUnsupported is a downsampled read of a category type, whose values make no line.
	ErrMaxPointsUnsupported = errors.New("max_points_unsupported")
)

// rawBatch is the number of samples that a downsampled read lists at once.
const rawBatch = 5000

// RawQuery is what a raw read reads (FR-12 of Storage and aggregates).
type RawQuery struct {
	// From and To bound the starts of the samples, [From, To).
	From, To time.Time
	// MaxDays is the longest range, in days of 24 hours (VERVE_RAW_MAX_DAYS).
	MaxDays int
	// After is the position of the last sample of the previous page, or zero for the first page.
	After store.Cursor
	// Limit is the maximum number of samples of a page, at least 1, ignored when MaxPoints is set.
	Limit int
	// MaxPoints, when above 0, downsamples the whole range to at most that many samples instead.
	MaxPoints int
}

// RawSample is a sample, and whether it counts in the aggregates (FR-9 of Deduplication and source
// priority).
type RawSample struct {
	store.Sample

	UsedInAggregates bool
}

// RawPage is the samples of a raw read.
type RawPage struct {
	// Samples are sorted by start then id.
	Samples []RawSample
	// More is true when samples remain after the last one, never for a downsampled read.
	More bool
}

// ReadRaw returns the samples of metricType of userID that q asks for, read through repos, with
// whether each counts in the aggregates of loc, the timezone of the user: when its source is the
// one that the hourly row of its local hour selected, or, for a type in combine mode, when its
// source is not ignored. A sample of an hour without a row counts in none.
//
// It returns ErrPeriodInvalid when q.To is not after q.From, ErrRangeTooLong when the range is longer
// than q.MaxDays days, ErrMaxPointsWithCursor when q downsamples after a position, and
// ErrMaxPointsUnsupported when it downsamples a category type.
func ReadRaw(ctx context.Context, repos store.Repositories, userID int64, metricType store.MetricType,
	loc *time.Location, q RawQuery,
) (RawPage, error) {
	switch {
	case !q.To.After(q.From):
		return RawPage{}, ErrPeriodInvalid
	case q.To.Sub(q.From) > time.Duration(q.MaxDays)*24*time.Hour:
		return RawPage{}, ErrRangeTooLong
	case q.MaxPoints > 0 && q.After != (store.Cursor{}):
		return RawPage{}, ErrMaxPointsWithCursor
	case q.MaxPoints > 0 && styleOf(metricType) == category:
		return RawPage{}, ErrMaxPointsUnsupported
	}

	var (
		page RawPage
		err  error
	)

	if q.MaxPoints > 0 {
		page, err = downsampledPage(ctx, repos, userID, metricType, q)
	} else {
		page, err = pageOf(ctx, repos, userID, metricType, q)
	}

	if err != nil {
		return RawPage{}, fmt.Errorf("read raw samples of %s: %w", metricType.Key, err)
	}

	if err := markUsed(ctx, repos, userID, metricType, loc, page.Samples); err != nil {
		return RawPage{}, fmt.Errorf("read raw samples of %s: %w", metricType.Key, err)
	}

	return page, nil
}

// pageOf returns the page of the samples that q asks for.
func pageOf(ctx context.Context, repos store.Repositories, userID int64, metricType store.MetricType, q RawQuery) (RawPage, error) {
	// One sample more than the limit tells whether more remain.
	samples, err := repos.Samples().List(ctx, userID, store.SamplesQuery{
		TypeID: metricType.ID, From: q.From, To: q.To, After: q.After, Limit: q.Limit + 1,
	})
	if err != nil {
		return RawPage{}, fmt.Errorf("list samples: %w", err)
	}

	more := len(samples) > q.Limit
	if more {
		samples = samples[:q.Limit]
	}

	return RawPage{Samples: rawSamples(samples), More: more}, nil
}

// downsampledPage returns every sample of the range of q, downsampled to at most q.MaxPoints.
func downsampledPage(ctx context.Context, repos store.Repositories, userID int64, metricType store.MetricType, q RawQuery) (RawPage, error) {
	var (
		all   []store.Sample
		after store.Cursor
	)

	for {
		batch, err := repos.Samples().List(ctx, userID, store.SamplesQuery{
			TypeID: metricType.ID, From: q.From, To: q.To, After: after, Limit: rawBatch,
		})
		if err != nil {
			return RawPage{}, fmt.Errorf("list samples: %w", err)
		}

		all = append(all, batch...)

		if len(batch) < rawBatch {
			break
		}

		after = batch[len(batch)-1].Cursor()
	}

	kept := Downsample(all, q.MaxPoints, func(s store.Sample) (float64, float64) {
		return float64(s.Start.UnixMilli()), s.Value
	})

	return RawPage{Samples: rawSamples(kept)}, nil
}

// rawSamples returns samples as raw samples that count in no aggregate.
func rawSamples(samples []store.Sample) []RawSample {
	raw := make([]RawSample, len(samples))
	for i, s := range samples {
		raw[i] = RawSample{Sample: s}
	}

	return raw
}

// markUsed sets UsedInAggregates on samples, sorted by start, of metricType of userID, from their
// priorities and, unless their type is in combine mode, the hourly rows of loc of their hours, read
// at once.
func markUsed(ctx context.Context, repos store.Repositories, userID int64, metricType store.MetricType,
	loc *time.Location, samples []RawSample,
) error {
	if len(samples) == 0 {
		return nil
	}

	priorities, err := repos.SourcePriorities().Get(ctx, userID, metricType.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("get source priorities: %w", err)
	}

	// As hourRow, which combines the sources of a discrete type only.
	if priorities.Combine && styleOf(metricType) == discrete {
		for i := range samples {
			samples[i].UsedInAggregates = !slices.Contains(priorities.Ignored, samples[i].SourceID)
		}

		return nil
	}

	from := hourStart(samples[0].Start, loc)
	to := hourEnd(hourStart(samples[len(samples)-1].Start, loc), loc)

	rows, err := repos.Aggregates().ListHourly(ctx, userID, metricType.ID, from, to)
	if err != nil {
		return fmt.Errorf("list hourly rows: %w", err)
	}

	// Keyed by Unix milliseconds, which compare whatever the location of a time.
	selected := make(map[int64]int64, len(rows))

	for _, row := range rows {
		if row.Selection == store.SelectedSource {
			selected[row.Start.UnixMilli()] = row.SourceID
		}
	}

	for i := range samples {
		source, ok := selected[hourStart(samples[i].Start, loc).UnixMilli()]
		samples[i].UsedInAggregates = ok && source == samples[i].SourceID
	}

	return nil
}
