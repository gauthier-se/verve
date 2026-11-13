-- +goose Up
-- The imports of each user and their reports (Ingestion pipeline FR-3, FR-5, FR-9, FR-11). token_id
-- references no table, so that revoking a token keeps the history. position is defined by the
-- adapter and opaque to the store. counters holds the total counters, type_counters those of each
-- type key and of the workouts, and errors at most 100 sample error messages. The id has
-- AUTOINCREMENT, so that the id of a purged import, which samples and workouts keep, is never reused.
CREATE TABLE imports (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    origin        TEXT    NOT NULL CHECK (origin IN ('xml', 'hae', 'native', 'cli')),
    token_id      INTEGER,          -- NULL for none
    client        TEXT CHECK (client IS NULL OR (json_valid(client) AND json_type(client) = 'object')),
    status        TEXT    NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    position      INTEGER NOT NULL CHECK (position >= 0),
    -- A JSON object of the counters of FR-3 by their name, a counter at zero left out.
    counters      TEXT    NOT NULL CHECK (json_valid(counters) AND json_type(counters) = 'object'),
    -- A JSON object {"types": {<type key>: <counters>}, "workouts": <counters>}, empty ones left out.
    type_counters TEXT    NOT NULL CHECK (json_valid(type_counters) AND json_type(type_counters) = 'object'),
    errors        TEXT    NOT NULL CHECK (json_valid(errors) AND json_type(errors) = 'array'),
    created_at    INTEGER NOT NULL, -- Unix milliseconds, UTC
    started_at    INTEGER,          -- Unix milliseconds, UTC
    finished_at   INTEGER           -- Unix milliseconds, UTC
) STRICT;

CREATE INDEX imports_user_created ON imports (user_id, created_at, id);
