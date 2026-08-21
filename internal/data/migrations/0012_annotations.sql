-- 0012_annotations: dated notes on the time axis, and the Dashboard's toggle.
--
-- An Annotation is context the Account writes about a day or a span of days,
-- an illness, a trip, a change of program, so a curve can be read against what
-- was happening. It carries no value, no unit and no Metric: it is not a
-- Measurement and never enters the Catalog. It belongs to the Account and not to
-- a Dashboard, because "flu, 12-19 March" is a fact about the year and must show
-- wherever that fortnight is on screen.
--
-- Dates are day-granular YYYY-MM-DD, like every other bound Verve stores, since
-- the whole read path stops at the day (ADR 0012). ends_on is NULL for a single
-- day; a span ends on or after it starts.
CREATE TABLE annotations (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    body        TEXT,
    starts_on   TEXT NOT NULL,
    ends_on     TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    CHECK (label <> ''),
    CHECK (ends_on IS NULL OR ends_on >= starts_on)
) STRICT;

-- Every read is "this Account's Annotations overlapping this window", scanned
-- from the start day.
CREATE INDEX annotations_account_start ON annotations (account_id, starts_on);

-- Whether a Dashboard draws the markers is a property of its time axis, so it
-- sits beside baseline_rule (0006) rather than with the Annotation itself: the
-- note belongs to the Account, showing it belongs to the Dashboard. On by
-- default, including for every dashboard that predates this migration, a
-- feature nobody can see is a feature nobody enables.
ALTER TABLE dashboards ADD COLUMN annotations INTEGER NOT NULL DEFAULT 1;
