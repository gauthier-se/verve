-- name: InsertAggregateDirtyRange :exec
INSERT INTO aggregate_dirty_ranges (user_id, type_id, from_at, to_at)
VALUES (?, ?, ?, ?);

-- name: ListUserAggregateDirtyRanges :many
SELECT *
FROM aggregate_dirty_ranges
WHERE user_id = ?
ORDER BY type_id, from_at, id;

-- name: DeleteUserAggregateDirtyRanges :exec
DELETE
FROM aggregate_dirty_ranges
WHERE user_id = sqlc.arg(user_id)
  AND id IN (sqlc.slice(ids));

-- name: DeleteUserAggHourlyRange :exec
DELETE
FROM agg_hourly
WHERE user_id = ? AND type_id = ? AND bucket_start >= sqlc.arg(from_at) AND bucket_start < sqlc.arg(to_at);

-- name: InsertAggHourly :exec
INSERT INTO agg_hourly (user_id, type_id, bucket_start, sum, count, min, max, last, last_at, selection, source_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListUserAggHourly :many
SELECT *
FROM agg_hourly
WHERE user_id = ? AND type_id = ? AND bucket_start >= sqlc.arg(from_at) AND bucket_start < sqlc.arg(to_at)
ORDER BY bucket_start;

-- name: DeleteUserAggDailyRange :exec
DELETE
FROM agg_daily
WHERE user_id = ? AND type_id = ? AND bucket_start >= sqlc.arg(from_at) AND bucket_start < sqlc.arg(to_at);

-- name: InsertAggDaily :exec
INSERT INTO agg_daily (user_id, type_id, bucket_start, sum, count, min, max, last, last_at, selection, source_id, hours)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListUserAggDaily :many
SELECT *
FROM agg_daily
WHERE user_id = ? AND type_id = ? AND bucket_start >= sqlc.arg(from_at) AND bucket_start < sqlc.arg(to_at)
ORDER BY bucket_start;

-- name: DeleteAllUserAggHourly :exec
DELETE
FROM agg_hourly
WHERE user_id = ?;

-- name: DeleteAllUserAggDaily :exec
DELETE
FROM agg_daily
WHERE user_id = ?;

-- name: DeleteUserAggHourlyBatch :execrows
DELETE
FROM agg_hourly
WHERE (user_id, type_id, bucket_start) IN (SELECT old.user_id, old.type_id, old.bucket_start
                                           FROM agg_hourly AS old
                                           WHERE old.user_id = sqlc.arg(user_id)
                                           LIMIT sqlc.arg(batch_size));

-- name: DeleteUserAggDailyBatch :execrows
DELETE
FROM agg_daily
WHERE (user_id, type_id, bucket_start) IN (SELECT old.user_id, old.type_id, old.bucket_start
                                           FROM agg_daily AS old
                                           WHERE old.user_id = sqlc.arg(user_id)
                                           LIMIT sqlc.arg(batch_size));

-- name: DeleteUserAggregateDirtyRangesBatch :execrows
DELETE
FROM aggregate_dirty_ranges
WHERE id IN (SELECT old.id
             FROM aggregate_dirty_ranges AS old
             WHERE old.user_id = sqlc.arg(user_id)
             LIMIT sqlc.arg(batch_size));

-- name: InsertUserAggregateDirtyRangesFromStats :exec
-- A range of each type of a user, from the first start of its samples to the last start widened by
-- the longest duration, so that it holds the end of the last sample.
INSERT INTO aggregate_dirty_ranges (user_id, type_id, from_at, to_at)
SELECT st.user_id, st.type_id, st.first_at, st.last_at + st.longest
FROM sample_stats AS st
WHERE st.user_id = ?
ORDER BY st.type_id;
