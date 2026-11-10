-- +goose Up
-- The API tokens of machine clients (API tokens FR-1, FR-3). token_hash is the SHA-256 hash of the
-- token, which is never stored, and prefix its start, shown to tell tokens apart. A revoked token
-- stays, so that a request with it is told apart from one with an unknown token (FR-5), until the
-- deletion of its user.
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL CHECK (name <> ''),
    scopes       TEXT    NOT NULL, -- ingest and read, separated by spaces
    prefix       TEXT    NOT NULL,
    token_hash   BLOB    NOT NULL UNIQUE,
    expires_at   INTEGER,          -- Unix milliseconds, UTC, NULL for none
    created_at   INTEGER NOT NULL, -- Unix milliseconds, UTC
    last_used_at INTEGER,          -- Unix milliseconds, UTC
    last_used_ip TEXT,
    revoked_at   INTEGER           -- Unix milliseconds, UTC
) STRICT;

CREATE INDEX api_tokens_user_id ON api_tokens (user_id);
