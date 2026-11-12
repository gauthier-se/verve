package storetest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

// workoutsTime has sub-millisecond digits and a non-UTC zone, so that the suite checks that the
// times of a workout and of its events are truncated to the millisecond and returned in UTC.
var workoutsTime = time.Date(2026, 11, 12, 18, 5, 0, 456_789_123, time.FixedZone("CET", 3600))

// workoutsUTC is workoutsTime truncated to the millisecond, in UTC.
var workoutsUTC = time.Date(2026, 11, 12, 17, 5, 0, 456_000_000, time.UTC)

// allWorkouts is a range that holds every workout of the suite.
var allWorkouts = store.WorkoutsQuery{From: time.Unix(0, 0), To: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}

// workoutsTest holds a store with two users and a source of each.
type workoutsTest struct {
	store   store.Store
	user    store.User
	other   store.User
	source  store.Source
	foreign store.Source
}

func newWorkoutsTest(t *testing.T, newStore NewStoreFunc) workoutsTest {
	t.Helper()

	s := newStore(t, clock.NewFake(workoutsTime))
	ctx := t.Context()
	user := mustCreateUser(ctx, t, s.Users(), newUser("camille", "camille@example.com"))
	other := mustCreateUser(ctx, t, s.Users(), newUser("dominique", "dominique@example.com"))

	return workoutsTest{
		store:   s,
		user:    user,
		other:   other,
		source:  mustResolve(ctx, t, s.Sources(), user.ID, watchInfo, store.SourceWatch, workoutsTime),
		foreign: mustResolve(ctx, t, s.Sources(), other.ID, watchInfo, store.SourceWatch, workoutsTime),
	}
}

// run returns a 30 minute run of the user of st from the XML export, keyed by key, starting at
// start, without totals, events or metadata, to store with the rule ignore.
func (st workoutsTest) run(key string, start time.Time) store.NewWorkout {
	return store.NewWorkout{
		Workout: store.Workout{
			SourceID:     st.source.ID,
			Origin:       store.OriginXML,
			Activity:     "running",
			Start:        start,
			End:          start.Add(30 * time.Minute),
			UTCOffset:    3600,
			Duration:     1800,
			LocationType: store.LocationUnknown,
			ImportID:     42,
			NaturalKey:   []byte(key),
		},
		OnConflict: store.ConflictIgnore,
	}
}

// fullRun returns a run of the user of st from Health Auto Export, keyed by key, with every field of
// FR-4 of Storage and aggregates set, to store with the rule update.
func (st workoutsTest) fullRun(key string) store.NewWorkout {
	workout := st.run(key, workoutsTime)
	workout.Origin = store.OriginHAE
	workout.ExternalID = "9B2C7E14-5A3D-4F8B-B1E6-3C0D9A7F2E58"
	workout.End = workoutsTime.Add(47 * time.Minute)
	workout.UTCOffset = -16200
	workout.Duration = 2700.5
	workout.Distance = ptr(8012.25)
	workout.ActiveEnergy = ptr(612.5)
	workout.TotalEnergy = ptr(689.75)
	workout.ElevationGain = ptr(84.5)
	workout.ElevationLoss = ptr(0)
	workout.AvgHeartRate = ptr(152.5)
	workout.MaxHeartRate = ptr(181)
	workout.Indoor = boolPtr(false)
	workout.LocationType = store.LocationOutdoor
	workout.Events = []store.WorkoutEvent{
		{Type: store.WorkoutEventSegment, Start: workoutsTime, End: workoutsTime.Add(20 * time.Minute)},
		{Type: store.WorkoutEventLap, Start: workoutsTime, End: workoutsTime.Add(5*time.Minute + 30*time.Second)},
		{Type: store.WorkoutEventPause, Start: workoutsTime.Add(21 * time.Minute), End: workoutsTime.Add(21 * time.Minute)},
		{Type: store.WorkoutEventResume, Start: workoutsTime.Add(23 * time.Minute), End: workoutsTime.Add(23 * time.Minute)},
		{Type: store.WorkoutEventMarker, Start: workoutsTime.Add(30 * time.Minute), End: workoutsTime.Add(30 * time.Minute)},
	}
	workout.Metadata = json.RawMessage(`{"HKMetadataKeyWeatherTemperature":"12 degC","HKElevationAscended":"8450 cm"}`)
	workout.ImportID = 7
	workout.OnConflict = store.ConflictUpdate

	return workout
}

