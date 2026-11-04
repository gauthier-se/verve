-- name: AppendAuditEvent :one
INSERT INTO audit_events (occurred_at, actor_id, subject_id, type, client_ip, user_agent, payload, last_at,
                          aggregation_key)
VALUES (sqlc.arg(occurred_at), sqlc.arg(actor_id), sqlc.arg(subject_id), sqlc.arg(type), sqlc.arg(client_ip),
        sqlc.arg(user_agent), sqlc.arg(payload), sqlc.arg(occurred_at), sqlc.arg(aggregation_key))
RETURNING *;

-- name: GetAuditBurst :one
-- The events of a type and key since a time, and the latest row that holds them. A row counts all
-- of its events while its last one is recent, so that a long burst stays on one row.
SELECT CAST(coalesce(sum(count), 0) AS INTEGER) AS events,
       CAST(coalesce(max(id), 0) AS INTEGER)    AS latest_id
FROM audit_events
WHERE type = sqlc.arg(type)
  AND aggregation_key = sqlc.arg(aggregation_key)
  AND last_at > sqlc.arg(since);

-- name: AggregateAuditEvent :one
UPDATE audit_events
SET count = count + 1,
    last_at = sqlc.arg(last_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetAuditEvent :one
SELECT *
FROM audit_events
WHERE id = ?;

-- name: ListAuditEvents :many
-- The events that match the filters, newest first, after a position: those that occurred before
-- it, or at the same time with a smaller id. The position and the time range are always set, the
-- first page starting after the largest time and id, so that the index on occurred_at bounds the
-- scan. A row is in the time range while one of its events is: its first one before to_at, and
-- its last one at or after from_at.
SELECT *
FROM audit_events
WHERE occurred_at <= sqlc.arg(after_at)
  AND (occurred_at < sqlc.arg(after_at) OR id < sqlc.arg(after_id))
  AND occurred_at < sqlc.arg(to_at)
  AND last_at >= sqlc.arg(from_at)
  AND (actor_id = sqlc.narg(user_id) OR subject_id = sqlc.narg(user_id) OR sqlc.narg(user_id) IS NULL)
  AND (actor_id = sqlc.narg(actor_id) OR sqlc.narg(actor_id) IS NULL)
  AND (subject_id = sqlc.narg(subject_id) OR sqlc.narg(subject_id) IS NULL)
  AND (type = sqlc.narg(type) OR sqlc.narg(type) IS NULL)
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg(max_events);

-- name: PurgeAuditEvents :execrows
-- The oldest events whose last occurrence is before a cutoff, at most a batch size. An event never
-- occurred after its last occurrence, so that the condition on occurred_at, which is indexed, finds
-- them without reading the rest of the table.
DELETE
FROM audit_events
WHERE id IN (SELECT old.id
             FROM audit_events AS old
             WHERE old.occurred_at < sqlc.arg(cutoff)
               AND old.last_at < sqlc.arg(cutoff)
             ORDER BY old.occurred_at
             LIMIT sqlc.arg(batch_size));
