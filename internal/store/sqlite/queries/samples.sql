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
-- Counts inserted samples of a user and type, and widens the first and last start and the longest
-- duration to theirs.
INSERT INTO sample_stats (user_id, type_id, first_at, last_at, count, longest)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, type_id) DO UPDATE
    SET first_at = min(first_at, excluded.first_at),
        last_at  = max(last_at, excluded.last_at),
        count    = count + excluded.count,
        longest  = max(longest, excluded.longest);

-- name: ListUserSampleStats :many
SELECT *
FROM sample_stats
WHERE user_id = ?
ORDER BY type_id;

-- name: CountUsersSamples :many
-- The primary key of sample_stats, on user then type, bounds the scan to the users asked for.
SELECT user_id, CAST(sum(count) AS INTEGER) AS samples
FROM sample_stats
WHERE user_id IN (sqlc.slice(user_ids))
GROUP BY user_id;

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

-- name: DeleteUserSampleByNaturalKey :one
DELETE
FROM samples
WHERE user_id = ? AND natural_key = ?
RETURNING *;

-- name: DeleteLastSampleStats :execrows
-- Removes the data availability of a user and type whose last sample was removed.
DELETE
FROM sample_stats
WHERE user_id = ? AND type_id = ? AND count <= 1;

-- name: RemoveSampleStats :exec
-- Counts one removed sample of a user and type, and narrows the first and last start to those of
-- the samples left, which the index on (user_id, type_id, start_at) gives.
UPDATE sample_stats
SET count    = count - 1,
    first_at = (SELECT min(s.start_at) FROM samples AS s WHERE s.user_id = sample_stats.user_id AND s.type_id = sample_stats.type_id),
    last_at  = (SELECT max(s.start_at) FROM samples AS s WHERE s.user_id = sample_stats.user_id AND s.type_id = sample_stats.type_id)
WHERE sample_stats.user_id = sqlc.arg(user_id)
  AND sample_stats.type_id = sqlc.arg(type_id);

-- name: ListUserSamplesOverlapping :many
-- The samples of a user and type that start in a range or overlap it, oldest first. A sample that
-- overlaps the range starts at most the longest duration of sample_stats before it, which bounds the
-- scan of the index on (user_id, type_id, start_at).
SELECT samples.*
FROM samples
WHERE samples.user_id = sqlc.arg(user_id)
  AND samples.type_id = sqlc.arg(type_id)
  AND samples.start_at >= sqlc.arg(from_at) - coalesce((SELECT st.longest
                                                         FROM sample_stats AS st
                                                         WHERE st.user_id = sqlc.arg(user_id)
                                                           AND st.type_id = sqlc.arg(type_id)), 0)
  AND samples.start_at < sqlc.arg(to_at)
  AND (samples.start_at >= sqlc.arg(from_at) OR samples.end_at > sqlc.arg(from_at))
ORDER BY samples.start_at, samples.id;
