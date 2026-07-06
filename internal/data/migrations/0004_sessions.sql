-- 0004_sessions: login sessions for local auth (ADR 0008).
--
-- A login session is a server-side record backing an opaque cookie. It is
-- deliberately NOT the workout Session family (see CONTEXT.md / table `sessions`
-- above is workouts) — this table is `auth_sessions` to keep the two apart.
--
-- We store only token_hash (SHA-256 hex of the cookie's opaque token), never the
-- token itself: a leak of this table cannot reconstruct a live cookie. Keeping
-- the record server-side (rather than a stateless signed token) makes logout an
-- immediate, real revocation and needs no long-lived signing key. Rows are
-- owned by exactly one Account (ADR 0007); ON DELETE CASCADE drops an Account's
-- sessions with it.
CREATE TABLE auth_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL
) STRICT;

-- Sweep expired sessions and list an Account's sessions by expiry.
CREATE INDEX auth_sessions_account ON auth_sessions (account_id);
CREATE INDEX auth_sessions_expires ON auth_sessions (expires_at);
