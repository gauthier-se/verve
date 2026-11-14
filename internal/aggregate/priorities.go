// Package aggregate computes the hourly and daily aggregates of the samples, after source
// selection (Storage and aggregates, Deduplication and source priority).
package aggregate

import (
	"cmp"
	"slices"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// Priorities are the resolved priorities of the sources of a user for a metric type (FR-4 to FR-7
// of Deduplication and source priority).
type Priorities struct {
	// Order holds the sources that are not ignored, highest priority first.
	Order []store.Source
	// Ignored holds the ignored sources, which never contribute.
	Ignored []store.Source
	// Combine makes every source of Order contribute.
	Combine bool
}

// ResolvePriorities returns the priorities of sources, the sources of a user, for metricType, from
// stored, the priorities that the user set, or the zero SourcePriorities when they set none.
//
// The stored order and ignored sources are kept. Each source missing from them comes after the last
// stored source of the order whose kind comes at or before its own in the default list of the type,
// or first when there is none, and the missing sources at the same place follow the default list.
// Without stored priorities, the order is thus the default list. A stored id that sources do not
// hold, such as a source deleted between the two reads, is left out.
func ResolvePriorities(metricType store.MetricType, sources []store.Source, stored store.SourcePriorities) Priorities {
	ranks := defaultRanks(metricType.Group)

	byID := make(map[int64]store.Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}

	resolved := Priorities{Combine: stored.Combine}

	var ordered []store.Source

	for _, id := range stored.Order {
		if source, ok := byID[id]; ok {
			ordered = append(ordered, source)
			delete(byID, id)
		}
	}

	for _, id := range stored.Ignored {
		if source, ok := byID[id]; ok {
			resolved.Ignored = append(resolved.Ignored, source)
			delete(byID, id)
		}
	}

	missing := make([]store.Source, 0, len(byID))
	for _, source := range byID {
		missing = append(missing, source)
	}

	slices.SortFunc(missing, func(a, b store.Source) int {
		return cmp.Or(
			cmp.Compare(ranks[a.Kind], ranks[b.Kind]),
			a.FirstSeenAt.Compare(b.FirstSeenAt),
			cmp.Compare(a.ID, b.ID),
		)
	})

	// after[i] holds the missing sources that come after the first i sources of ordered.
	after := make([][]store.Source, len(ordered)+1)

	for _, source := range missing {
		place := 0

		for i, o := range ordered {
			if ranks[o.Kind] <= ranks[source.Kind] {
				place = i + 1
			}
		}

		after[place] = append(after[place], source)
	}

	for i, group := range after {
		resolved.Order = append(resolved.Order, group...)

		if i < len(ordered) {
			resolved.Order = append(resolved.Order, ordered[i])
		}
	}

	return resolved
}

// defaultRanks returns the position of each kind in the default list of the display group: the
// activity and energy list for activity, the body list for body, the nutrition list for nutrition,
// and the other list otherwise. Kinds of the same rank share a position. The kinds that a list does
// not name come after those it names, in the order of the other list.
func defaultRanks(group catalog.Group) map[store.SourceKind]int {
	// The apps are the sources of kind app or unknown, since a third-party app known only by its
	// name is unknown (FR-2).
	apps := []store.SourceKind{store.SourceApp, store.SourceUnknown}

	other := [][]store.SourceKind{
		{store.SourceAggregated},
		{store.SourceWatch},
		{store.SourceRing},
		{store.SourcePhone},
		{store.SourceScale, store.SourceApp, store.SourceManual, store.SourceUnknown},
	}

	var named [][]store.SourceKind

	switch group {
	case catalog.GroupActivity:
		named = [][]store.SourceKind{
			{store.SourceAggregated}, {store.SourceWatch}, {store.SourceRing}, {store.SourcePhone}, apps,
			{store.SourceManual},
		}
	case catalog.GroupBody:
		named = [][]store.SourceKind{{store.SourceScale}, apps, {store.SourceManual}}
	case catalog.GroupNutrition:
		named = [][]store.SourceKind{apps, {store.SourceManual}}
	default:
		named = other
	}

	ranks := make(map[store.SourceKind]int)

	for rank, kinds := range named {
		for _, kind := range kinds {
			ranks[kind] = rank
		}
	}

	for rank, kinds := range other {
		for _, kind := range kinds {
			if _, ok := ranks[kind]; !ok {
				ranks[kind] = len(named) + rank
			}
		}
	}

	return ranks
}
