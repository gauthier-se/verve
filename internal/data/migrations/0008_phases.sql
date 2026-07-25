-- 0008_phases: a Phase is a bounded stretch over which the Account pursues one
-- Target rate — a cut, a bulk, a maintenance stretch (CONTEXT.md, ADR 0023).
--
-- Phases are kept as a history and never overwritten, so each is judged against the
-- window it actually ran over and "was I doing what I meant to be doing?" stays
-- answerable about the past. Opening a new Phase closes the current one.
--
-- rate_pct_per_week is signed: a bulk is positive and a cut negative. It is a rate
-- rather than a calorie figure because the same deficit is trivial at one body size
-- and dangerous at another; the calorie target is derived from it on read.
CREATE TABLE phases (
    id                INTEGER PRIMARY KEY,
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    rate_pct_per_week REAL NOT NULL,
    started_at        TEXT NOT NULL,
    ended_at          TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- At most one open Phase per Account, enforced by the database rather than by
-- application logic: Open() closes the current Phase and inserts the new one in a
-- transaction, and this index is what makes a concurrent second opener fail loudly
-- instead of leaving two Phases silently open.
CREATE UNIQUE INDEX phases_one_open_per_account
    ON phases (account_id) WHERE ended_at IS NULL;

-- The history listing: an Account's Phases newest first.
CREATE INDEX phases_account_started ON phases (account_id, started_at DESC);
