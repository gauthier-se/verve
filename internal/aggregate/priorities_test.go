package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

var prioritiesTime = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// prioritySource returns the source of id of kind, first seen hours after prioritiesTime.
func prioritySource(id int64, kind store.SourceKind, hours int) store.Source {
	return store.Source{
		ID:          id,
		UserID:      1,
		Name:        string(kind),
		DisplayName: string(kind),
		Kind:        kind,
		FirstSeenAt: prioritiesTime.Add(time.Duration(hours) * time.Hour),
		LastSeenAt:  prioritiesTime.Add(time.Duration(hours) * time.Hour),
	}
}

// prioritySources holds a source of each kind, sorted by id as the store lists them, whose first
// seen times do not follow their ids. The apps 3 and 9 were first seen at the same time.
func prioritySources() []store.Source {
	return []store.Source{
		prioritySource(1, store.SourceManual, 1),
		prioritySource(2, store.SourceApp, 5),
		prioritySource(3, store.SourceUnknown, 2),
		prioritySource(4, store.SourcePhone, 0),
		prioritySource(5, store.SourceWatch, 3),
		prioritySource(6, store.SourceScale, 4),
		prioritySource(7, store.SourceRing, 6),
		prioritySource(8, store.SourceAggregated, 7),
		prioritySource(9, store.SourceApp, 2),
	}
}

// metricTypeOf returns a type of group, curated or not.
func metricTypeOf(group catalog.Group, curated bool) store.MetricType {
	return store.MetricType{
		ID:          1,
		Key:         "type_of_" + string(group),
		Identifier:  "HKQuantityTypeIdentifierOf" + string(group),
		Kind:        catalog.KindQuantity,
		Aggregation: catalog.AggregationDiscrete,
		Group:       group,
		Curated:     curated,
	}
}

// idsOf returns the ids of sources, in order, or nil when there are none.
func idsOf(sources []store.Source) []int64 {
	var ids []int64
	for _, source := range sources {
		ids = append(ids, source.ID)
	}

	return ids
}

// Given a type without stored priorities, when its priorities are resolved, then the sources follow
// the default list of the display group of the type, the kinds that the list does not name come
// after those it names in the order of the other list, and sources at the same position are ordered
// by first seen, then by id. No source is ignored and combine is off.
func TestResolvePriorities_WithoutStoredPrioritiesFollowsTheDefaultListOfTheGroup(t *testing.T) {
	t.Parallel()

	// The ids of prioritySources by kind: 1 manual, 2, 3 (unknown) and 9 apps, 4 phone, 5 watch,
	// 6 scale, 7 ring, 8 aggregated. Among the apps, 3 and 9 come before 2, and 3 before 9.
	activity := []int64{8, 5, 7, 4, 3, 9, 2, 1, 6}
	other := []int64{8, 5, 7, 4, 1, 3, 9, 6, 2}

	tests := []struct {
		name       string
		metricType store.MetricType
		want       []int64
	}{
		{"ActivityUsesTheActivityAndEnergyList", metricTypeOf(catalog.GroupActivity, true), activity},
		{"BodyUsesTheBodyList", metricTypeOf(catalog.GroupBody, true), []int64{6, 3, 9, 2, 1, 8, 5, 7, 4}},
		{"NutritionUsesTheNutritionList", metricTypeOf(catalog.GroupNutrition, true), []int64{3, 9, 2, 1, 8, 5, 7, 4, 6}},
		{"HeartUsesTheOtherList", metricTypeOf(catalog.GroupHeart, true), other},
		{"SleepUsesTheOtherList", metricTypeOf(catalog.GroupSleep, true), other},
		{"UncuratedTypeUsesTheOtherList", metricTypeOf(catalog.GroupOther, false), other},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolvePriorities(tt.metricType, prioritySources(), store.SourcePriorities{})

			require.Equal(t, tt.want, idsOf(got.Order))
			require.Empty(t, got.Ignored)
			require.False(t, got.Combine)
		})
	}
}

