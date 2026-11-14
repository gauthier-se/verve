-- name: GetSourcePriorities :many
-- One row per entry, or a single row without entry for priorities that list no source, so that a
-- single statement reads the row of the type and its entries.
SELECT source_priorities.combine, source_priority_entries.source_id, source_priority_entries.ignored
FROM source_priorities
LEFT JOIN source_priority_entries
    ON source_priority_entries.user_id = source_priorities.user_id
    AND source_priority_entries.type_id = source_priorities.type_id
WHERE source_priorities.user_id = ? AND source_priorities.type_id = ?
ORDER BY source_priority_entries.ignored, source_priority_entries.position;

-- name: SetSourcePriorities :exec
INSERT INTO source_priorities (user_id, type_id, combine)
VALUES (?, ?, ?)
ON CONFLICT (user_id, type_id) DO UPDATE SET combine = excluded.combine;

-- name: DeleteSourcePriorityEntries :exec
DELETE FROM source_priority_entries
WHERE user_id = ? AND type_id = ?;

-- name: CreateSourcePriorityEntry :exec
INSERT INTO source_priority_entries (user_id, type_id, source_id, ignored, position)
VALUES (?, ?, ?, ?, ?);
