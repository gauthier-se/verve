package workouts

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/gauthier-se/verve/internal/catalog"
	"github.com/gauthier-se/verve/internal/ingest"
	"github.com/gauthier-se/verve/internal/store"
)

// Detector flags the duplicate workouts of the batches of the pipeline (FR-11 of Deduplication and
// source priority). It is the duplicate detector of the pipeline.
type Detector struct {
	activities *catalog.Activities
}

var _ ingest.WorkoutDuplicates = Detector{}

// NewDetector returns a detector that finds the family of each activity type in activities.
func NewDetector(activities *catalog.Activities) Detector {
	return Detector{activities: activities}
}

// Evaluate evaluates again the duplicate groups that changes overlap, and sets the kept workout of
// each workout of those groups whose kept workout changed.
//
// The group starts with the workouts of changes that are still stored, and the duplicates of every
// workout of changes, so that the duplicates of a kept workout removed or moved away are evaluated
// again. It then grows with each workout that a workout of the group duplicates. Those are read
// from the index on workouts (user_id, start_at), from the start of each workout of the group
// widened by the longest duration of the group to its end, not by a scan of the workouts of the
// user. A longer workout that starts before that misses the group, until it is written itself.
func (d Detector) Evaluate(ctx context.Context, tx store.Repositories, userID int64, changes []ingest.WorkoutChange) error {
	group, err := d.group(ctx, tx.Workouts(), userID, changes)
	if err != nil {
		return err
	}

	candidates, err := d.candidates(ctx, tx, userID, group)
	if err != nil {
		return err
	}

	kept := Duplicates(candidates)

	for _, id := range slices.Sorted(maps.Keys(group)) {
		if kept[id] == group[id].DuplicateOf {
			continue
		}

		if err := tx.Workouts().SetDuplicateOf(ctx, userID, id, kept[id]); err != nil {
			return fmt.Errorf("set duplicate of workout %d: %w", id, err)
		}
	}

	return nil
}

// groupSearch is the search of the group of the changes of a batch.
type groupSearch struct {
	d       Detector
	repo    store.WorkoutsRepository
	userID  int64
	members map[int64]store.Workout
	// longest is the longest duration of the group, changes included.
	longest time.Duration
	// queried holds the longest duration that the window of each member was read with.
	queried map[int64]time.Duration
}

// group returns the workouts of the duplicate groups that changes overlap, by id.
func (d Detector) group(ctx context.Context, repo store.WorkoutsRepository, userID int64, changes []ingest.WorkoutChange) (map[int64]store.Workout, error) {
	s := &groupSearch{d: d, repo: repo, userID: userID, members: make(map[int64]store.Workout), queried: make(map[int64]time.Duration)}

	ids := make([]int64, 0, len(changes))
	for _, c := range changes {
		ids = append(ids, c.ID)
		s.longest = max(s.longest, c.End.Sub(c.Start))
	}

	duplicates, err := repo.ListDuplicatesOf(ctx, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("list duplicate workouts: %w", err)
	}

	for _, w := range duplicates {
		s.add(w)
	}

	for _, c := range changes {
		if err := s.expand(ctx, c.ID, c.Start, c.End); err != nil {
			return nil, err
		}
	}

	for {
		pending := slices.Sorted(maps.Keys(s.members))
		pending = slices.DeleteFunc(pending, func(id int64) bool { return s.queried[id] >= s.longest })

		if len(pending) == 0 {
			return s.members, nil
		}

		for _, id := range pending {
			m := s.members[id]
			if err := s.expand(ctx, id, m.Start, m.End); err != nil {
				return nil, err
			}
		}
	}
}

// add adds w to the group.
func (s *groupSearch) add(w store.Workout) {
	s.members[w.ID] = w
	s.longest = max(s.longest, w.End.Sub(w.Start))
}

// expand reads the workouts that start in the window of the workout of id, from start widened by the
// longest duration of the group to end, and adds the workout of id and those that it duplicates.
func (s *groupSearch) expand(ctx context.Context, id int64, start, end time.Time) error {
	longest := s.longest

	// The end is included, so that a workout of no duration finds itself.
	window, err := s.repo.ListStarting(ctx, s.userID, start.Add(-longest), end.Add(time.Millisecond))
	if err != nil {
		return fmt.Errorf("list workouts starting near workout %d: %w", id, err)
	}

	i := slices.IndexFunc(window, func(w store.Workout) bool { return w.ID == id })
	if i < 0 {
		// A removed workout: its duplicates are in the group already.
		return nil
	}

	self := window[i]
	s.add(self)
	s.queried[id] = longest

	for _, w := range window {
		if _, ok := s.members[w.ID]; !ok && duplicate(s.d.candidate(self), s.d.candidate(w)) {
			s.add(w)
		}
	}

	return nil
}

// candidate returns w as duplicate detection compares it, without the details of its source and
// its route.
func (d Detector) candidate(w store.Workout) Candidate {
	family := catalog.FamilyOther
	if activity, ok := d.activities.ByKey(w.Activity); ok {
		family = activity.Family
	}

	return Candidate{ID: w.ID, SourceID: w.SourceID, Origin: w.Origin, Family: family, Start: w.Start, End: w.End}
}

// candidates returns the workouts of group as duplicate detection compares them, by id.
func (d Detector) candidates(ctx context.Context, tx store.Repositories, userID int64, group map[int64]store.Workout) ([]Candidate, error) {
	sources, err := tx.Sources().List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}

	byID := make(map[int64]store.Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}

	ids := slices.Sorted(maps.Keys(group))

	thumbnails, err := tx.Workouts().Thumbnails(ctx, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("list route thumbnails: %w", err)
	}

	routes := make(map[int64]bool, len(thumbnails))
	for _, thumbnail := range thumbnails {
		routes[thumbnail.WorkoutID] = true
	}

	candidates := make([]Candidate, 0, len(ids))

	for _, id := range ids {
		c := d.candidate(group[id])
		source := byID[c.SourceID]
		c.SourceKind, c.SourceFirstSeen, c.HasRoute = source.Kind, source.FirstSeenAt, routes[id]
		candidates = append(candidates, c)
	}

	return candidates, nil
}
