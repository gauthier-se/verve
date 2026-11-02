-- +goose Up
-- The audit log. AUTOINCREMENT never reuses an id, so that a purge cannot give the id of an old
-- event to a new one. actor_id and subject_id have no foreign key: events outlive their users.
CREATE TABLE audit_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at INTEGER NOT NULL, -- Unix milliseconds, UTC
    actor_id    INTEGER,          -- NULL for the system
    subject_id  INTEGER,
    type        TEXT    NOT NULL,
    client_ip   TEXT,
    user_agent  TEXT,
    payload     TEXT    NOT NULL, -- JSON
    count       INTEGER NOT NULL DEFAULT 1,
    last_at     INTEGER NOT NULL  -- Unix milliseconds, UTC
) STRICT;

CREATE INDEX audit_events_occurred_at ON audit_events (occurred_at);

CREATE INDEX audit_events_actor_id_occurred_at ON audit_events (actor_id, occurred_at);

CREATE INDEX audit_events_subject_id_occurred_at ON audit_events (subject_id, occurred_at);