// foreignRun is run for the other user of st.
func (st workoutsTest) foreignRun(key string, start time.Time) store.NewWorkout {
	workout := st.run(key, start)
	workout.SourceID = st.foreign.ID

	return workout
}

func (st workoutsTest) mustUpsert(ctx context.Context, t *testing.T, userID int64, workout store.NewWorkout) store.WorkoutUpsert {
	t.Helper()

	got, err := st.store.Workouts().Upsert(ctx, userID, workout)
	require.NoError(t, err)

	return got
}

func (st workoutsTest) mustGet(ctx context.Context, t *testing.T, userID, id int64) store.Workout {
	t.Helper()

	got, err := st.store.Workouts().Get(ctx, userID, id)
	require.NoError(t, err)

	return got
}

// mustListAll returns every workout of userID, read in pages of two, so that a list of a few
// workouts takes several pages.
func (st workoutsTest) mustListAll(ctx context.Context, t *testing.T, userID int64) []store.Workout {
	t.Helper()

	const pageSize = 2

	query := allWorkouts
	query.Limit = pageSize

	var all []store.Workout

	for {
		page, err := st.store.Workouts().List(ctx, userID, query)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), pageSize)

		all = append(all, page...)

		if len(page) < pageSize {
			return all
		}

		query.After = page[len(page)-1].Cursor()
	}
}

// storedWorkout returns workout as the store returns it for userID, with id.
func storedWorkout(workout store.NewWorkout, userID, id int64) store.Workout {
	got := workout.Workout
	got.ID = id
	got.UserID = userID
	got.Start = got.Start.UTC().Truncate(time.Millisecond)
	got.End = got.End.UTC().Truncate(time.Millisecond)

	if len(got.Events) == 0 {
		got.Events = nil
	} else {
		got.Events = make([]store.WorkoutEvent, len(workout.Events))
		for i, event := range workout.Events {
			event.Start = event.Start.UTC().Truncate(time.Millisecond)
			event.End = event.End.UTC().Truncate(time.Millisecond)
			got.Events[i] = event
		}
	}

	return got
}

func boolPtr(v bool) *bool {
	return &v
}

// RunWorkouts runs the WorkoutsRepository contract against the stores returned by newStore.
func RunWorkouts(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runWorkoutsUpsert(t, newStore)
	runWorkoutsConflicts(t, newStore)
	runWorkoutsInvalid(t, newStore)
	runWorkoutsList(t, newStore)
	runWorkoutsGet(t, newStore)
	runWorkoutsDelete(t, newStore)
}

