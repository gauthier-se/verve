package aggregate

import (
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// style is how the samples of a type aggregate (FR-7 of Storage and aggregates).
type style int

const (
	// cumulative samples, such as steps, are prorated by overlap.
	cumulative style = iota
	// discrete samples, such as heart rate, count in the bucket of their start.
	discrete
	// category samples count with the seconds of their overlap, whatever their value.
	category
)

// styleOf returns the aggregation style of metricType.
func styleOf(metricType store.MetricType) style {
	switch {
	case metricType.Kind == catalog.KindCategory:
		return category
	case metricType.Aggregation == catalog.AggregationCumulative:
		return cumulative
	default:
		return discrete
	}
}

// inBucket reports whether sample counts in the bucket [from, to) of a type of style s: when it
// starts in it for a discrete type, and when it overlaps it otherwise.
func (s style) inBucket(sample store.Sample, from, to time.Time) bool {
	if s == discrete {
		return !sample.Start.Before(from) && sample.Start.Before(to)
	}

	_, ok := overlap(sample.Start, sample.End, from, to)

	return ok
}

// hourRow returns the row of the local hour [from, to) of metricType from samples, the samples of
// the type that may count in it, sorted by start then id, and reports whether the hour has one.
//
// It selects, by priorities, the highest priority source that is not ignored and has samples in the
// hour, and aggregates the samples of that source only (FR-6 of Deduplication and source priority).
// A discrete type in combine mode aggregates the samples of every source that is not ignored
// instead (FR-7). A source that priorities do not order never contributes. The hour has no row when
// no contributing source has samples in it.
func hourRow(metricType store.MetricType, priorities Priorities, from, to time.Time, samples []store.Sample) (store.AggregateRow, bool) {
	s := styleOf(metricType)
	combine := priorities.Combine && s == discrete

	rank := make(map[int64]int, len(priorities.Order))
	for i, source := range priorities.Order {
		rank[source.ID] = i
	}

	var in []store.Sample

	selected := -1

	for _, sample := range samples {
		r, ok := rank[sample.SourceID]
		if !ok || !s.inBucket(sample, from, to) {
			continue
		}

		in = append(in, sample)

		if selected < 0 || r < selected {
			selected = r
		}
	}

	if len(in) == 0 {
		return store.AggregateRow{}, false
	}

	row := store.AggregateRow{Start: from, Selection: store.SelectedCombined}
	if !combine {
		row.Selection, row.SourceID = store.SelectedSource, priorities.Order[selected].ID
	}

	var last store.Sample

	for _, sample := range in {
		if !combine && sample.SourceID != row.SourceID {
			continue
		}

		row.Count++

		switch s {
		case cumulative:
			share, _ := prorate(sample.Value, sample.Start, sample.End, from, to)
			row.Sum += share
		case category:
			shared, _ := overlap(sample.Start, sample.End, from, to)
			row.Sum += shared.Seconds()
		case discrete:
			row.Sum += sample.Value
			row.Min = lowest(row.Min, valueOr(sample.Min, sample.Value))
			row.Max = highest(row.Max, valueOr(sample.Max, sample.Value))

			if row.Last == nil || later(sample, last) {
				last = sample
				row.Last, row.LastAt = &last.Value, last.Start
			}
		}
	}

	return row, true
}

// later reports whether a is later than b by start, then end, then id.
func later(a, b store.Sample) bool {
	switch {
	case !a.Start.Equal(b.Start):
		return a.Start.After(b.Start)
	case !a.End.Equal(b.End):
		return a.End.After(b.End)
	default:
		return a.ID > b.ID
	}
}

// valueOr returns *v, or value when v is nil.
func valueOr(v *float64, value float64) float64 {
	if v == nil {
		return value
	}

	return *v
}

// lowest returns the lower of *current and v, current being nil before the first value.
func lowest(current *float64, v float64) *float64 {
	if current != nil && *current <= v {
		return current
	}

	return &v
}

// highest returns the higher of *current and v, current being nil before the first value.
func highest(current *float64, v float64) *float64 {
	if current != nil && *current >= v {
		return current
	}

	return &v
}

// dayRow returns the row of the local day that starts at start from hours, its hourly rows sorted
// by start, at least one (FR-7 of Storage and aggregates): the sums of their sum and count, the
// smallest min, the largest max, the last of the last hour, and the number of hours. The day holds
// the selection of its hours when they all have the same, and mixed otherwise.
func dayRow(start time.Time, hours []store.AggregateRow) store.AggregateRow {
	row := store.AggregateRow{Start: start, Selection: hours[0].Selection, SourceID: hours[0].SourceID}

	for _, hour := range hours {
		row.Sum += hour.Sum
		row.Count += hour.Count
		row.Hours++

		if hour.Min != nil {
			row.Min = lowest(row.Min, *hour.Min)
		}

		if hour.Max != nil {
			row.Max = highest(row.Max, *hour.Max)
		}

		if hour.Last != nil {
			row.Last, row.LastAt = hour.Last, hour.LastAt
		}

		if hour.Selection != row.Selection || hour.SourceID != row.SourceID {
			row.Selection, row.SourceID = store.SelectedMixed, 0
		}
	}

	return row
}
