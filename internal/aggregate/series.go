package aggregate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gauthier-se/verve/internal/store"
)

// Bucket is the bucket of the points of a series (FR-11 of Storage and aggregates).
type Bucket string

// The buckets of a series, from the smallest.
const (
	// BucketAuto picks the smallest bucket that yields at most autoPoints points.
	BucketAuto  Bucket = "auto"
	BucketHour  Bucket = "hour"
	BucketDay   Bucket = "day"
	BucketWeek  Bucket = "week"
	BucketMonth Bucket = "month"
	BucketYear  Bucket = "year"
)

// autoBuckets are the buckets that BucketAuto picks from, from the smallest.
var autoBuckets = []Bucket{BucketHour, BucketDay, BucketWeek, BucketMonth, BucketYear}

// Statistic is the value of each point of a series.
type Statistic string

// The statistics of a series. Which ones a type accepts depends on its kind (FR-11 of Storage and
// aggregates).
const (
	StatisticSum   Statistic = "sum"
	StatisticMean  Statistic = "mean"
	StatisticMin   Statistic = "min"
	StatisticMax   Statistic = "max"
	StatisticLast  Statistic = "last"
	StatisticCount Statistic = "count"
)

const (
	// autoPoints is the largest number of points of the bucket that BucketAuto picks.
	autoPoints = 400
	// maxPoints is the largest number of points of a series.
	maxPoints = 5000
)

// The errors of ReadSeries, whose messages are stable codes.
var (
	// ErrStatisticUnsupported is a statistic that the kind of the type does not accept with the
	// bucket.
	ErrStatisticUnsupported = errors.New("statistic_unsupported")
	// ErrTooManyPoints is a bucket that yields more than 5,000 points over the period.
	ErrTooManyPoints = errors.New("too_many_points")
	// ErrPeriodInvalid is a period that ends before it starts, or for a raw read, a range that does
	// not end after it starts.
	ErrPeriodInvalid = errors.New("period_invalid")
)

// SeriesQuery is what a series reads.
type SeriesQuery struct {
	// From and To are the first and last local days of the period, both included, read from their
	// year, month and day whatever their location.
	From, To  time.Time
	Bucket    Bucket
	Statistic Statistic
}

// Point is the value of a bucket with data.
type Point struct {
	// Start is the start of the bucket, a UTC instant: a local hour, or the local midnight that
	// starts a day, a week, a month or a year.
	Start time.Time
	Value float64
}

// Series is the series of a type over a period.
type Series struct {
	// Bucket is the bucket used, never BucketAuto.
	Bucket Bucket
	// Points are those of the buckets with data, oldest first, empty without data.
	Points []Point
	// Updating is true while a dirty range of the type overlaps the period.
	Updating bool
}

// ReadSeries returns the series of metricType of userID that q asks for, read through repos in loc,
// the timezone of the user (FR-11 of Storage and aggregates). Hours come from the hourly rows, and
// days, weeks starting on Monday, months and years from the daily rows. A bucket that starts before
// the period holds the rows of the period only.
//
// It returns ErrPeriodInvalid when q.To is before q.From, ErrTooManyPoints when the bucket yields
// more than 5,000 points over the period, and ErrStatisticUnsupported when the kind of metricType
// does not accept q.Statistic with the bucket.
func ReadSeries(ctx context.Context, repos store.Repositories, userID int64, metricType store.MetricType,
	loc *time.Location, q SeriesQuery,
) (Series, error) {
	if q.To.Before(q.From) {
		return Series{}, ErrPeriodInvalid
	}

	from := localDate(q.From, 0, loc)
	// The period ends where the day after To starts.
	to := localDate(q.To, 1, loc)

	bucket, err := resolveBucket(q.Bucket, from, to, loc)
	if err != nil {
		return Series{}, err
	}

	s := styleOf(metricType)
	if !s.accepts(bucket, q.Statistic) {
		return Series{}, ErrStatisticUnsupported
	}

	var rows []store.AggregateRow
	if bucket == BucketHour {
		rows, err = repos.Aggregates().ListHourly(ctx, userID, metricType.ID, from, to)
	} else {
		rows, err = repos.Aggregates().ListDaily(ctx, userID, metricType.ID, from, to)
	}

	if err != nil {
		return Series{}, fmt.Errorf("read series of %s: %w", metricType.Key, err)
	}

	updating, err := repos.Aggregates().AnyDirty(ctx, userID, metricType.ID, from, to)
	if err != nil {
		return Series{}, fmt.Errorf("read series of %s: %w", metricType.Key, err)
	}

	return Series{Bucket: bucket, Points: points(rows, bucket, s, q.Statistic, loc), Updating: updating}, nil
}

// localDate returns the local midnight of loc that starts the day of date, plus days, as a UTC
// instant.
func localDate(date time.Time, days int, loc *time.Location) time.Time {
	year, month, day := date.Date()

	return time.Date(year, month, day+days, 0, 0, 0, 0, loc).UTC()
}

// resolveBucket returns bucket, or for BucketAuto the smallest of autoBuckets that yields at most
// autoPoints points over [from, to), and year when none does. It returns ErrTooManyPoints when the
// bucket yields more than maxPoints points.
func resolveBucket(bucket Bucket, from, to time.Time, loc *time.Location) (Bucket, error) {
	if bucket == BucketAuto {
		bucket = BucketYear

		for _, b := range autoBuckets {
			if countBuckets(b, from, to, loc, autoPoints) <= autoPoints {
				bucket = b

				break
			}
		}
	}

	if countBuckets(bucket, from, to, loc, maxPoints) > maxPoints {
		return "", ErrTooManyPoints
	}

	return bucket, nil
}

