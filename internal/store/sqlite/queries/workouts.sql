-- name: InsertWorkout :one
-- Returns no row when the user already has a workout of the natural key.
INSERT INTO workouts (user_id, source_id, origin, external_id, activity, start_at, end_at, utc_offset, duration,
                      distance, active_energy, total_energy, elevation_gain, elevation_loss, avg_heart_rate,
                      max_heart_rate, indoor, location_type, events, metadata, import_id, natural_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, natural_key) DO NOTHING
RETURNING id;

-- name: UpdateWorkout :one
-- Replaces every field of the workout of a natural key but its id, user and natural key, when one of
-- them differs, the import id aside. Returns no row when none differs.
UPDATE workouts
SET source_id      = sqlc.arg(source_id),
    origin         = sqlc.arg(origin),
    external_id    = sqlc.arg(external_id),
    activity       = sqlc.arg(activity),
    start_at       = sqlc.arg(start_at),
    end_at         = sqlc.arg(end_at),
    utc_offset     = sqlc.arg(utc_offset),
    duration       = sqlc.arg(duration),
    distance       = sqlc.narg(distance),
    active_energy  = sqlc.narg(active_energy),
    total_energy   = sqlc.narg(total_energy),
    elevation_gain = sqlc.narg(elevation_gain),
    elevation_loss = sqlc.narg(elevation_loss),
    avg_heart_rate = sqlc.narg(avg_heart_rate),
    max_heart_rate = sqlc.narg(max_heart_rate),
    indoor         = sqlc.narg(indoor),
    location_type  = sqlc.arg(location_type),
    events         = sqlc.arg(events),
    metadata       = sqlc.narg(metadata),
    import_id      = sqlc.arg(import_id)
WHERE user_id = sqlc.arg(user_id)
  AND natural_key = sqlc.arg(natural_key)
  AND (source_id IS NOT sqlc.arg(source_id)
    OR origin IS NOT sqlc.arg(origin)
    OR external_id IS NOT sqlc.arg(external_id)
    OR activity IS NOT sqlc.arg(activity)
    OR start_at IS NOT sqlc.arg(start_at)
    OR end_at IS NOT sqlc.arg(end_at)
    OR utc_offset IS NOT sqlc.arg(utc_offset)
    OR duration IS NOT sqlc.arg(duration)
    OR distance IS NOT sqlc.narg(distance)
    OR active_energy IS NOT sqlc.narg(active_energy)
    OR total_energy IS NOT sqlc.narg(total_energy)
    OR elevation_gain IS NOT sqlc.narg(elevation_gain)
    OR elevation_loss IS NOT sqlc.narg(elevation_loss)
    OR avg_heart_rate IS NOT sqlc.narg(avg_heart_rate)
    OR max_heart_rate IS NOT sqlc.narg(max_heart_rate)
    OR indoor IS NOT sqlc.narg(indoor)
    OR location_type IS NOT sqlc.arg(location_type)
    OR events IS NOT sqlc.arg(events)
    OR metadata IS NOT sqlc.narg(metadata))
RETURNING id;

-- name: GetUserWorkoutIDByNaturalKey :one
SELECT id
FROM workouts
WHERE user_id = ? AND natural_key = ?;

-- name: GetUserWorkout :one
SELECT *
FROM workouts
WHERE user_id = ? AND id = ?;

-- name: ListUserWorkouts :many
-- The workouts of a user whose start is in a range, most recent first, after a position: those that
-- start before it, or at the same time with a smaller id. The position is always set, the first
-- page starting after the largest time and id. The duplicates are left out unless asked for. The
-- index on (user_id, start_at), which holds the id, gives them in order.
SELECT *
FROM workouts
WHERE user_id = sqlc.arg(user_id)
  AND start_at >= sqlc.arg(from_at)
  AND start_at < sqlc.arg(to_at)
  AND start_at <= sqlc.arg(after_at)
  AND (start_at < sqlc.arg(after_at) OR id < sqlc.arg(after_id))
  AND (CAST(sqlc.arg(with_duplicates) AS INTEGER) OR duplicate_of IS NULL)
ORDER BY start_at DESC, id DESC
LIMIT sqlc.arg(max_workouts);

-- name: DeleteUserWorkouts :execrows
DELETE
FROM workouts
WHERE id IN (SELECT old.id
             FROM workouts AS old
             WHERE old.user_id = sqlc.arg(user_id)
             LIMIT sqlc.arg(batch_size));

-- name: DeleteUserWorkoutByNaturalKey :one
-- Removes the workout of a natural key of a user, and its route by its foreign key.
DELETE
FROM workouts
WHERE user_id = ? AND natural_key = ?
RETURNING *;

-- name: ListUserWorkoutsStarting :many
-- Every workout of a user whose start is in a range, duplicates included, from the index on
-- (user_id, start_at).
SELECT *
FROM workouts
WHERE user_id = sqlc.arg(user_id)
  AND start_at >= sqlc.arg(from_at)
  AND start_at < sqlc.arg(to_at)
ORDER BY start_at, id;

-- name: ListUserWorkoutDuplicatesOf :many
-- The duplicates of kept workouts of a user. The condition on NULL lets the partial index on
-- (user_id, duplicate_of) answer.
SELECT *
FROM workouts
WHERE user_id = sqlc.arg(user_id)
  AND duplicate_of IS NOT NULL
  AND duplicate_of IN (sqlc.slice(kept_ids))
ORDER BY id;

-- name: SetUserWorkoutDuplicateOf :execrows
UPDATE workouts
SET duplicate_of = sqlc.narg(duplicate_of)
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id);

-- name: ListUserWorkoutsOfSource :many
-- The workouts of a source of a user, duplicates included, from the index on (user_id, source_id),
-- which holds the id.
SELECT *
FROM workouts
WHERE user_id = sqlc.arg(user_id)
  AND source_id = sqlc.arg(source_id)
ORDER BY id
LIMIT sqlc.arg(max_workouts);

-- name: MoveUserWorkout :one
-- Moves a workout of a source of a user to another source with a new natural key. Returns no row
-- when the workout is not of the source, and when the user has the new key stored, the workout then
-- left as it is.
UPDATE OR IGNORE workouts
SET source_id   = sqlc.arg(to_id),
    natural_key = sqlc.arg(natural_key)
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND source_id = sqlc.arg(from_id)
RETURNING id;

-- name: DeleteUserWorkoutOfSource :one
-- Removes a workout of a source of a user, and its route by its foreign key.
DELETE
FROM workouts
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND source_id = sqlc.arg(source_id)
RETURNING *;
