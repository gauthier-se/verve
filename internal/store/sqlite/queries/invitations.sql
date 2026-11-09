-- name: CreateInvitation :one
INSERT INTO invitations (kind, token_hash, username, email, role, user_id, expires_at, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetInvitationByTokenHash :one
SELECT *
FROM invitations
WHERE token_hash = ?;

-- name: MarkInvitationUsed :execrows
UPDATE invitations
SET used_at = sqlc.arg(used_at),
    user_id = sqlc.arg(user_id)
WHERE id = sqlc.arg(id)
  AND used_at IS NULL;

-- name: DeleteUnusedResets :exec
DELETE
FROM invitations
WHERE kind = 'reset'
  AND user_id = ?
  AND used_at IS NULL;
