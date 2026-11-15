package workouts_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/workouts"
)

// runStart is the start of the runs of the duplicate tests.
var runStart = time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)

// firstSeen is when the sources of the duplicate tests were first seen, unless a test says
// otherwise.
var firstSeen = time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)

// run returns a 30 minute run of id, starting shifted after runStart, from the source of sourceID
// of kind, received through Health Auto Export, without route.
func run(id, sourceID int64, kind store.SourceKind, shift time.Duration) workouts.Candidate {
	return workouts.Candidate{
		ID:              id,
		SourceID:        sourceID,
		SourceKind:      kind,
		SourceFirstSeen: firstSeen,
		Origin:          store.OriginHAE,
		Family:          catalog.FamilyRunning,
		Start:           runStart.Add(shift),
		End:             runStart.Add(shift + 30*time.Minute),
	}
}

// Given workouts of different sources of the same family, overlapping by more than 80 percent of
// the shorter one, when Duplicates runs, then the one whose source kind comes later in the default
// activity and energy list, then whose source was first seen later, then whose source has the
// higher id, duplicates the other. Given an overlap of 80 percent or less, or different families,
// then neither is a duplicate.
func TestDuplicates_DifferentSources_LowerRankDuplicatesTheOther(t *testing.T) {
	t.Parallel()

	laterSeen := func(c workouts.Candidate) workouts.Candidate {
		c.SourceFirstSeen = firstSeen.Add(24 * time.Hour)

		return c
	}

	cycling := func(c workouts.Candidate) workouts.Candidate {
		c.Family = catalog.FamilyCycling

		return c
	}

	hour := func(c workouts.Candidate) workouts.Candidate {
		c.Start, c.End = runStart.Add(-15*time.Minute), runStart.Add(45*time.Minute)

		return c
	}

	tests := []struct {
		name  string
		group []workouts.Candidate
		want  map[int64]int64
	}{
		{
			name:  "AnAppCopyOfAWatchRunDuplicatesIt",
			group: []workouts.Candidate{run(1, 20, store.SourceApp, 3*time.Minute), run(2, 10, store.SourceWatch, 0)},
			want:  map[int64]int64{1: 2, 2: 0},
		},
		{
			name:  "AnUnknownSourceRanksAsAnApp",
			group: []workouts.Candidate{run(1, 10, store.SourceManual, 0), run(2, 20, store.SourceUnknown, 0)},
			want:  map[int64]int64{1: 2, 2: 0},
		},
		{
			name:  "TheSourceFirstSeenLaterLoses",
			group: []workouts.Candidate{run(1, 10, store.SourceApp, 0), laterSeen(run(2, 5, store.SourceApp, 0))},
			want:  map[int64]int64{1: 0, 2: 1},
		},
		{
			name:  "TheSourceOfTheHigherIDLosesBetweenSourcesFirstSeenTogether",
			group: []workouts.Candidate{run(1, 20, store.SourceApp, 0), run(2, 10, store.SourceApp, 0)},
			want:  map[int64]int64{1: 2, 2: 0},
		},
		{
			name:  "AWorkoutThatHoldsMostOfAShorterOneDuplicatesIt",
			group: []workouts.Candidate{hour(run(1, 10, store.SourceWatch, 0)), run(2, 20, store.SourceApp, 0)},
			want:  map[int64]int64{1: 0, 2: 1},
		},
		{
			name:  "AnOverlapOf80PercentIsNoDuplicate",
			group: []workouts.Candidate{run(1, 10, store.SourceWatch, 0), run(2, 20, store.SourceApp, 6*time.Minute)},
			want:  map[int64]int64{1: 0, 2: 0},
		},
		{
			name:  "AnOverlapOfJustOver80PercentIsADuplicate",
			group: []workouts.Candidate{run(1, 10, store.SourceWatch, 0), run(2, 20, store.SourceApp, 6*time.Minute-time.Millisecond)},
			want:  map[int64]int64{1: 0, 2: 1},
		},
		{
			name:  "DifferentFamiliesAreNoDuplicates",
			group: []workouts.Candidate{run(1, 10, store.SourceWatch, 0), cycling(run(2, 20, store.SourceApp, 0))},
			want:  map[int64]int64{1: 0, 2: 0},
		},
		{
			name:  "WorkoutsOfNoDurationAreNoDuplicates",
			group: []workouts.Candidate{instant(run(1, 10, store.SourceWatch, 0)), instant(run(2, 20, store.SourceApp, 0))},
			want:  map[int64]int64{1: 0, 2: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, workouts.Duplicates(tt.group))
		})
	}
}

