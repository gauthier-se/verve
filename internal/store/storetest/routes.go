package storetest

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/store"
)

// lyonRoute returns a route around Lyon, its encoding a stand-in with a zero byte and bytes that
// are not UTF-8, so that the suite checks that it comes back byte for byte.
func lyonRoute() store.WorkoutRoute {
	return store.WorkoutRoute{
		Encoded:    []byte{0x01, 0x28, 0xb5, 0x2f, 0xfd, 0x00, 0xff, 0x80},
		Box:        store.RouteBox{MinLat: 45.7484721, MinLon: 4.8264531, MaxLat: 45.7674358, MaxLon: 4.8573159},
		Distance:   8012.25,
		PointCount: 2700,
		Polyline:   "_p~iF~ps|U_ulLnnqC_mqNvxq`@",
	}
}

// parisRoute returns another route, around Paris, every field different from those of lyonRoute.
func parisRoute() store.WorkoutRoute {
	return store.WorkoutRoute{
		Encoded:    []byte{0x01, 0x28, 0xb5, 0x2f, 0xfd, 0x42},
		Box:        store.RouteBox{MinLat: 48.8462218, MinLon: 2.3371129, MaxLat: 48.8583701, MaxLon: 2.3522219},
		Distance:   5003.5,
		PointCount: 1501,
		Polyline:   "o}iiHmsaM~@vA",
	}
}

func (st workoutsTest) mustSetRoute(ctx context.Context, t *testing.T, userID, workoutID int64, route store.WorkoutRoute) {
	t.Helper()

	require.NoError(t, st.store.Workouts().SetRoute(ctx, userID, workoutID, route))
}

func (st workoutsTest) mustGetRoute(ctx context.Context, t *testing.T, userID, workoutID int64) store.WorkoutRoute {
	t.Helper()

	got, err := st.store.Workouts().GetRoute(ctx, userID, workoutID)
	require.NoError(t, err)

	return got
}

func thumbnailOf(workoutID int64, route store.WorkoutRoute) store.RouteThumbnail {
	return store.RouteThumbnail{WorkoutID: workoutID, Box: route.Box, Polyline: route.Polyline}
}

// RunWorkoutRoutes runs the contract of the routes of WorkoutsRepository against the stores
// returned by newStore.
func RunWorkoutRoutes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	runWorkoutRoutesSet(t, newStore)
	runWorkoutRoutesInvalid(t, newStore)
	runWorkoutRoutesGet(t, newStore)
	runWorkoutRoutesThumbnails(t, newStore)
	runWorkoutRoutesDelete(t, newStore)
}

// runWorkoutRoutesSet runs the cases of the routes stored by SetRoute.
func runWorkoutRoutesSet(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a workout, when SetRoute runs, then its encoded route, bounding box, distance, point
	// count and polyline are stored.
	t.Run("SetRouteStoresEveryFieldOfTheRoute", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		workout := st.mustUpsert(ctx, t, st.user.ID, st.run("a", workoutsTime))

		st.mustSetRoute(ctx, t, st.user.ID, workout.ID, lyonRoute())
		require.Equal(t, lyonRoute(), st.mustGetRoute(ctx, t, st.user.ID, workout.ID))
	})

	// Given a workout with a route, when SetRoute runs a second time, then the second route
	// replaces the first: a workout has at most one route.
	t.Run("SetRouteReplacesTheRouteOfTheWorkout", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		workout := st.mustUpsert(ctx, t, st.user.ID, st.run("a", workoutsTime))

		st.mustSetRoute(ctx, t, st.user.ID, workout.ID, lyonRoute())
		st.mustSetRoute(ctx, t, st.user.ID, workout.ID, parisRoute())
		require.Equal(t, parisRoute(), st.mustGetRoute(ctx, t, st.user.ID, workout.ID))

		thumbnails, err := st.store.Workouts().Thumbnails(ctx, st.user.ID, []int64{workout.ID})
		require.NoError(t, err)
		require.Equal(t, []store.RouteThumbnail{thumbnailOf(workout.ID, parisRoute())}, thumbnails)
	})

	// Given a workout updated by the rule update, then its route stays attached.
	t.Run("SetRouteKeepsTheRouteOfAnUpdatedWorkout", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		workout := st.fullRun("a")
		inserted := st.mustUpsert(ctx, t, st.user.ID, workout)
		st.mustSetRoute(ctx, t, st.user.ID, inserted.ID, lyonRoute())

		workout.Duration++
		updated := st.mustUpsert(ctx, t, st.user.ID, workout)
		require.Equal(t, store.OutcomeUpdated, updated.Outcome)
		require.Equal(t, lyonRoute(), st.mustGetRoute(ctx, t, st.user.ID, updated.ID))
	})

	// Given a workout of another user, or an unknown workout, when SetRoute runs, then it answers
	// ErrNotFound and stores nothing.
	t.Run("SetRouteAnswersNotFoundForAWorkoutOfAnotherUser", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		foreign := st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))

		err := st.store.Workouts().SetRoute(ctx, st.user.ID, foreign.ID, lyonRoute())
		require.ErrorIs(t, err, store.ErrNotFound)

		err = st.store.Workouts().SetRoute(ctx, st.other.ID, foreign.ID+1, lyonRoute())
		require.ErrorIs(t, err, store.ErrNotFound)

		_, err = st.store.Workouts().GetRoute(ctx, st.other.ID, foreign.ID)
		require.ErrorIs(t, err, store.ErrNotFound, "the workout of the other user has no route")
	})
}

