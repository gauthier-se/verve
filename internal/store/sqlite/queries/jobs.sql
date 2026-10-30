-- name: EnqueueJob :one
INSERT INTO jobs (kind, payload, status, run_at, user_id, serial_key, created_at)
VALUES (?, ?, 'queued', ?, ?, ?, ?)
RETURNING *;

-- name: GetJob :one
SELECT *
FROM jobs
WHERE id = ?;

-- name: AbandonJobs :exec
-- lost_leases does not count the lease that has just expired yet, as ClaimJob would.
UPDATE jobs
SET status = 'failed',
    lost_leases = lost_leases + 1,
    locked_until = NULL,
    last_error = sqlc.arg(last_error),
    finished_at = sqlc.arg(now)
WHERE status = 'running'
  AND locked_until <= sqlc.arg(now)
  AND lost_leases + 1 >= sqlc.arg(max_lost_leases);

-- name: ClaimJob :one
-- A queued job waits for any running job of its serial key, even one whose lease has expired: that
-- job's process can still renew its lease until another claim changes its claims value.
UPDATE jobs
SET lost_leases = CASE WHEN status = 'running' THEN lost_leases + 1 ELSE lost_leases END,
    status = 'running',
    claims = claims + 1,
    locked_until = sqlc.arg(locked_until)
WHERE id = (
    SELECT candidate.id
    FROM jobs AS candidate
    WHERE (
        (candidate.status = 'queued' AND candidate.run_at <= sqlc.arg(now))
        OR (candidate.status = 'running' AND candidate.locked_until <= sqlc.arg(now))
    )
    AND NOT EXISTS (
        SELECT 1
        FROM jobs AS holder
        WHERE holder.status = 'running'
          AND holder.serial_key = candidate.serial_key
          AND holder.id <> candidate.id
          AND (candidate.status = 'queued' OR holder.locked_until > sqlc.arg(now))
    )
    ORDER BY candidate.run_at, candidate.id
    LIMIT 1
)
RETURNING *;

-- name: HeartbeatJob :execrows
UPDATE jobs
SET locked_until = ?
WHERE id = ? AND claims = ? AND status = 'running';

-- name: CompleteJob :execrows
UPDATE jobs
SET status = 'completed',
    locked_until = NULL,
    finished_at = ?
WHERE id = ? AND claims = ? AND status = 'running';

-- name: FailJob :execrows
UPDATE jobs
SET status = 'failed',
    attempts = attempts + 1,
    locked_until = NULL,
    last_error = ?,
    finished_at = ?
WHERE id = ? AND claims = ? AND status = 'running';

-- name: ReleaseJob :execrows
UPDATE jobs
SET status = 'queued',
    locked_until = NULL
WHERE id = ? AND claims = ? AND status = 'running';
