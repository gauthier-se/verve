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