// Given stored priorities, when they are resolved, then the order is the stored order, the ignored
// sources stay apart in their stored order, the combine flag is the stored one, and each source
// missing from the stored order comes after the last stored source whose kind comes at or before
// its own in the default list of the type, or first when there is none. Missing sources at the same
// place follow the default list.
func TestResolvePriorities_WithStoredPrioritiesPlacesTheMissingSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		metricType  store.MetricType
		stored      store.SourcePriorities
		wantOrder   []int64
		wantIgnored []int64
	}{
		{
			// Aggregated, watch and ring come before manual and phone in the activity list, so
			// no stored source comes at or before them: they come first.
			name:       "MissingSourcesBeforeEveryStoredOneComeFirst",
			metricType: metricTypeOf(catalog.GroupActivity, true),
			stored:     store.SourcePriorities{Order: []int64{1, 4}},
			wantOrder:  []int64{8, 5, 7, 1, 4, 3, 9, 2, 6},
		},
		{
			// The scale comes last in the activity list, so every missing source but the aggregated
			// one comes after the watch, the last stored source at or before them.
			name:        "MissingSourcesComeAfterTheLastStoredOneAtOrBeforeTheirKind",
			metricType:  metricTypeOf(catalog.GroupActivity, true),
			stored:      store.SourcePriorities{Order: []int64{6, 5}, Ignored: []int64{2}},
			wantOrder:   []int64{8, 6, 5, 7, 4, 3, 9, 1},
			wantIgnored: []int64{2},
		},
		{
			// In the body list, the missing apps share the position of the stored app 2, and the
			// scale comes before it: they come after the scale, the last stored source at or
			// before them.
			name:        "MissingSourcesAtTheSamePositionComeAfterTheLastStoredOne",
			metricType:  metricTypeOf(catalog.GroupBody, true),
			stored:      store.SourcePriorities{Order: []int64{2, 6}, Ignored: []int64{4, 8}},
			wantOrder:   []int64{2, 6, 3, 9, 1, 5, 7},
			wantIgnored: []int64{4, 8},
		},
		{
			name:        "EveryStoredSourceKeepsItsPlace",
			metricType:  metricTypeOf(catalog.GroupOther, false),
			stored:      store.SourcePriorities{Order: []int64{2, 9, 3, 1, 6, 4}, Ignored: []int64{7, 5, 8}},
			wantOrder:   []int64{2, 9, 3, 1, 6, 4},
			wantIgnored: []int64{7, 5, 8},
		},
		{
			// A source removed between the reads of the sources and of the priorities is left out.
			name:        "StoredSourceMissingFromTheSourcesIsLeftOut",
			metricType:  metricTypeOf(catalog.GroupOther, false),
			stored:      store.SourcePriorities{Order: []int64{2, 42, 9, 3, 1, 6, 4}, Ignored: []int64{7, 5, 43, 8}},
			wantOrder:   []int64{2, 9, 3, 1, 6, 4},
			wantIgnored: []int64{7, 5, 8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolvePriorities(tt.metricType, prioritySources(), tt.stored)

			require.Equal(t, tt.wantOrder, idsOf(got.Order))
			require.Equal(t, tt.wantIgnored, idsOf(got.Ignored))
			require.False(t, got.Combine)
		})
	}
}

// Given stored priorities in combine mode, when they are resolved, then combine is on.
func TestResolvePriorities_KeepsTheStoredCombineFlag(t *testing.T) {
	t.Parallel()

	got := ResolvePriorities(metricTypeOf(catalog.GroupBody, true), prioritySources(),
		store.SourcePriorities{Combine: true})

	require.True(t, got.Combine)
	require.Equal(t, []int64{6, 3, 9, 2, 1, 8, 5, 7, 4}, idsOf(got.Order))
}
