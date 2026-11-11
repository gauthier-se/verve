-- name: GetMetricType :one
SELECT *
FROM metric_types
WHERE id = ?;

-- name: ListMetricTypes :many
SELECT *
FROM metric_types
ORDER BY id;

-- name: SyncMetricType :exec
-- Writes a type of the catalog, matched by identifier, which keeps the id and the creation time.
INSERT INTO metric_types (key, identifier, kind, unit, aggregation, display_group, curated, created_at)
VALUES (?, ?, ?, ?, ?, ?, 1, ?)
ON CONFLICT (identifier) DO UPDATE
SET key           = excluded.key,
    kind          = excluded.kind,
    unit          = excluded.unit,
    aggregation   = excluded.aggregation,
    display_group = excluded.display_group,
    curated       = 1;

-- name: SetMetricTypeKey :exec
UPDATE metric_types
SET key = ?
WHERE id = ?;

-- name: UncurateMetricType :exec
UPDATE metric_types
SET curated = 0
WHERE id = ?;

-- name: GetMetricTypeByKey :one
SELECT *
FROM metric_types
WHERE key = ?;

-- name: GetMetricTypeByIdentifier :one
SELECT *
FROM metric_types
WHERE identifier = ?;

-- name: ListMetricTypeKeys :many
SELECT key
FROM metric_types;

-- name: CreateUncuratedMetricType :exec
INSERT INTO metric_types (key, identifier, kind, unit, aggregation, display_group, curated, created_at)
VALUES (?, ?, ?, ?, 'discrete', 'other', 0, ?)
ON CONFLICT (identifier) DO NOTHING;
