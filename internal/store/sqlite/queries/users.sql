-- name: CreateUser :one
INSERT INTO users (username, username_fold, email, email_fold, password_hash, role, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUser :one
SELECT *
FROM users
WHERE id = ?;

-- name: GetUserByLogin :one
SELECT *
FROM users
WHERE username_fold = sqlc.arg(login_fold)
   OR email_fold = sqlc.arg(login_fold);

-- name: SetUserPasswordHash :execrows
UPDATE users
SET password_hash = ?
WHERE id = ?;

-- name: SetUserLastLoginAt :one
UPDATE users
SET last_login_at = ?
WHERE id = ?
RETURNING *;

-- name: CountUsers :one
SELECT count(*)
FROM users;

-- name: ListUsers :many
-- The users newest first by creation, after a position: those created before it, or at the same
-- time with a smaller id. The position is always set, the first page starting after the largest
-- time and id, so that the index on created_at bounds the scan.
SELECT *
FROM users
WHERE created_at <= sqlc.arg(after_at)
  AND (created_at < sqlc.arg(after_at) OR id < sqlc.arg(after_id))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_users);

-- name: SetUserRole :one
UPDATE users
SET role = ?
WHERE id = ?
RETURNING *;

-- name: SetUserStatus :one
UPDATE users
SET status = ?
WHERE id = ?
RETURNING *;

-- name: CountActiveAdmins :one
SELECT count(*)
FROM users
WHERE role = 'admin'
  AND status = 'active';

-- name: DeleteUser :execrows
DELETE
FROM users
WHERE id = ?;
