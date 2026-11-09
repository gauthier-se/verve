-- name: CreateSession :one
INSERT INTO sessions (id_hash, public_id, user_id, method, created_at, last_seen_at, expires_at, client_ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSession :one
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.id_hash = ?;

-- name: TouchSession :one
UPDATE sessions
SET last_seen_at = ?,
    expires_at = ?
WHERE id_hash = ?
RETURNING *;

-- name: DeleteSession :execrows
DELETE
FROM sessions
WHERE id_hash = ?;

-- name: PurgeExpiredSessions :exec
-- The table holds a few sessions per user, so that the purge reads it whole rather than keep an
-- index on expires_at, which every touch would rewrite.
DELETE
FROM sessions
WHERE expires_at < sqlc.arg(now);

-- name: DeleteOtherSessions :execrows
DELETE
FROM sessions
WHERE user_id = sqlc.arg(user_id)
  AND id_hash <> sqlc.arg(keep_id_hash);

-- name: ListUserSessions :many
SELECT *
FROM sessions
WHERE user_id = ?
ORDER BY last_seen_at DESC, created_at DESC, public_id;

-- name: DeleteUserSessionByPublicID :execrows
DELETE
FROM sessions
WHERE user_id = sqlc.arg(user_id)
  AND public_id = sqlc.arg(public_id);
