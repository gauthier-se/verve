-- name: CreateUpload :one
INSERT INTO uploads (id, user_id, file_name, length, "offset", created_at, expires_at)
VALUES (?, ?, ?, ?, 0, ?, ?)
RETURNING *;

-- name: GetUserUpload :one
SELECT *
FROM uploads
WHERE user_id = ?
  AND id = ?;

-- name: AdvanceUserUpload :one
UPDATE uploads
SET "offset"   = sqlc.arg(to_offset),
    expires_at = sqlc.arg(expires_at)
WHERE user_id = sqlc.arg(user_id)
  AND id = sqlc.arg(id)
  AND "offset" = sqlc.arg(from_offset)
RETURNING *;

-- name: DeleteUserUpload :execrows
DELETE
FROM uploads
WHERE user_id = ?
  AND id = ?;

-- name: DeleteExpiredUploads :many
DELETE
FROM uploads
WHERE expires_at <= ?
RETURNING id;

-- name: ListUploadIDs :many
SELECT id
FROM uploads
ORDER BY id;
