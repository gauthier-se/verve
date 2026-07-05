-- 0001_init: accounts table.
--
-- An Account is a person who logs into Verve and owns their own data (see
-- CONTEXT.md / ADR 0007). Every future family table carries an owning account_id.
-- password_hash is nullable for now: local auth (argon2id) lands in slice 05.
-- The date_of_birth / biological_sex / blood_type columns hold Apple's static
-- `Me` profile, used later to normalize age-based metrics; all nullable.

CREATE TABLE accounts (
    id             INTEGER PRIMARY KEY,
    email          TEXT NOT NULL UNIQUE,
    password_hash  TEXT,
    date_of_birth  TEXT,
    biological_sex TEXT,
    blood_type     TEXT,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
