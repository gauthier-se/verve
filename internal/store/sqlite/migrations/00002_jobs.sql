-- +goose Up
-- Background jobs. AUTOINCREMENT never reuses an id, so that a purge cannot give the id and claims
-- of a finished job to a new one. claims is the fencing token of a claim. user_id has no foreign
-- key: the rows of a deleted account are removed when the account is deleted.
CREATE TABLE jobs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT    NOT NULL,
    payload      TEXT    NOT NULL, -- JSON
    status       TEXT    NOT NULL, -- queued, running, completed, failed or cancelled
    attempts     INTEGER NOT NULL DEFAULT 0,
    claims       INTEGER NOT NULL DEFAULT 0,
    lost_leases  INTEGER NOT NULL DEFAULT 0,
    run_at       INTEGER NOT NULL, -- Unix milliseconds, UTC
    locked_until INTEGER,          -- Unix milliseconds, UTC
    user_id      INTEGER,
    serial_key   TEXT,
    last_error   TEXT,
    created_at   INTEGER NOT NULL, -- Unix milliseconds, UTC
    finished_at  INTEGER           -- Unix milliseconds, UTC
) STRICT;

CREATE INDEX jobs_status_run_at ON jobs (status, run_at, id);

CREATE INDEX jobs_running_serial_key ON jobs (serial_key) WHERE status = 'running';
