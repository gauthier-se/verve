-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, scopes, prefix, token_hash, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAPITokenByHash :one
SELECT sqlc.embed(api_tokens), sqlc.embed(users)
FROM api_tokens
JOIN users ON users.id = api_tokens.user_id
WHERE api_tokens.token_hash = ?;

-- name: ListUserAPITokens :many
SELECT *
FROM api_tokens
WHERE user_id = ?
ORDER BY created_at DESC, id DESC;

-- name: RevokeUserAPIToken :one
-- A revoked token keeps its first revocation time.
UPDATE api_tokens
SET revoked_at = coalesce(revoked_at, sqlc.arg(revoked_at))
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
RETURNING *;