// runWorkoutRoutesInvalid runs the cases of the routes that SetRoute refuses.
func runWorkoutRoutesInvalid(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	invalid := []struct {
		name   string
		change func(route *store.WorkoutRoute)
	}{
		{"EmptyEncoding", func(route *store.WorkoutRoute) { route.Encoded = nil }},
		{"NoPoint", func(route *store.WorkoutRoute) { route.PointCount = 0 }},
		{"NegativeDistance", func(route *store.WorkoutRoute) { route.Distance = -1 }},
		{"NaNDistance", func(route *store.WorkoutRoute) { route.Distance = math.NaN() }},
		{"InfiniteDistance", func(route *store.WorkoutRoute) { route.Distance = math.Inf(1) }},
		{"MinLatAboveMaxLat", func(route *store.WorkoutRoute) { route.Box.MinLat = route.Box.MaxLat + 1e-7 }},
		{"MinLonAboveMaxLon", func(route *store.WorkoutRoute) { route.Box.MinLon = route.Box.MaxLon + 1e-7 }},
		{"NaNLatitude", func(route *store.WorkoutRoute) { route.Box.MaxLat = math.NaN() }},
		{"LatitudeOutOfRange", func(route *store.WorkoutRoute) { route.Box.MaxLat = 90.5 }},
		{"LongitudeOutOfRange", func(route *store.WorkoutRoute) { route.Box.MinLon = -180.5 }},
	}

	for _, tc := range invalid {
		// Given a route with a field that the store refuses, when SetRoute runs, then it answers
		// ErrInvalid and the stored route stays as it was.
		t.Run("SetRouteRefuses"+tc.name, func(t *testing.T) {
			t.Parallel()

			st := newWorkoutsTest(t, newStore)
			ctx := t.Context()

			workout := st.mustUpsert(ctx, t, st.user.ID, st.run("a", workoutsTime))
			st.mustSetRoute(ctx, t, st.user.ID, workout.ID, lyonRoute())

			route := parisRoute()
			tc.change(&route)

			err := st.store.Workouts().SetRoute(ctx, st.user.ID, workout.ID, route)
			require.ErrorIs(t, err, store.ErrInvalid)
			require.Equal(t, lyonRoute(), st.mustGetRoute(ctx, t, st.user.ID, workout.ID))
		})
	}
}

// runWorkoutRoutesGet runs the cases of GetRoute.
func runWorkoutRoutesGet(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a workout without route, the workout with a route of another user, or an unknown
	// workout, then GetRoute answers ErrNotFound.
	t.Run("GetRouteAnswersNotFoundWithoutARouteOfTheUser", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		bare := st.mustUpsert(ctx, t, st.user.ID, st.run("a", workoutsTime))
		foreign := st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))
		st.mustSetRoute(ctx, t, st.other.ID, foreign.ID, lyonRoute())

		for _, id := range []int64{bare.ID, foreign.ID, foreign.ID + 1} {
			_, err := st.store.Workouts().GetRoute(ctx, st.user.ID, id)
			require.ErrorIs(t, err, store.ErrNotFound, "workout %d", id)
		}

		require.Equal(t, lyonRoute(), st.mustGetRoute(ctx, t, st.other.ID, foreign.ID))
	})
}