// runWorkoutsUpsert runs the cases of the workouts stored by an upsert.
func runWorkoutsUpsert(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a workout with every field of FR-4, events and metadata included, when Upsert runs, then
	// it is inserted with every field, its times and those of its events in UTC with millisecond
	// precision, and the result gives its id.
	t.Run("UpsertStoresEveryFieldOfTheWorkout", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		workout := st.fullRun("full")
		workout.NaturalKey = []byte{0x00, 0xff, 0x10, 0x80}

		got := st.mustUpsert(ctx, t, st.user.ID, workout)
		require.Equal(t, store.OutcomeInserted, got.Outcome)
		require.NotZero(t, got.ID)

		stored := st.mustGet(ctx, t, st.user.ID, got.ID)
		require.Equal(t, workoutsUTC, stored.Start)
		require.Equal(t, workoutsUTC, stored.Events[0].Start)
		require.Equal(t, storedWorkout(workout, st.user.ID, got.ID), stored)
	})

	// Given a workout without totals, indoor flag, events or metadata, then they come back nil.
	t.Run("UpsertStoresAWorkoutWithoutOptionalFields", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		workout := st.run("bare", workoutsTime)
		workout.Events = []store.WorkoutEvent{}

		got := st.mustUpsert(ctx, t, st.user.ID, workout)
		require.Equal(t, store.OutcomeInserted, got.Outcome)

		stored := st.mustGet(ctx, t, st.user.ID, got.ID)
		require.Equal(t, storedWorkout(workout, st.user.ID, got.ID), stored)
		require.Nil(t, stored.Distance)
		require.Nil(t, stored.Indoor)
		require.Nil(t, stored.Events)
		require.Nil(t, stored.Metadata)
	})

	// Given the natural key of a workout of another user, then the workout is inserted, and each
	// user lists only their own.
	t.Run("UpsertOfTheNaturalKeyOfAnotherUserInserts", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		foreign := st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))

		mine := st.run("a", workoutsTime.Add(time.Hour))
		got := st.mustUpsert(ctx, t, st.user.ID, mine)
		require.Equal(t, store.OutcomeInserted, got.Outcome)
		require.NotEqual(t, foreign.ID, got.ID)

		require.Equal(t, []store.Workout{storedWorkout(mine, st.user.ID, got.ID)}, st.mustListAll(ctx, t, st.user.ID))

		others := st.mustListAll(ctx, t, st.other.ID)
		require.Len(t, others, 1)
		require.Equal(t, foreign.ID, others[0].ID)
	})
}

// runWorkoutsConflicts runs the cases of the conflict rules.
func runWorkoutsConflicts(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given the rule update and a natural key already stored, then every field of the workout is
	// replaced, it keeps its id, so that its route stays attached, and the result says updated. The
	// origin and external id stay, since they make the natural key.
	t.Run("UpdateReplacesTheStoredWorkoutAndKeepsItsID", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		first := st.mustUpsert(ctx, t, st.user.ID, st.fullRun("hae"))

		again := st.run("hae", workoutsTime.Add(time.Minute))
		again.Origin = store.OriginHAE
		again.Activity = "trail_running"
		again.ExternalID = "9B2C7E14-5A3D-4F8B-B1E6-3C0D9A7F2E58"
		again.Distance = ptr(8100)
		again.Indoor = boolPtr(true)
		again.LocationType = store.LocationIndoor
		again.Events = []store.WorkoutEvent{{Type: store.WorkoutEventLap, Start: workoutsTime, End: workoutsTime.Add(time.Minute)}}
		again.ImportID = 8
		again.OnConflict = store.ConflictUpdate

		got := st.mustUpsert(ctx, t, st.user.ID, again)
		require.Equal(t, store.WorkoutUpsert{ID: first.ID, Outcome: store.OutcomeUpdated}, got)

		require.Equal(t, []store.Workout{storedWorkout(again, st.user.ID, first.ID)}, st.mustListAll(ctx, t, st.user.ID))
	})

	// Given the rule ignore and a natural key already stored, then the workout counts as a duplicate
	// of the stored one, whose id the result gives, and the stored one is unchanged.
	t.Run("IgnoreKeepsTheStoredWorkout", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		first := st.mustUpsert(ctx, t, st.user.ID, st.run("xml", workoutsTime))
		before := st.mustListAll(ctx, t, st.user.ID)

		again := st.fullRun("xml")
		again.OnConflict = store.ConflictIgnore

		got := st.mustUpsert(ctx, t, st.user.ID, again)
		require.Equal(t, store.WorkoutUpsert{ID: first.ID, Outcome: store.OutcomeDuplicate}, got)

		require.Equal(t, before, st.mustListAll(ctx, t, st.user.ID))
	})
}

