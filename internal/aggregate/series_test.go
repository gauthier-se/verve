package aggregate_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite"
)

// seriesEnv is a migrated store with the shipped catalog, a user and a source, whose aggregate rows
// the tests write directly.
type seriesEnv struct {
	store  store.Store
	user   store.User
	source store.Source
}

func newSeriesEnv(t *testing.T) seriesEnv {
	t.Helper()

	ctx := t.Context()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "verve.db"))
	require.NoError(t, err)

	_, err = sqlite.Migrate(ctx, db, sqlite.MigrateOptions{})
	require.NoError(t, err)

	s := sqlite.New(db, clock.NewFake(jobTime))
	t.Cleanup(func() { require.NoError(t, s.Close(context.Background())) })

	cat, err := catalog.Shipped()
	require.NoError(t, err)
	require.NoError(t, s.MetricTypes().Sync(ctx, cat))

	user, err := s.Users().Create(ctx, store.NewUser{Username: "alice", Role: store.RoleMember})
	require.NoError(t, err)

	source, err := s.Sources().Resolve(ctx, user.ID, watch, store.SourceWatch, jobTime)
	require.NoError(t, err)

	return seriesEnv{store: s, user: user, source: source}
}

// mustType returns the metric type of key.
func (e seriesEnv) mustType(t *testing.T, key string) store.MetricType {
	t.Helper()

	metricType, err := e.store.MetricTypes().GetByKey(t.Context(), key)
	require.NoError(t, err)

	return metricType
}

// dayRow is a daily row of the local day that date names, YYYY-MM-DD.
type dayRow struct {
	date            string
	sum             float64
	count           int64
	low, high, last *float64
}

// mustWriteDays writes rows as the daily rows of the user and metricType, in loc.
func (e seriesEnv) mustWriteDays(t *testing.T, metricType store.MetricType, loc *time.Location, rows ...dayRow) {
	t.Helper()

	stored := make([]store.AggregateRow, 0, len(rows))
	for _, r := range rows {
		start := localMidnight(t, r.date, loc)
		row := store.AggregateRow{
			Start: start, Sum: r.sum, Count: r.count, Min: r.low, Max: r.high, Last: r.last,
			Selection: store.SelectedSource, SourceID: e.source.ID, Hours: 1,
		}

		if r.last != nil {
			row.LastAt = start.Add(time.Hour)
		}

		stored = append(stored, row)
	}

	err := e.store.Aggregates().ReplaceDaily(t.Context(), e.user.ID, metricType.ID,
		time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), stored)
	require.NoError(t, err)
}

// mustWriteHours writes, as hourly rows of a cumulative type of the user and metricType, the sum of
// sums at each start.
func (e seriesEnv) mustWriteHours(t *testing.T, metricType store.MetricType, sums map[time.Time]float64) {
	t.Helper()

	stored := make([]store.AggregateRow, 0, len(sums))
	for start, sum := range sums {
		stored = append(stored, store.AggregateRow{
			Start: start, Sum: sum, Count: 1, Selection: store.SelectedSource, SourceID: e.source.ID,
		})
	}

	err := e.store.Aggregates().ReplaceHourly(t.Context(), e.user.ID, metricType.ID,
		time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), stored)
	require.NoError(t, err)
}

// read reads the series of metricType of the user in loc, from and to being local dates.
func (e seriesEnv) read(t *testing.T, metricType store.MetricType, loc *time.Location, from, to string,
	bucket aggregate.Bucket, statistic aggregate.Statistic,
) (aggregate.Series, error) {
	t.Helper()

	return aggregate.ReadSeries(t.Context(), e.store, e.user.ID, metricType, loc, aggregate.SeriesQuery{
		From: date(t, from), To: date(t, to), Bucket: bucket, Statistic: statistic,
	})
}

