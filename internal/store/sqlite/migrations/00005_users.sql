-- +goose Up
-- The accounts. AUTOINCREMENT never reuses an id, so that a new user cannot inherit the audit events
-- of a deleted one. username_fold and email_fold hold the case-folded values, computed by the store:
-- they carry the uniqueness and the lookups because NOCASE and lower() only fold ASCII. The checks
-- keep a login from being both a username and an email.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL CHECK (instr(username, '@') = 0),
    username_fold TEXT    NOT NULL UNIQUE,
    email         TEXT    CHECK (instr(email, '@') > 0),
    email_fold    TEXT,
    password_hash TEXT,             -- NULL for the external logins of M5
    role          TEXT    NOT NULL, -- admin or member
    status        TEXT    NOT NULL, -- active, disabled or deleting
    created_at    INTEGER NOT NULL, -- Unix milliseconds, UTC
    last_login_at INTEGER           -- Unix milliseconds, UTC
) STRICT;

CREATE UNIQUE INDEX users_email_fold ON users (email_fold) WHERE email_fold IS NOT NULL;
