-- name: AppendAuditEvent :one
INSERT INTO audit_events (occurred_at, actor_id, subject_id, type, client_ip, user_agent, payload, last_at)
VALUES (sqlc.arg(occurred_at), sqlc.arg(actor_id), sqlc.arg(subject_id), sqlc.arg(type), sqlc.arg(client_ip),
        sqlc.arg(user_agent), sqlc.arg(payload), sqlc.arg(occurred_at))
RETURNING *;

-- name: GetAuditEvent :one
SELECT *
FROM audit_events
WHERE id = ?;
