// Package workouts finds the workouts that several sources recorded (FR-11 of Deduplication and
// source priority).
package workouts

import (
	"cmp"
	"slices"
	"time"

	"github.com/gauthier-se/verve/internal/aggregate"
	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/store"
)

// Candidate is a workout as duplicate detection compares it.
type Candidate struct {
	ID     int64
	Source store.Source
	Origin store.Origin
	// Family is the activity family of its activity type.
	Family   catalog.Family
	Start    time.Time
	End      time.Time
	HasRoute bool
}

// KeptWorkouts returns the id of the kept workout that each workout of group duplicates, by id, 0
// for a kept workout.
//
// The workouts are ranked by their source, as the default activity and energy list of FR-4 orders
// sources: by kind, then by first seen, then by source id, which leaves the workouts of one source
// together. Among them, the one with a route comes first, then the one imported first, of the lower
// id. In that order, each workout duplicates the first kept workout that it duplicates (see
// duplicate), and is kept when there is none. The result of a workout depends only on the workouts
// of group that it duplicates, so that a group closed under duplicate gives the result that all the
// workouts of the user would.
func KeptWorkouts(group []Candidate) map[int64]int64 {
	compareSources := aggregate.CompareSources(catalog.GroupActivity)

	ranked := slices.Clone(group)
	slices.SortFunc(ranked, func(a, b Candidate) int {
		return cmp.Or(
			compareSources(a.Source, b.Source),
			compareRoute(a.HasRoute, b.HasRoute),
			cmp.Compare(a.ID, b.ID),
		)
	})

	result := make(map[int64]int64, len(ranked))

	var kept []Candidate

	for _, c := range ranked {
		i := slices.IndexFunc(kept, func(k Candidate) bool { return duplicate(c, k) })
		if i < 0 {
			kept = append(kept, c)
			result[c.ID] = 0

			continue
		}

		result[c.ID] = kept[i].ID
	}

	return result
}

// compareRoute orders a workout with a route before one without.
func compareRoute(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}

// duplicate reports whether a and b record the same workout: of different sources or different
// origins, of the same activity family, overlapping by more than 80 percent of the shorter one.
func duplicate(a, b Candidate) bool {
	if a.ID == b.ID || a.Family != b.Family || (a.Source.ID == b.Source.ID && a.Origin == b.Origin) {
		return false
	}

	start, end := a.Start, a.End
	if b.Start.After(start) {
		start = b.Start
	}

	if b.End.Before(end) {
		end = b.End
	}

	overlap := end.Sub(start)
	shorter := min(a.End.Sub(a.Start), b.End.Sub(b.Start))

	return overlap > 0 && overlap*5 > shorter*4
}
