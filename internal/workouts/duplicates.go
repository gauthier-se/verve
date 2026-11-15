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
	ID              int64
	SourceID        int64
	SourceKind      store.SourceKind
	SourceFirstSeen time.Time
	Origin          store.Origin
	// Family is the activity family of its activity type.
	Family   catalog.Family
	Start    time.Time
	End      time.Time
	HasRoute bool
}

// Duplicates returns the id of the kept workout that each workout of group duplicates, by id, 0 for
// a kept workout.
//
// The workouts are ranked by the kind of their source in the default activity and energy list,
// then by first seen of their source, then by source id, as FR-4 orders sources, which leaves the
// workouts of one source together: among them, the one with a route first, then the one imported
// first, of the lower id. In that order, each workout duplicates the first kept workout that it
// duplicates (see duplicate), and is kept when there is none. The result of a workout depends only
// on the workouts of group that it duplicates, so that a group closed under duplicate gives the
// result that all the workouts of the user would.
func Duplicates(group []Candidate) map[int64]int64 {
	ranks := aggregate.DefaultRanks(catalog.GroupActivity)

	ranked := slices.Clone(group)
	slices.SortFunc(ranked, func(a, b Candidate) int {
		return cmp.Or(
			cmp.Compare(ranks[a.SourceKind], ranks[b.SourceKind]),
			a.SourceFirstSeen.Compare(b.SourceFirstSeen),
			cmp.Compare(a.SourceID, b.SourceID),
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
	if a.ID == b.ID || a.Family != b.Family || (a.SourceID == b.SourceID && a.Origin == b.Origin) {
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