// instant returns c ending when it starts.
func instant(c workouts.Candidate) workouts.Candidate {
	c.End = c.Start

	return c
}

// Given overlapping workouts of the same source, received through different origins, when
// Duplicates runs, then the one with a route is kept, then the one imported first. Given the same
// origin, then neither is a duplicate.
func TestDuplicates_SameSource_RouteThenFirstImportedIsKept(t *testing.T) {
	t.Parallel()

	xml := func(c workouts.Candidate) workouts.Candidate {
		c.Origin = store.OriginXML

		return c
	}

	withRoute := func(c workouts.Candidate) workouts.Candidate {
		c.HasRoute = true

		return c
	}

	tests := []struct {
		name  string
		group []workouts.Candidate
		want  map[int64]int64
	}{
		{
			name:  "TheOneWithARouteIsKept",
			group: []workouts.Candidate{xml(run(1, 10, store.SourceWatch, 0)), withRoute(run(2, 10, store.SourceWatch, 0))},
			want:  map[int64]int64{1: 2, 2: 0},
		},
		{
			name:  "WithoutRouteTheOneImportedFirstIsKept",
			group: []workouts.Candidate{run(2, 10, store.SourceWatch, 0), xml(run(1, 10, store.SourceWatch, 0))},
			want:  map[int64]int64{1: 0, 2: 1},
		},
		{
			name:  "WithRoutesTheOneImportedFirstIsKept",
			group: []workouts.Candidate{withRoute(run(2, 10, store.SourceWatch, 0)), withRoute(xml(run(1, 10, store.SourceWatch, 0)))},
			want:  map[int64]int64{1: 0, 2: 1},
		},
		{
			name:  "TheSameOriginIsNoDuplicate",
			group: []workouts.Candidate{run(1, 10, store.SourceWatch, 0), run(2, 10, store.SourceWatch, time.Minute)},
			want:  map[int64]int64{1: 0, 2: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, workouts.Duplicates(tt.group))
		})
	}
}

// Given three or more workouts that overlap one another, when Duplicates runs, then one is kept and
// each other points to it, whatever the order of the group.
func TestDuplicates_ThreeOverlappingWorkouts_EachPointsToTheKeptOne(t *testing.T) {
	t.Parallel()

	watch := run(3, 10, store.SourceWatch, 2*time.Minute)
	phone := run(1, 20, store.SourcePhone, 0)
	app := run(2, 30, store.SourceApp, time.Minute)
	strava := run(4, 40, store.SourceUnknown, 3*time.Minute)

	want := map[int64]int64{1: 3, 2: 3, 3: 0, 4: 3}

	require.Equal(t, want, workouts.Duplicates([]workouts.Candidate{phone, app, watch, strava}))
	require.Equal(t, want, workouts.Duplicates([]workouts.Candidate{strava, watch, app, phone}))
}

// Given workouts that overlap a kept workout but not one another, and a duplicate that overlaps a
// workout which it does not duplicate, when Duplicates runs, then each points to the kept workout it
// duplicates, and a workout that only a duplicate overlaps stays kept.
func TestDuplicates_Chain_PointsOnlyToKeptWorkouts(t *testing.T) {
	t.Parallel()

	// The watch run starts at 0, the app copy at 5 minutes, overlapping it by 25 of 30 minutes,
	// and the phone run at 10 minutes, overlapping the copy by 25 minutes but the watch run by 20
	// only, which is not more than 80 percent.
	watch := run(1, 10, store.SourceWatch, 0)
	app := run(2, 20, store.SourceApp, 5*time.Minute)
	phone := run(3, 30, store.SourcePhone, 10*time.Minute)

	require.Equal(t, map[int64]int64{1: 0, 2: 1, 3: 0}, workouts.Duplicates([]workouts.Candidate{app, phone, watch}))
}
