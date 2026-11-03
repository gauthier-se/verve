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