// runWorkoutsInvalid runs the cases of the workouts that Upsert refuses.
func runWorkoutsInvalid(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	cases := []struct {
		name  string
		spoil func(st workoutsTest, w *store.NewWorkout)
	}{
		{"SourceOfAnotherUser", func(st workoutsTest, w *store.NewWorkout) { w.SourceID = st.foreign.ID }},
		{"UnknownSource", func(st workoutsTest, w *store.NewWorkout) { w.SourceID = st.source.ID + st.foreign.ID + 1 }},
		{"EmptyNaturalKey", func(_ workoutsTest, w *store.NewWorkout) { w.NaturalKey = nil }},
		{"RuleOfASample", func(_ workoutsTest, w *store.NewWorkout) { w.OnConflict = store.ConflictReplace }},
		{"UnknownRule", func(_ workoutsTest, w *store.NewWorkout) { w.OnConflict = "merge" }},
		{"UnknownOrigin", func(_ workoutsTest, w *store.NewWorkout) { w.Origin = "csv" }},
		{"EmptyActivity", func(_ workoutsTest, w *store.NewWorkout) { w.Activity = "" }},
		{"EndBeforeStart", func(_ workoutsTest, w *store.NewWorkout) { w.End = w.Start.Add(-time.Millisecond) }},
		{"NegativeDuration", func(_ workoutsTest, w *store.NewWorkout) { w.Duration = -1 }},
		{"UnknownLocationType", func(_ workoutsTest, w *store.NewWorkout) { w.LocationType = "space" }},
		{"UnknownEventType", func(_ workoutsTest, w *store.NewWorkout) {
			w.Events = []store.WorkoutEvent{{Type: "split", Start: w.Start, End: w.Start}}
		}},
		{"EventEndBeforeStart", func(_ workoutsTest, w *store.NewWorkout) {
			w.Events = []store.WorkoutEvent{{Type: store.WorkoutEventLap, Start: w.Start, End: w.Start.Add(-time.Millisecond)}}
		}},
		{"MetadataNotJSON", func(_ workoutsTest, w *store.NewWorkout) { w.Metadata = json.RawMessage(`{"a":`) }},
		{"MetadataNotAnObject", func(_ workoutsTest, w *store.NewWorkout) { w.Metadata = json.RawMessage(`[1]`) }},
	}

	// Given an invalid workout, then Upsert answers ErrInvalid and writes nothing, also in a WithTx
	// whose caller commits despite the error.
	for _, tc := range cases {
		t.Run("UpsertRefusesAWorkoutWith"+tc.name, func(t *testing.T) {
			t.Parallel()

			st := newWorkoutsTest(t, newStore)
			ctx := t.Context()

			bad := st.run("bad", workoutsTime)
			tc.spoil(st, &bad)

			_, err := st.store.Workouts().Upsert(ctx, st.user.ID, bad)
			require.ErrorIs(t, err, store.ErrInvalid)

			require.NoError(t, st.store.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
				_, err := tx.Workouts().Upsert(ctx, st.user.ID, bad)
				require.ErrorIs(t, err, store.ErrInvalid)

				return nil
			}))

			require.Empty(t, st.mustListAll(ctx, t, st.user.ID))
			require.Empty(t, st.mustListAll(ctx, t, st.other.ID))
		})
	}

	// Given a stored workout and the rule update, when a workout of its natural key has a source of
	// another user, then Upsert answers ErrInvalid and the stored workout is unchanged.
	t.Run("UpdateWithASourceOfAnotherUserChangesNothing", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		st.mustUpsert(ctx, t, st.user.ID, st.fullRun("hae"))
		before := st.mustListAll(ctx, t, st.user.ID)

		again := st.fullRun("hae")
		again.SourceID = st.foreign.ID
		again.Distance = ptr(1)

		_, err := st.store.Workouts().Upsert(ctx, st.user.ID, again)
		require.ErrorIs(t, err, store.ErrInvalid)

		require.Equal(t, before, st.mustListAll(ctx, t, st.user.ID))
	})

	// Given an unknown user, then Upsert answers ErrInvalid, since no source belongs to them.
	t.Run("UpsertRefusesAnUnknownUser", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)

		_, err := st.store.Workouts().Upsert(t.Context(), st.user.ID+st.other.ID+1, st.run("a", workoutsTime))
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}