// mustRead is read, requiring no error.
func (e seriesEnv) mustRead(t *testing.T, metricType store.MetricType, loc *time.Location, from, to string,
	bucket aggregate.Bucket, statistic aggregate.Statistic,
) aggregate.Series {
	t.Helper()

	series, err := e.read(t, metricType, loc, from, to, bucket, statistic)
	require.NoError(t, err)

	return series
}

// date returns the date value, YYYY-MM-DD, at midnight UTC.
func date(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.DateOnly, value)
	require.NoError(t, err)

	return parsed
}

// localMidnight returns the local midnight of loc that starts the day of value, as a UTC instant.
func localMidnight(t *testing.T, value string, loc *time.Location) time.Time {
	t.Helper()

	d := date(t, value)

	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc).UTC()
}

func ptr(v float64) *float64 {
	return &v
}

// mustLoadZone returns the location of the IANA timezone name.
func mustLoadZone(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	require.NoError(t, err)

	return loc
}

// Given daily rows around a period, when the series is read by day, then it holds the points of the
// local days of the period with data, to included, each starting at its local midnight.
func TestReadSeries_DayBucketReturnsTheDaysOfThePeriodToIncluded(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	paris := mustLoadZone(t, "Europe/Paris")
	steps := e.mustType(t, "step_count")

	e.mustWriteDays(t, steps, paris,
		dayRow{date: "2026-11-08", sum: 100, count: 1},
		dayRow{date: "2026-11-09", sum: 200, count: 2},
		dayRow{date: "2026-11-11", sum: 400, count: 4},
		dayRow{date: "2026-11-12", sum: 800, count: 8},
	)

	series := e.mustRead(t, steps, paris, "2026-11-09", "2026-11-11", aggregate.BucketDay, aggregate.StatisticSum)

	require.Equal(t, aggregate.BucketDay, series.Bucket)
	require.Equal(t, []aggregate.Point{
		{Start: utc(t, "2026-11-08T23:00:00Z"), Value: 200},
		{Start: utc(t, "2026-11-10T23:00:00Z"), Value: 400},
	}, series.Points)
	require.False(t, series.Updating)
}

// Given hourly rows, when the series is read by hour, then it holds the points of the local hours
// of the period with data.
func TestReadSeries_HourBucketReturnsTheHoursOfThePeriod(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	paris := mustLoadZone(t, "Europe/Paris")
	steps := e.mustType(t, "step_count")

	e.mustWriteHours(t, steps, map[time.Time]float64{
		utc(t, "2026-11-09T22:00:00Z"): 10,
		utc(t, "2026-11-09T23:00:00Z"): 20,
		utc(t, "2026-11-10T22:00:00Z"): 40,
		utc(t, "2026-11-10T23:00:00Z"): 80,
	})

	series := e.mustRead(t, steps, paris, "2026-11-10", "2026-11-10", aggregate.BucketHour, aggregate.StatisticSum)

	require.Equal(t, aggregate.BucketHour, series.Bucket)
	require.Equal(t, []aggregate.Point{
		{Start: utc(t, "2026-11-09T23:00:00Z"), Value: 20},
		{Start: utc(t, "2026-11-10T22:00:00Z"), Value: 40},
	}, series.Points)
}

