-- name: GetPreferences :one
-- Reads from users, so that a user without preferences is told from a missing user.
SELECT user_preferences.timezone
FROM users
LEFT JOIN user_preferences ON user_preferences.user_id = users.id
WHERE users.id = ?;

-- name: SetTimezone :one
INSERT INTO user_preferences (user_id, timezone)
VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE SET timezone = excluded.timezone
RETURNING *;
