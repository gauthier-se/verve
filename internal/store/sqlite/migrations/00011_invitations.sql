-- +goose Up
-- The single-use links: invitations, and the password reset links to come. token_hash is the
-- SHA-256 hash of the token of the link, which is never stored. user_id is the user that an
-- invitation created once used, or the user of a reset link. A link outlives the admin who created
-- it, so that deleting them does not revoke the pending invitations.
CREATE TABLE invitations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT    NOT NULL, -- invite or reset
    token_hash BLOB    NOT NULL UNIQUE,
    username   TEXT,             -- NULL for a reset link
    email      TEXT,
    role       TEXT,             -- NULL for a reset link
    user_id    INTEGER REFERENCES users (id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL, -- Unix milliseconds, UTC
    used_at    INTEGER,          -- Unix milliseconds, UTC
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL  -- Unix milliseconds, UTC
) STRICT;