// Given the daily sums of a cumulative type, when the series is read by week, then weeks start on
// Monday, and sum, mean, min and max are over the daily sums of each week.
func TestReadSeries_WeeksStartOnMondayAndCumulativeStatisticsUseTheDailySums(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	paris := mustLoadZone(t, "Europe/Paris")
	steps := e.mustType(t, "step_count")

	// 2026-11-08 is a Sunday, 2026-11-09 and 2026-11-16 are Mondays.
	e.mustWriteDays(t, steps, paris,
		dayRow{date: "2026-11-08", sum: 100, count: 1},
		dayRow{date: "2026-11-09", sum: 200, count: 5},
		dayRow{date: "2026-11-11", sum: 400, count: 7},
		dayRow{date: "2026-11-16", sum: 50, count: 1},
	)

	starts := []time.Time{
		utc(t, "2026-11-01T23:00:00Z"), utc(t, "2026-11-08T23:00:00Z"), utc(t, "2026-11-15T23:00:00Z"),
	}

	for _, tc := range []struct {
		statistic aggregate.Statistic
		values    []float64
	}{
		{aggregate.StatisticSum, []float64{100, 600, 50}},
		{aggregate.StatisticMean, []float64{100, 300, 50}},
		{aggregate.StatisticMin, []float64{100, 200, 50}},
		{aggregate.StatisticMax, []float64{100, 400, 50}},
	} {
		t.Run(string(tc.statistic), func(t *testing.T) {
			t.Parallel()

			series := e.mustRead(t, steps, paris, "2026-11-08", "2026-11-16", aggregate.BucketWeek, tc.statistic)

			require.Equal(t, aggregate.BucketWeek, series.Bucket)
			require.Equal(t, []aggregate.Point{
				{Start: starts[0], Value: tc.values[0]},
				{Start: starts[1], Value: tc.values[1]},
				{Start: starts[2], Value: tc.values[2]},
			}, series.Points)
		})
	}
}

// Given the daily rows of a discrete type, when the series is read by month or year, then the mean
// is the sum over the count of its days, min and max the extremes of its days, and last the last
// value of its last day with data.
func TestReadSeries_MonthsAndYearsCombineTheDailyRowsOfADiscreteType(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	utcZone := time.UTC
	heart := e.mustType(t, "heart_rate")

	e.mustWriteDays(t, heart, utcZone,
		dayRow{date: "2025-12-31", sum: 300, count: 5, low: ptr(50), high: ptr(70), last: ptr(55)},
		dayRow{date: "2026-01-02", sum: 120, count: 2, low: ptr(58), high: ptr(62), last: ptr(61)},
		dayRow{date: "2026-01-20", sum: 280, count: 2, low: ptr(90), high: ptr(190), last: ptr(150)},
		dayRow{date: "2026-02-01", sum: 80, count: 1, low: ptr(80), high: ptr(80), last: ptr(80)},
	)

	for _, tc := range []struct {
		bucket    aggregate.Bucket
		statistic aggregate.Statistic
		want      []aggregate.Point
	}{
		{aggregate.BucketMonth, aggregate.StatisticMean, []aggregate.Point{
			{Start: utc(t, "2025-12-01T00:00:00Z"), Value: 60},
			{Start: utc(t, "2026-01-01T00:00:00Z"), Value: 100},
			{Start: utc(t, "2026-02-01T00:00:00Z"), Value: 80},
		}},
		{aggregate.BucketMonth, aggregate.StatisticMin, []aggregate.Point{
			{Start: utc(t, "2025-12-01T00:00:00Z"), Value: 50},
			{Start: utc(t, "2026-01-01T00:00:00Z"), Value: 58},
			{Start: utc(t, "2026-02-01T00:00:00Z"), Value: 80},
		}},
		{aggregate.BucketMonth, aggregate.StatisticMax, []aggregate.Point{
			{Start: utc(t, "2025-12-01T00:00:00Z"), Value: 70},
			{Start: utc(t, "2026-01-01T00:00:00Z"), Value: 190},
			{Start: utc(t, "2026-02-01T00:00:00Z"), Value: 80},
		}},
		{aggregate.BucketMonth, aggregate.StatisticLast, []aggregate.Point{
			{Start: utc(t, "2025-12-01T00:00:00Z"), Value: 55},
			{Start: utc(t, "2026-01-01T00:00:00Z"), Value: 150},
			{Start: utc(t, "2026-02-01T00:00:00Z"), Value: 80},
		}},
		{aggregate.BucketYear, aggregate.StatisticMean, []aggregate.Point{
			{Start: utc(t, "2025-01-01T00:00:00Z"), Value: 60},
			{Start: utc(t, "2026-01-01T00:00:00Z"), Value: 96},
		}},
		{aggregate.BucketYear, aggregate.StatisticLast, []aggregate.Point{
			{Start: utc(t, "2025-01-01T00:00:00Z"), Value: 55},
			{Start: utc(t, "2026-01-01T00:00:00Z"), Value: 80},
		}},
	} {
		t.Run(string(tc.bucket)+"/"+string(tc.statistic), func(t *testing.T) {
			t.Parallel()

			series := e.mustRead(t, heart, utcZone, "2025-12-15", "2026-02-28", tc.bucket, tc.statistic)

			require.Equal(t, tc.bucket, series.Bucket)
			require.Equal(t, tc.want, series.Points)
		})
	}
}

