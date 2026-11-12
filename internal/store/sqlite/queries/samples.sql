-- name: InsertSample :one
-- Returns no row when the user already has a sample of the natural key.
INSERT INTO samples (user_id, type_id, source_id, start_at, end_at, utc_offset, value, min, max, category, origin,
                     import_id, uuid, natural_key, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, natural_key) DO NOTHING
RETURNING id;

-- name: UpdateSampleIfChanged :execrows
UPDATE samples
SET value    = sqlc.arg(value),
    min      = sqlc.narg(min),
    max      = sqlc.narg(max),
    category = sqlc.arg(category)
WHERE user_id = sqlc.arg(user_id)
  AND natural_key = sqlc.arg(natural_key)
  AND (value IS NOT sqlc.arg(value)
    OR min IS NOT sqlc.narg(min)
    OR max IS NOT sqlc.narg(max)
    OR category IS NOT sqlc.arg(category));

-- name: ReplaceSample :execrows
UPDATE samples
SET value = ?,
    min   = ?,
    max   = ?
WHERE user_id = ? AND natural_key = ?;

-- name: AddSampleStats :exec
-- Counts inserted samples of a user and type, and widens the first and last start to theirs.
INSERT INTO sample_stats (user_id, type_id, first_at, last_at, count)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id, type_id) DO UPDATE
    SET first_at = min(first_at, excluded.first_at),
        last_at  = max(last_at, excluded.last_at),
        count    = count + excluded.count;

-- name: ListUserSampleStats :many
SELECT *
FROM sample_stats
WHERE user_id = ?
ORDER BY type_id;

-- name: ListUserSamples :many
-- The samples of a user and type whose start is in a range, oldest first, after a position: those
-- that start after it, or at the same time with a larger id. The index on (user_id, type_id,
-- start_at), which holds the id, gives them in order.
SELECT *
FROM samples
WHERE user_id = sqlc.arg(user_id)
  AND type_id = sqlc.arg(type_id)
  AND start_at >= sqlc.arg(from_at)
  AND start_at < sqlc.arg(to_at)
  AND start_at >= sqlc.arg(after_at)
  AND (start_at > sqlc.arg(after_at) OR id > sqlc.arg(after_id))
ORDER BY start_at, id
LIMIT sqlc.arg(max_samples);

-- name: DeleteUserSamples :execrows
DELETE
FROM samples
WHERE id IN (SELECT old.id
             FROM samples AS old
             WHERE old.user_id = sqlc.arg(user_id)
             LIMIT sqlc.arg(batch_size));

-- name: DeleteUserSampleStats :execrows
DELETE
FROM sample_stats
WHERE sample_stats.user_id = sqlc.arg(user_id)
  AND sample_stats.type_id IN (SELECT old.type_id
                               FROM sample_stats AS old
                               WHERE old.user_id = sqlc.arg(user_id)
                               LIMIT sqlc.arg(batch_size));
