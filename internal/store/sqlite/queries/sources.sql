-- name: GetUserSource :one
SELECT *
FROM sources
WHERE user_id = ? AND id = ?;

-- name: ListUserSources :many
SELECT *
FROM sources
WHERE user_id = ?
ORDER BY id;

-- name: CreateSource :one
INSERT INTO sources (user_id, name, device_model, display_name, bundle_id, device_name, hardware_version,
                     software_version, kind, kind_set_by_user, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
RETURNING *;

-- name: ListUserSourcesByName :many
SELECT *
FROM sources
WHERE user_id = ? AND name = ?
ORDER BY id;

-- name: UpdateSource :one
UPDATE sources
SET device_model     = ?,
    bundle_id        = ?,
    device_name      = ?,
    hardware_version = ?,
    software_version = ?,
    kind             = ?,
    last_seen_at     = ?
WHERE user_id = ? AND id = ?
RETURNING *;

-- name: GetUserSourceByIdentity :one
SELECT *
FROM sources
WHERE user_id = ? AND name = ? AND device_model = ?;

-- name: ListUserSourceAliasesByName :many
SELECT *
FROM source_aliases
WHERE user_id = ? AND name = ?;

-- name: CreateSourceAlias :exec
INSERT INTO source_aliases (user_id, name, device_model, source_id)
VALUES (?, ?, ?, ?);

-- name: SetSourceAliasModel :exec
UPDATE source_aliases
SET device_model = sqlc.arg(device_model)
WHERE user_id = sqlc.arg(user_id) AND name = sqlc.arg(name) AND device_model = '';

-- name: RenameUserSource :one
UPDATE sources
SET display_name = ?
WHERE user_id = ? AND id = ?
RETURNING *;

-- name: SetUserSourceKind :one
UPDATE sources
SET kind             = ?,
    kind_set_by_user = 1
WHERE user_id = ? AND id = ?
RETURNING *;