// Given the daily rows of a category type, when the series is read by month, then sum adds up their
// seconds and count their samples.
func TestReadSeries_CategoryTypesSumSecondsAndCountSamples(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	sleep := e.mustType(t, "sleep_analysis")

	e.mustWriteDays(t, sleep, time.UTC,
		dayRow{date: "2026-10-30", sum: 3600, count: 3},
		dayRow{date: "2026-11-01", sum: 7200, count: 4},
		dayRow{date: "2026-11-02", sum: 1800, count: 1},
	)

	sum := e.mustRead(t, sleep, time.UTC, "2026-10-01", "2026-11-30", aggregate.BucketMonth, aggregate.StatisticSum)
	require.Equal(t, []aggregate.Point{
		{Start: utc(t, "2026-10-01T00:00:00Z"), Value: 3600},
		{Start: utc(t, "2026-11-01T00:00:00Z"), Value: 9000},
	}, sum.Points)

	count := e.mustRead(t, sleep, time.UTC, "2026-10-01", "2026-11-30", aggregate.BucketMonth, aggregate.StatisticCount)
	require.Equal(t, []aggregate.Point{
		{Start: utc(t, "2026-10-01T00:00:00Z"), Value: 3},
		{Start: utc(t, "2026-11-01T00:00:00Z"), Value: 5},
	}, count.Points)
}

// Given a statistic that the kind of the type does not accept with the bucket, then
// ErrStatisticUnsupported. Given one it accepts, then no error.
func TestReadSeries_RefusesTheStatisticsThatTheKindDoesNotAccept(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)

	for _, tc := range []struct {
		key       string
		bucket    aggregate.Bucket
		statistic aggregate.Statistic
		accepted  bool
	}{
		{"step_count", aggregate.BucketHour, aggregate.StatisticSum, true},
		{"step_count", aggregate.BucketDay, aggregate.StatisticSum, true},
		{"step_count", aggregate.BucketYear, aggregate.StatisticSum, true},
		{"step_count", aggregate.BucketWeek, aggregate.StatisticMean, true},
		{"step_count", aggregate.BucketMonth, aggregate.StatisticMin, true},
		{"step_count", aggregate.BucketYear, aggregate.StatisticMax, true},
		{"step_count", aggregate.BucketHour, aggregate.StatisticMean, false},
		{"step_count", aggregate.BucketDay, aggregate.StatisticMax, false},
		{"step_count", aggregate.BucketWeek, aggregate.StatisticLast, false},
		{"step_count", aggregate.BucketMonth, aggregate.StatisticCount, false},
		{"heart_rate", aggregate.BucketHour, aggregate.StatisticMean, true},
		{"heart_rate", aggregate.BucketDay, aggregate.StatisticMin, true},
		{"heart_rate", aggregate.BucketWeek, aggregate.StatisticMax, true},
		{"heart_rate", aggregate.BucketMonth, aggregate.StatisticLast, true},
		{"heart_rate", aggregate.BucketDay, aggregate.StatisticSum, false},
		{"heart_rate", aggregate.BucketWeek, aggregate.StatisticCount, false},
		{"sleep_analysis", aggregate.BucketHour, aggregate.StatisticSum, true},
		{"sleep_analysis", aggregate.BucketMonth, aggregate.StatisticCount, true},
		{"sleep_analysis", aggregate.BucketDay, aggregate.StatisticMean, false},
		{"sleep_analysis", aggregate.BucketWeek, aggregate.StatisticLast, false},
	} {
		t.Run(tc.key+"/"+string(tc.bucket)+"/"+string(tc.statistic), func(t *testing.T) {
			t.Parallel()

			_, err := e.read(t, e.mustType(t, tc.key), time.UTC, "2026-11-01", "2026-11-03", tc.bucket, tc.statistic)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, aggregate.ErrStatisticUnsupported)
			}
		})
	}
}

