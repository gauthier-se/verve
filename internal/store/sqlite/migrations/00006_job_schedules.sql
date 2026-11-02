-- +goose Up
-- The next run of the recurring jobs, which are declared in code. A row survives restarts, so that
-- a restart neither repeats a run nor forgets one missed during downtime.
CREATE TABLE job_schedules (
    name     TEXT    NOT NULL PRIMARY KEY,
    next_run INTEGER NOT NULL -- Unix milliseconds, UTC
) STRICT;
