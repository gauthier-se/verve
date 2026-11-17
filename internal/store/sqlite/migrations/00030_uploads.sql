-- +goose Up
-- The incomplete resumable uploads of each user (XML import FR-2, FR-3, FR-4). The id is random and
-- names the file of the upload in /data/uploads. file_name is the name that the client gave, kept
-- for display only. A completed upload leaves this table for the inbox and an import.
CREATE TABLE uploads (
    id         TEXT    PRIMARY KEY CHECK (id <> ''),
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    file_name  TEXT,             -- NULL for none
    length     INTEGER NOT NULL CHECK (length >= 1),
    "offset"   INTEGER NOT NULL CHECK ("offset" >= 0 AND "offset" <= length),
    created_at INTEGER NOT NULL, -- Unix milliseconds, UTC
    expires_at INTEGER NOT NULL  -- Unix milliseconds, UTC
) STRICT;

CREATE INDEX uploads_user ON uploads (user_id);
CREATE INDEX uploads_expires ON uploads (expires_at);
