-- +goose Up
-- cancel_requested is 1 once a cancellation of the job was requested. A running job stays running
-- until its handler has stopped.
ALTER TABLE jobs ADD COLUMN cancel_requested INTEGER NOT NULL DEFAULT 0;
