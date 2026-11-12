-- name: UpsertWorkoutRoute :execrows
-- Stores the route of a workout of a user, replacing the one it had. Writes no row when the user
-- has no such workout. The WHERE clause keeps ON CONFLICT from parsing as a join constraint.
INSERT INTO workout_routes (workout_id, user_id, encoded, min_lat, min_lon, max_lat, max_lon, distance, point_count,
                            polyline)
SELECT w.id,
       w.user_id,
       CAST(sqlc.arg(encoded) AS BLOB),
       CAST(sqlc.arg(min_lat) AS REAL),
       CAST(sqlc.arg(min_lon) AS REAL),
       CAST(sqlc.arg(max_lat) AS REAL),
       CAST(sqlc.arg(max_lon) AS REAL),
       CAST(sqlc.arg(distance) AS REAL),
       CAST(sqlc.arg(point_count) AS INTEGER),
       CAST(sqlc.arg(polyline) AS TEXT)
FROM workouts AS w
WHERE w.id = sqlc.arg(workout_id)
  AND w.user_id = sqlc.arg(user_id)
ON CONFLICT (workout_id) DO UPDATE SET encoded     = excluded.encoded,
                                       min_lat     = excluded.min_lat,
                                       min_lon     = excluded.min_lon,
                                       max_lat     = excluded.max_lat,
                                       max_lon     = excluded.max_lon,
                                       distance    = excluded.distance,
                                       point_count = excluded.point_count,
                                       polyline    = excluded.polyline;

-- name: GetUserWorkoutRoute :one
SELECT *
FROM workout_routes
WHERE user_id = ? AND workout_id = ?;

-- name: ListUserRouteThumbnails :many
SELECT workout_id, min_lat, min_lon, max_lat, max_lon, polyline
FROM workout_routes
WHERE user_id = sqlc.arg(user_id)
  AND workout_id IN (sqlc.slice(workout_ids))
ORDER BY workout_id;

-- name: DeleteUserWorkoutRoutes :execrows
DELETE
FROM workout_routes
WHERE workout_id IN (SELECT old.workout_id
                     FROM workout_routes AS old
                     WHERE old.user_id = sqlc.arg(user_id)
                     LIMIT sqlc.arg(batch_size));
