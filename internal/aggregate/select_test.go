package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// selectHour is the hour of the selection tests, in UTC.
var selectHour = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// selectTypes are a type of each aggregation style.
var (
	cumulativeType = store.MetricType{ID: 1, Key: "step_count", Kind: catalog.KindQuantity, Aggregation: catalog.AggregationCumulative}
	discreteType   = store.MetricType{ID: 2, Key: "heart_rate", Kind: catalog.KindQuantity, Aggregation: catalog.AggregationDiscrete}
	categoryType   = store.MetricType{ID: 3, Key: "mindful_session", Kind: catalog.KindCategory, Aggregation: catalog.AggregationDiscrete}
)

// The sources of the selection tests: the watch comes before the phone, and the app is ignored.
var (
	selectWatch = store.Source{ID: 10, Kind: store.SourceWatch}
	selectPhone = store.Source{ID: 11, Kind: store.SourcePhone}
	selectApp   = store.Source{ID: 12, Kind: store.SourceApp}
)

// selectPriorities orders the watch before the phone and ignores the app.
func selectPriorities(combine bool) Priorities {
	return Priorities{Order: []store.Source{selectWatch, selectPhone}, Ignored: []store.Source{selectApp}, Combine: combine}
}

// at returns the time minutes after selectHour.
func at(minutes float64) time.Time {
	return selectHour.Add(time.Duration(minutes * float64(time.Minute)))
}

// sampleOf returns a sample of id of source, from start to end minutes after selectHour, of value.
func sampleOf(id int64, source store.Source, start, end, value float64) store.Sample {
	return store.Sample{ID: id, SourceID: source.ID, Start: at(start), End: at(end), Value: value}
}

// pointOf returns a pre-aggregated point of sampleOf with min and max.
func pointOf(id int64, source store.Source, start, end, value, low, high float64) store.Sample {
	sample := sampleOf(id, source, start, end, value)
	sample.Min, sample.Max = &low, &high

	return sample
}

// ptr returns a pointer to v.
func ptr(v float64) *float64 {
	return &v
}

// requireHour checks that hourRow over samples of the hour of selectHour gives want, and returns it.
func requireHour(t *testing.T, metricType store.MetricType, priorities Priorities, samples []store.Sample, want store.AggregateRow) {
	t.Helper()

	got, ok := hourRow(metricType, priorities, selectHour, selectHour.Add(time.Hour), samples)
	require.True(t, ok)
	require.InDelta(t, want.Sum, got.Sum, 1e-9)

	got.Sum = want.Sum
	require.Equal(t, want, got)
}

// Given cumulative samples of one source, the first starting in the hour before and the last
// ending in the hour after, then the hour holds the prorated sum of their overlaps and the number of
// samples that overlap it, without min, max or last. A sample that only touches the hour does not
// count.
func TestHourRow_Cumulative_HoldsTheProratedSumAndTheOverlappingSamples(t *testing.T) {
	t.Parallel()

	samples := []store.Sample{
		sampleOf(1, selectWatch, -60, 0, 500),
		sampleOf(2, selectWatch, -30, 30, 600),
		sampleOf(3, selectWatch, 30, 45, 150),
		sampleOf(4, selectWatch, 50, 50, 8),
		sampleOf(5, selectWatch, 45, 105, 240),
	}

	requireHour(t, cumulativeType, selectPriorities(false), samples, store.AggregateRow{
		Start:     selectHour,
		Sum:       300 + 150 + 8 + 60,
		Count:     4,
		Selection: store.SelectedSource,
		SourceID:  selectWatch.ID,
	})
}

// Given discrete samples, then the hour holds the sum, count, min, max and last value with its
// time of the samples that start in it, a pre-aggregated point counting as one sample with its own
// min and max. The last value is that of the latest start, then the latest end, then the highest
// id. A sample that starts in the hour before does not count.
func TestHourRow_Discrete_HoldsTheStatisticsOfTheSamplesThatStartInIt(t *testing.T) {
	t.Parallel()

	samples := []store.Sample{
		sampleOf(1, selectWatch, -5, 5, 200),
		sampleOf(2, selectWatch, 0, 0, 60),
		pointOf(3, selectWatch, 10, 20, 90, 50, 120),
		sampleOf(7, selectWatch, 40, 41, 75),
		sampleOf(6, selectWatch, 40, 42, 70),
		sampleOf(5, selectWatch, 40, 42, 80),
	}

	requireHour(t, discreteType, selectPriorities(false), samples, store.AggregateRow{
		Start:     selectHour,
		Sum:       60 + 90 + 75 + 70 + 80,
		Count:     5,
		Min:       ptr(50),
		Max:       ptr(120),
		Last:      ptr(70),
		LastAt:    at(40),
		Selection: store.SelectedSource,
		SourceID:  selectWatch.ID,
	})
}

// Given category samples, then the hour holds the number of samples that overlap it and the
// seconds of their overlap, whatever their values, without min, max or last.
func TestHourRow_Category_HoldsTheOverlappingSamplesAndTheirSeconds(t *testing.T) {
	t.Parallel()

	asleep := sampleOf(1, selectWatch, -30, 15, 1)
	asleep.Category = "asleep_core"
	awake := sampleOf(2, selectWatch, 50, 70, 1)
	awake.Category = "awake"

	requireHour(t, categoryType, selectPriorities(false), []store.Sample{asleep, awake}, store.AggregateRow{
		Start:     selectHour,
		Sum:       15*60 + 10*60,
		Count:     2,
		Selection: store.SelectedSource,
		SourceID:  selectWatch.ID,
	})
}

