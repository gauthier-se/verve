-- name: CreateImport :one
INSERT INTO imports (user_id, origin, token_id, client, inbox_file, status, position, counters, type_counters, errors,
                     created_at)
VALUES (?, ?, ?, ?, ?, 'queued', 0, '{}', '{}', '[]', ?)
RETURNING *;

-- name: PurgeUserImports :exec
-- Removes the finished imports of a user that are not among their newest ones, by creation then id.
-- Queued and running imports count among the newest but are never removed.
DELETE
FROM imports AS old
WHERE old.user_id = sqlc.arg(user_id)
  AND old.status IN ('completed', 'failed', 'cancelled')
  AND old.id NOT IN (SELECT newest.id
                     FROM imports AS newest
                     WHERE newest.user_id = sqlc.arg(user_id)
                     ORDER BY newest.created_at DESC, newest.id DESC
                     LIMIT sqlc.arg(kept));

-- name: GetUserImport :one
SELECT *
FROM imports
WHERE user_id = ? AND id = ?;

-- name: ListUserImports :many
-- The imports of a user, newest first by creation, after a position: those created before it, or at
-- the same time with a smaller id. The position is always set, the first page starting after the
-- largest time and id, so that the index on user, creation and id bounds the scan.
SELECT *
FROM imports
WHERE user_id = sqlc.arg(user_id)
  AND created_at <= sqlc.arg(after_at)
  AND (created_at < sqlc.arg(after_at) OR id < sqlc.arg(after_id))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_imports);

-- name: StartUserImport :one
-- A resumed import, already running, keeps the time it first ran.
UPDATE imports
SET status     = 'running',
    started_at = coalesce(started_at, sqlc.arg(started_at))
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND status IN ('queued', 'running')
RETURNING *;

-- name: FinishUserImport :one
UPDATE imports
SET status      = sqlc.arg(status),
    finished_at = sqlc.arg(finished_at)
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND status IN ('queued', 'running')
RETURNING *;

-- name: SaveUserImportProgress :execrows
UPDATE imports
SET position      = sqlc.arg(position),
    counters      = sqlc.arg(counters),
    type_counters = sqlc.arg(type_counters),
    errors        = sqlc.arg(errors)
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND status = 'running';

-- name: SetUserImportClient :execrows
UPDATE imports
SET client = sqlc.arg(client)
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND status = 'running';

-- name: InboxFilesKept :many
-- The inbox files of the imports, except those of the imports that failed before a time.
SELECT CAST(inbox_file AS TEXT) AS inbox_file
FROM imports
WHERE inbox_file IS NOT NULL
  AND NOT (status = 'failed' AND finished_at < sqlc.arg(failed_since));

-- name: LastCompletedUserImports :many
SELECT origin, CAST(max(finished_at) AS INTEGER) AS finished_at
FROM imports
WHERE user_id = ?
  AND status = 'completed'
GROUP BY origin;

-- name: DeleteUserImports :execrows
DELETE
FROM imports
WHERE id IN (SELECT old.id
             FROM imports AS old
             WHERE old.user_id = sqlc.arg(user_id)
             LIMIT sqlc.arg(batch_size));