// runWorkoutRoutesThumbnails runs the cases of Thumbnails.
func runWorkoutRoutesThumbnails(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given workouts with routes, a workout without route, a workout with a route of another user
	// and an unknown id, then Thumbnails returns the polylines and bounding boxes of the routes of
	// the user only, by workout id, without their encoded routes.
	t.Run("ThumbnailsReturnsTheRoutesOfTheUserWithoutTheirEncoding", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		first := st.mustUpsert(ctx, t, st.user.ID, st.run("a", workoutsTime))
		second := st.mustUpsert(ctx, t, st.user.ID, st.run("b", workoutsTime.Add(time.Hour)))
		bare := st.mustUpsert(ctx, t, st.user.ID, st.run("c", workoutsTime.Add(2*time.Hour)))
		foreign := st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))

		st.mustSetRoute(ctx, t, st.user.ID, first.ID, lyonRoute())
		st.mustSetRoute(ctx, t, st.user.ID, second.ID, parisRoute())
		st.mustSetRoute(ctx, t, st.other.ID, foreign.ID, lyonRoute())

		got, err := st.store.Workouts().Thumbnails(ctx, st.user.ID,
			[]int64{second.ID, foreign.ID, bare.ID, first.ID, foreign.ID + 100})
		require.NoError(t, err)
		require.Equal(t, []store.RouteThumbnail{
			thumbnailOf(first.ID, lyonRoute()),
			thumbnailOf(second.ID, parisRoute()),
		}, got)
	})

	// Given no workout id, then Thumbnails returns none.
	t.Run("ThumbnailsOfNoWorkoutIsEmpty", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)

		got, err := st.store.Workouts().Thumbnails(t.Context(), st.user.ID, nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// runWorkoutRoutesDelete runs the cases of the removal of routes.
func runWorkoutRoutesDelete(t *testing.T, newStore NewStoreFunc) {
	t.Helper()

	// Given a workout with a route, when the workout is removed, then its route goes with it.
	t.Run("DeleteByUserRemovesTheRoutesOfTheWorkouts", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		workout := st.mustUpsert(ctx, t, st.user.ID, st.run("a", workoutsTime))
		st.mustSetRoute(ctx, t, st.user.ID, workout.ID, lyonRoute())

		deleted, err := st.store.Workouts().DeleteByUser(ctx, st.user.ID, 10)
		require.NoError(t, err)
		require.Equal(t, int64(1), deleted)

		_, err = st.store.Workouts().GetRoute(ctx, st.user.ID, workout.ID)
		require.ErrorIs(t, err, store.ErrNotFound)

		routes, err := st.store.Workouts().DeleteRoutesByUser(ctx, st.user.ID, 10)
		require.NoError(t, err)
		require.Zero(t, routes, "no route is left")
	})

	// Given three routes of a user and a route of another user, when DeleteRoutesByUser runs with a
	// limit of two, then each call removes at most two routes, the workouts stay, and the other
	// user keeps their route.
	t.Run("DeleteRoutesByUserRemovesTheRoutesInBatches", func(t *testing.T) {
		t.Parallel()

		st := newWorkoutsTest(t, newStore)
		ctx := t.Context()

		var ids []int64

		for i, key := range []string{"a", "b", "c"} {
			workout := st.mustUpsert(ctx, t, st.user.ID, st.run(key, workoutsTime.Add(time.Duration(i)*time.Hour)))
			st.mustSetRoute(ctx, t, st.user.ID, workout.ID, lyonRoute())
			ids = append(ids, workout.ID)
		}

		foreign := st.mustUpsert(ctx, t, st.other.ID, st.foreignRun("a", workoutsTime))
		st.mustSetRoute(ctx, t, st.other.ID, foreign.ID, parisRoute())

		var removed []int64

		for {
			deleted, err := st.store.Workouts().DeleteRoutesByUser(ctx, st.user.ID, 2)
			require.NoError(t, err)

			removed = append(removed, deleted)

			if deleted < 2 {
				break
			}
		}

		require.Equal(t, []int64{2, 1}, removed)

		thumbnails, err := st.store.Workouts().Thumbnails(ctx, st.user.ID, ids)
		require.NoError(t, err)
		require.Empty(t, thumbnails)
		require.Len(t, st.mustListAll(ctx, t, st.user.ID), 3, "the workouts stay")
		require.Equal(t, parisRoute(), st.mustGetRoute(ctx, t, st.other.ID, foreign.ID))

		_, err = st.store.Workouts().DeleteRoutesByUser(ctx, st.user.ID, 0)
		require.ErrorIs(t, err, store.ErrInvalid)
	})
}