// countBuckets returns the number of buckets of loc that overlap [from, to), whole local days, or
// limit + 1 when there are more than limit.
func countBuckets(bucket Bucket, from, to time.Time, loc *time.Location, limit int) int {
	if bucket == BucketHour {
		count := 0
		for start := hourStart(from, loc); start.Before(to) && count <= limit; start = hourEnd(start, loc) {
			count++
		}

		return count
	}

	first, last := localDay(from, loc), localDay(to, loc).AddDate(0, 0, -1)

	var count int

	switch bucket {
	case BucketDay:
		count = civilDays(first, last) + 1
	case BucketWeek:
		count = civilDays(first.AddDate(0, 0, -sinceMonday(first)), last.AddDate(0, 0, -sinceMonday(last)))/7 + 1
	case BucketMonth:
		count = (last.Year()-first.Year())*12 + int(last.Month()) - int(first.Month()) + 1
	default:
		count = last.Year() - first.Year() + 1
	}

	return min(count, limit+1)
}

// civilDays returns the number of days from the date of from to the date of to, whatever their
// clock times and offsets.
func civilDays(from, to time.Time) int {
	y1, m1, d1 := from.Date()
	y2, m2, d2 := to.Date()

	start := time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)
	end := time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)

	return int(end.Sub(start).Hours() / 24)
}

// sinceMonday returns the number of days since the Monday that starts the week of local.
func sinceMonday(local time.Time) int {
	return (int(local.Weekday()) + 6) % 7
}

// localDay returns the middle of the local day of loc that starts at start, whose date is the date
// of the day. Its start may not be: time.Date moves a local midnight that a clock change skips,
// such as America/Asuncion on 2017-10-01, to the evening before.
func localDay(start time.Time, loc *time.Location) time.Time {
	return start.Add(12 * time.Hour).In(loc)
}

// bucketStart returns the start of the bucket of loc that holds the local day starting at day, as
// a UTC instant.
func bucketStart(bucket Bucket, day time.Time, loc *time.Location) time.Time {
	local := localDay(day, loc)
	year, month, date := local.Date()

	switch bucket {
	case BucketWeek:
		return time.Date(year, month, date-sinceMonday(local), 0, 0, 0, 0, loc).UTC()
	case BucketMonth:
		return time.Date(year, month, 1, 0, 0, 0, 0, loc).UTC()
	case BucketYear:
		return time.Date(year, time.January, 1, 0, 0, 0, 0, loc).UTC()
	default:
		return day
	}
}

// accepts reports whether a type of style s accepts statistic with bucket (FR-11 of Storage and
// aggregates). A cumulative type accepts mean, min and max for the buckets of several days only,
// over the daily sums of the bucket.
func (s style) accepts(bucket Bucket, statistic Statistic) bool {
	switch s {
	case cumulative:
		switch statistic {
		case StatisticSum:
			return true
		case StatisticMean, StatisticMin, StatisticMax:
			return bucket == BucketWeek || bucket == BucketMonth || bucket == BucketYear
		default:
			return false
		}
	case discrete:
		return statistic == StatisticMean || statistic == StatisticMin || statistic == StatisticMax ||
			statistic == StatisticLast
	default:
		return statistic == StatisticSum || statistic == StatisticCount
	}
}

// points returns the points of rows, hourly or daily rows sorted by start, grouped by the buckets
// of loc that hold them, each the statistic that s accepts.
func points(rows []store.AggregateRow, bucket Bucket, s style, statistic Statistic, loc *time.Location) []Point {
	list := []Point{}

	for start := 0; start < len(rows); {
		bucketFrom := bucketStart(bucket, rows[start].Start, loc)

		end := start
		for end < len(rows) && bucketStart(bucket, rows[end].Start, loc).Equal(bucketFrom) {
			end++
		}

		if value, ok := combine(rows[start:end], s, statistic); ok {
			list = append(list, Point{Start: bucketFrom, Value: value})
		}

		start = end
	}

	return list
}

// combine returns the statistic of rows, the rows of one bucket sorted by start, and false when
// they lack it, such as a min of rows without one. The mean of a discrete type is the sum of the
// rows over their count, and the mean, min and max of a cumulative type are over the sums of the
// rows.
func combine(rows []store.AggregateRow, s style, statistic Statistic) (float64, bool) {
	var (
		sum, count float64
		low, high  = math.Inf(1), math.Inf(-1)
		last       *float64
	)

	for _, row := range rows {
		sum += row.Sum
		count += float64(row.Count)

		rowMin, rowMax := row.Min, row.Max
		if s == cumulative {
			rowMin, rowMax = &row.Sum, &row.Sum
		}

		if rowMin != nil {
			low = math.Min(low, *rowMin)
		}

		if rowMax != nil {
			high = math.Max(high, *rowMax)
		}

		if row.Last != nil {
			last = row.Last
		}
	}

	switch statistic {
	case StatisticSum:
		return sum, true
	case StatisticCount:
		return count, true
	case StatisticMean:
		if s == cumulative {
			return sum / float64(len(rows)), true
		}

		return sum / count, true
	case StatisticMin:
		return low, !math.IsInf(low, 0)
	case StatisticMax:
		return high, !math.IsInf(high, 0)
	default:
		if last == nil {
			return 0, false
		}

		return *last, true
	}
}
