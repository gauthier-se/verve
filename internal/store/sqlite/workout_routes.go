package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

func (r workouts) SetRoute(ctx context.Context, userID, workoutID int64, route store.WorkoutRoute) error {
	if err := checkRoute(route); err != nil {
		return fmt.Errorf("set route of workout %d of user %d: %w", workoutID, userID, err)
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		stored, err := q.UpsertWorkoutRoute(ctx, sqlcgen.UpsertWorkoutRouteParams{
			Encoded:    route.Encoded,
			MinLat:     route.Box.MinLat,
			MinLon:     route.Box.MinLon,
			MaxLat:     route.Box.MaxLat,
			MaxLon:     route.Box.MaxLon,
			Distance:   route.Distance,
			PointCount: int64(route.PointCount),
			Polyline:   route.Polyline,
			WorkoutID:  workoutID,
			UserID:     userID,
		})
		if err != nil {
			return constraintError(err)
		}

		if stored == 0 {
			return store.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("set route of workout %d of user %d: %w", workoutID, userID, err)
	}

	return nil
}

func (r workouts) GetRoute(ctx context.Context, userID, workoutID int64) (store.WorkoutRoute, error) {
	row, err := r.q.read().GetUserWorkoutRoute(ctx, sqlcgen.GetUserWorkoutRouteParams{UserID: userID, WorkoutID: workoutID})
	if errors.Is(err, sql.ErrNoRows) {
		return store.WorkoutRoute{}, fmt.Errorf("get route of workout %d of user %d: %w", workoutID, userID, store.ErrNotFound)
	}

	if err != nil {
		return store.WorkoutRoute{}, fmt.Errorf("get route of workout %d of user %d: %w", workoutID, userID, err)
	}

	return store.WorkoutRoute{
		Encoded:    row.Encoded,
		Box:        store.RouteBox{MinLat: row.MinLat, MinLon: row.MinLon, MaxLat: row.MaxLat, MaxLon: row.MaxLon},
		Distance:   row.Distance,
		PointCount: int(row.PointCount),
		Polyline:   row.Polyline,
	}, nil
}

func (r workouts) Thumbnails(ctx context.Context, userID int64, workoutIDs []int64) ([]store.RouteThumbnail, error) {
	if len(workoutIDs) == 0 {
		return nil, nil
	}

	rows, err := r.q.read().ListUserRouteThumbnails(ctx, sqlcgen.ListUserRouteThumbnailsParams{
		UserID:     userID,
		WorkoutIds: workoutIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list route thumbnails of user %d: %w", userID, err)
	}

	thumbnails := make([]store.RouteThumbnail, 0, len(rows))
	for _, row := range rows {
		thumbnails = append(thumbnails, store.RouteThumbnail{
			WorkoutID: row.WorkoutID,
			Box:       store.RouteBox{MinLat: row.MinLat, MinLon: row.MinLon, MaxLat: row.MaxLat, MaxLon: row.MaxLon},
			Polyline:  row.Polyline,
		})
	}

	return thumbnails, nil
}

func (r workouts) DeleteRoutesByUser(ctx context.Context, userID int64, limit int) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("delete routes of user %d: limit %d below 1: %w", userID, limit, store.ErrInvalid)
	}

	var deleted int64

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		deleted, err = q.DeleteUserWorkoutRoutes(ctx, sqlcgen.DeleteUserWorkoutRoutesParams{UserID: userID, BatchSize: int64(limit)})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete routes of user %d: %w", userID, err)
	}

	return deleted, nil
}

// checkRoute returns ErrInvalid when route has a field that the store refuses.
func checkRoute(route store.WorkoutRoute) error {
	box := route.Box

	// Written so that NaN, which compares false, is refused too.
	switch {
	case len(route.Encoded) == 0:
		return fmt.Errorf("empty encoding: %w", store.ErrInvalid)
	case route.PointCount < 1:
		return fmt.Errorf("point count %d: %w", route.PointCount, store.ErrInvalid)
	case !(route.Distance >= 0) || math.IsInf(route.Distance, 1):
		return fmt.Errorf("distance %v: %w", route.Distance, store.ErrInvalid)
	case !(-90 <= box.MinLat && box.MinLat <= box.MaxLat && box.MaxLat <= 90):
		return fmt.Errorf("latitudes %v to %v: %w", box.MinLat, box.MaxLat, store.ErrInvalid)
	case !(-180 <= box.MinLon && box.MinLon <= box.MaxLon && box.MaxLon <= 180):
		return fmt.Errorf("longitudes %v to %v: %w", box.MinLon, box.MaxLon, store.ErrInvalid)
	}

	return nil
}