// Given an explicit bucket that yields more than 5,000 points over the period, then
// ErrTooManyPoints. Given 5,000 or fewer, then no error.
func TestReadSeries_RefusesAnExplicitBucketOfMoreThan5000Points(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	steps := e.mustType(t, "step_count")

	for _, tc := range []struct {
		name     string
		from, to string
		bucket   aggregate.Bucket
		accepted bool
	}{
		// 208 days are 4,992 hours, 209 days 5,016.
		{"4992 hours", "2026-01-01", "2026-07-27", aggregate.BucketHour, true},
		{"5016 hours", "2026-01-01", "2026-07-28", aggregate.BucketHour, false},
		{"5000 days", "2000-01-01", "2013-09-08", aggregate.BucketDay, true},
		{"5001 days", "2000-01-01", "2013-09-09", aggregate.BucketDay, false},
		{"5000 months", "1600-01-01", "2016-08-31", aggregate.BucketMonth, true},
		{"5001 months", "1600-01-01", "2016-09-01", aggregate.BucketMonth, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := e.read(t, steps, time.UTC, tc.from, tc.to, tc.bucket, aggregate.StatisticSum)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, aggregate.ErrTooManyPoints)
			}
		})
	}
}

// Given auto, then the bucket is the smallest of hour, day, week, month and year that yields at
// most 400 points over the period.
func TestReadSeries_AutoPicksTheSmallestBucketOfAtMost400Points(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)
	steps := e.mustType(t, "step_count")

	for _, tc := range []struct {
		name     string
		from, to string
		want     aggregate.Bucket
	}{
		// 16 days are 384 hours, 17 days 408.
		{"384 hours", "2026-11-01", "2026-11-16", aggregate.BucketHour},
		{"408 hours", "2026-11-01", "2026-11-17", aggregate.BucketDay},
		{"400 days", "2026-01-01", "2027-02-04", aggregate.BucketDay},
		// 401 days from a Thursday overlap 58 weeks.
		{"401 days", "2026-01-01", "2027-02-05", aggregate.BucketWeek},
		// 400 weeks from Monday 2000-01-03, then one more day.
		{"400 weeks", "2000-01-03", "2007-09-02", aggregate.BucketWeek},
		{"401 weeks", "2000-01-03", "2007-09-03", aggregate.BucketMonth},
		{"400 months", "2000-01-01", "2033-04-30", aggregate.BucketMonth},
		{"401 months", "2000-01-01", "2033-05-01", aggregate.BucketYear},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			series := e.mustRead(t, steps, time.UTC, tc.from, tc.to, aggregate.BucketAuto, aggregate.StatisticSum)
			require.Equal(t, tc.want, series.Bucket)
		})
	}
}

// Given to before from, then ErrPeriodInvalid.
func TestReadSeries_RefusesAPeriodThatEndsBeforeItStarts(t *testing.T) {
	t.Parallel()

	e := newSeriesEnv(t)

	_, err := e.read(t, e.mustType(t, "step_count"), time.UTC, "2026-11-10", "2026-11-09", aggregate.BucketDay,
		aggregate.StatisticSum)
	require.ErrorIs(t, err, aggregate.ErrPeriodInvalid)
}
