-- Exclusions: a standing refusal that a stretch of a Metric is Verve's to hold.
-- It does two things and the pair is what makes it honest: no Connector may write
-- what it names, and what was already stored under it was deleted the moment it
-- was created. It is never a read-time predicate, so no query outside the import
-- path and the purge ever consults this table (ADR 0033).
--
-- This is the tombstone table ADR 0022 named when it refused to make imported
-- Measurements deletable: without it a delete drops the row's content key and the
-- next import restores the row verbatim.
CREATE TABLE exclusions (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    metric     TEXT NOT NULL,
    -- Inclusive YYYY-MM-DD bounds, '' for unbounded. Empty string rather than
    -- NULL so the UNIQUE below actually catches a duplicate: SQLite treats NULLs
    -- as distinct, so two "the whole of body_fat_percentage" rows would both be
    -- admitted and the second would report a purge the first had already done.
    starts_on  TEXT NOT NULL DEFAULT '',
    ends_on    TEXT NOT NULL DEFAULT '',
    -- How many Measurements the purge removed when this row was written. A
    -- historical figure, not a running count: the History reads it to say what
    -- this decision cost, and nothing recomputes it.
    purged     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (metric <> ''),
    CHECK (ends_on = '' OR starts_on = '' OR ends_on >= starts_on),
    UNIQUE (account_id, metric, starts_on, ends_on)
) STRICT;

-- Every read is "this Account's whole set", loaded once per import and once per
-- page, so the index is for the Account scope rather than for a span lookup.
CREATE INDEX exclusions_account ON exclusions (account_id, metric);
