-- name: GetInstanceSetting :one
SELECT key, value, updated_at
FROM instance_settings
WHERE key = ?;

-- name: ListInstanceSettings :many
SELECT key, value, updated_at
FROM instance_settings
ORDER BY key;

-- name: SetInstanceSetting :exec
INSERT INTO instance_settings (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: DeleteInstanceSetting :execrows
DELETE FROM instance_settings
WHERE key = ?;