// runWorkoutsList runs the cases of List.
func runWorkoutsList(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given workouts of a user inserted out of order, and a workout of another user, when List reads
	// a range in pages, then it returns the workouts of that user whose start is in the range, most
	// recent first by start then id, each once.
	t.Run("ListReturnsTheWorkoutsOfTheUserInTheRangeMostRecentFirstInPages", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		at := func(days int) time.Time { return workoutsTime.AddDate(0, 0, days) }

		for _, workout := range []store.NewWorkout{
			st.run("before", at(-1)),
			st.run("third", at(3)),
			st.run("first-a", at(0)),
			st.run("second", at(1)),
			st.run("first-b", at(0)),
			st.run("to", at(4)),
		} {
			st.mustUpsert(ctx, t, st.user.ID, workout)
		}

		st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("foreign", at(2)))

		query := store.WorkoutsQuery{From: at(0), To: at(4), Limit: 2}

		var keys []string

		for range 3 {
			page, err := st.store.Workouts().List(ctx, st.user.ID, query)
			require.NoError(t, err)
			require.LessOrEqual(t, len(page), 2)

			for _, workout := range page {
				require.Equal(t, st.user.ID, workout.UserID)
				keys = append(keys, string(workout.NaturalKey))
			}

			if len(page) > 0 {
				query.After = page[len(page)-1].Cursor()
			}
		}

		require.Equal(t, []string{"third", "second", "first-b", "first-a"}, keys)
	})

	// Given a limit below 1 or a range whose end is not after its start, then List answers
	// ErrInvalid.
	t.Run("ListRefusesAnInvalidQuery", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		for _, query := range []store.WorkoutsQuery{
			{From: workoutsTime, To: workoutsTime.Add(time.Hour), Limit: 0},
			{From: workoutsTime, To: workoutsTime, Limit: 10},
		} {
			_, err := st.store.Workouts().List(ctx, st.user.ID, query)
			require.ErrorIs(t, err, store.ErrInvalid)
		}
	})
}

// runWorkoutsGet runs the cases of Get.
func runWorkoutsGet(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given the id of a workout of another user, or an unknown id, then Get answers ErrNotFound.
	t.Run("GetAnswersNotFoundForAWorkoutOfAnotherUser", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		foreign := st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))

		_, err := st.store.Workouts().Get(ctx, st.user.ID, foreign.ID)
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = st.store.Workouts().Get(ctx, st.other.ID, foreign.ID+1)
		require.ErrorIs(t, err, store.ErrNotFound)

		require.Equal(t, st.other.ID, st.mustGet(ctx, t, st.other.ID, foreign.ID).UserID)
	})
}

// runWorkoutsDelete runs the cases of DeleteByUser.
func runWorkoutsDelete(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given three workouts of a user and a workout of another user, when DeleteByUser runs with a
	// limit of two, then each call removes at most two workouts, and the other user keeps theirs.
	t.Run("DeleteByUserRemovesTheWorkoutsInBatches", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		for _, key := range []string{"a", "b", "c"} {
			st.mustUpsert(ctx, t, st.user.ID, st.run(key, workoutsTime))
		}

		st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))

		var removed []int64

		for {
			deleted, err := st.store.Workouts().DeleteByUser(ctx, st.user.ID, 2)
			require.NoError(t, err)

			removed = append(removed, deleted)

			if deleted < 2 {
				break
			}
		}

		require.Equal(t, []int64{2, 1}, removed)
		require.Empty(t, st.mustListAll(ctx, t, st.user.ID))
		require.Len(t, st.mustListAll(ctx, t, st.other.ID), 1)

		_, err := st.store.Workouts().DeleteByUser(ctx, st.user.ID, 0)
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}
