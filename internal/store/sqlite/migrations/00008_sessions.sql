-- +goose Up
-- The server-side sessions (ADR 0005). id_hash is the SHA-256 hash of the session identifier, which
-- is never stored. public_id names the session on the sessions page: it is random, so that nothing
-- derived from the identifier reaches a URL.
CREATE TABLE sessions (
    id_hash      BLOB    NOT NULL PRIMARY KEY,
    public_id    TEXT    NOT NULL UNIQUE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    method       TEXT    NOT NULL, -- password
    created_at   INTEGER NOT NULL, -- Unix milliseconds, UTC
    last_seen_at INTEGER NOT NULL, -- Unix milliseconds, UTC
    expires_at   INTEGER NOT NULL, -- Unix milliseconds, UTC
    client_ip    TEXT,
    user_agent   TEXT
) STRICT, WITHOUT ROWID;

CREATE INDEX sessions_user_id ON sessions (user_id);
