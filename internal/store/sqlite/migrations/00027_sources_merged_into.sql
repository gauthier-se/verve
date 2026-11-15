-- +goose Up
-- The target of the merge in progress of a source, empty otherwise (Deduplication and source
-- priority FR-3). It references no source: SQLite cannot add a foreign key on (user_id, id), and the
-- target of a merge in progress is never deleted, since it cannot be merged meanwhile. A source
-- merged into another is replaced by an alias once its records are moved.
ALTER TABLE sources ADD COLUMN merged_into INTEGER CHECK (merged_into IS NULL OR merged_into <> id);