// Given two sources with data in the same hour, then only the one of higher priority contributes,
// and the row holds it, even when the other one has data first or more of it.
func TestHourRow_SelectsTheSourceOfHighestPriority(t *testing.T) {
	t.Parallel()

	samples := []store.Sample{
		sampleOf(1, selectPhone, 0, 30, 900),
		sampleOf(2, selectPhone, 30, 60, 900),
		sampleOf(3, selectWatch, 40, 50, 100),
		sampleOf(4, selectApp, 0, 60, 5000),
	}

	requireHour(t, cumulativeType, selectPriorities(false), samples, store.AggregateRow{
		Start:     selectHour,
		Sum:       100,
		Count:     1,
		Selection: store.SelectedSource,
		SourceID:  selectWatch.ID,
	})

	requireHour(t, cumulativeType, selectPriorities(false), samples[:2], store.AggregateRow{
		Start:     selectHour,
		Sum:       1800,
		Count:     2,
		Selection: store.SelectedSource,
		SourceID:  selectPhone.ID,
	})
}

// Given an hour where only an ignored source has data, then it has no row, whatever the kind of
// its type and the combine mode. Given an hour without samples, then it has no row.
func TestHourRow_IgnoredSourcesNeverContribute(t *testing.T) {
	t.Parallel()

	samples := []store.Sample{sampleOf(1, selectApp, 10, 20, 70), sampleOf(2, selectWatch, -20, -10, 60)}

	for _, metricType := range []store.MetricType{cumulativeType, discreteType, categoryType} {
		for _, combine := range []bool{false, true} {
			_, ok := hourRow(metricType, selectPriorities(combine), selectHour, selectHour.Add(time.Hour), samples)
			require.False(t, ok, "%s combine %t", metricType.Key, combine)
		}
	}

	_, ok := hourRow(discreteType, selectPriorities(false), selectHour, selectHour.Add(time.Hour), nil)
	require.False(t, ok)
}

// Given a discrete type in combine mode, then every source that is not ignored contributes, and the
// row holds combined.
func TestHourRow_CombineModeCombinesTheSourcesNotIgnored(t *testing.T) {
	t.Parallel()

	samples := []store.Sample{
		sampleOf(1, selectPhone, 5, 5, 82.5),
		sampleOf(2, selectWatch, 30, 30, 81.5),
		sampleOf(3, selectApp, 50, 50, 99),
	}

	requireHour(t, discreteType, selectPriorities(true), samples, store.AggregateRow{
		Start:     selectHour,
		Sum:       164,
		Count:     2,
		Min:       ptr(81.5),
		Max:       ptr(82.5),
		Last:      ptr(81.5),
		LastAt:    at(30),
		Selection: store.SelectedCombined,
	})
}

// Given hours that selected the same source, then their day holds that source, the sums of their
// sum and count, the smallest min, the largest max, the last of its last hour with data and the
// number of hours with data. Given hours that selected different sources, then the day holds mixed,
// and given combined hours, combined.
func TestDayRow_CombinesItsHours(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 11, 13, 23, 0, 0, 0, time.UTC)

	hour := func(offset int, sum float64, count int64, low, high, last float64, source int64) store.AggregateRow {
		start := day.Add(time.Duration(offset) * time.Hour)

		return store.AggregateRow{
			Start: start, Sum: sum, Count: count, Min: ptr(low), Max: ptr(high), Last: ptr(last),
			LastAt: start.Add(10 * time.Minute), Selection: store.SelectedSource, SourceID: source,
		}
	}

	hours := []store.AggregateRow{
		hour(1, 300, 4, 60, 90, 70, selectWatch.ID),
		hour(5, 100, 2, 45, 55, 50, selectWatch.ID),
		hour(20, 80, 1, 80, 80, 80, selectWatch.ID),
	}

	require.Equal(t, store.AggregateRow{
		Start: day, Sum: 480, Count: 7, Min: ptr(45), Max: ptr(90), Last: ptr(80),
		LastAt: day.Add(20*time.Hour + 10*time.Minute), Selection: store.SelectedSource, SourceID: selectWatch.ID, Hours: 3,
	}, dayRow(day, hours))

	hours[1].SourceID = selectPhone.ID

	mixed := dayRow(day, hours)
	require.Equal(t, store.SelectedMixed, mixed.Selection)
	require.Zero(t, mixed.SourceID)

	for i := range hours {
		hours[i].Selection, hours[i].SourceID = store.SelectedCombined, 0
	}

	combined := dayRow(day, hours)
	require.Equal(t, store.SelectedCombined, combined.Selection)
	require.Zero(t, combined.SourceID)
}

// Given cumulative hours, which have no min, max or last, then their day has none either.
func TestDayRow_CumulativeHoursHaveNoMinMaxOrLast(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 11, 13, 23, 0, 0, 0, time.UTC)
	hours := []store.AggregateRow{
		{Start: day, Sum: 120, Count: 2, Selection: store.SelectedSource, SourceID: selectWatch.ID},
		{Start: day.Add(time.Hour), Sum: 30, Count: 1, Selection: store.SelectedSource, SourceID: selectWatch.ID},
	}

	require.Equal(t, store.AggregateRow{
		Start: day, Sum: 150, Count: 3, Selection: store.SelectedSource, SourceID: selectWatch.ID, Hours: 2,
	}, dayRow(day, hours))
}
