-- +goose Up
-- The kept workout of a duplicate workout, empty for a kept one (Deduplication and source priority
-- FR-11). It references no workout: the transaction that removes a kept workout evaluates its
-- duplicates again, which it finds by the index. The workouts stored before are evaluated when a
-- workout that they overlap is next written.
ALTER TABLE workouts ADD COLUMN duplicate_of INTEGER CHECK (duplicate_of IS NULL OR duplicate_of <> id);

CREATE INDEX workouts_user_duplicate_of ON workouts (user_id, duplicate_of) WHERE duplicate_of IS NOT NULL;
