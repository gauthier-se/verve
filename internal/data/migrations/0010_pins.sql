-- 0010_pins: a Pin is a Catalog Metric the Account keeps in the sidebar, beside
-- its Dashboards (CONTEXT.md).
--
-- A Pin is a shortcut to that Metric's page and deliberately nothing more: it
-- carries no Time range, no Baseline and no composition. A Pin holding a
-- persisted range would be a one-Panel Dashboard under another name, and would
-- immediately owe an answer to "why has my Pin no Baseline, no bucket override,
-- no second Metric" (ADR 0015, ADR 0020).
--
-- The Metric is a Catalog slug validated at the API boundary rather than a
-- foreign key: the Catalog is compiled into the binary, not a table (ADR 0002).
CREATE TABLE pins (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    metric      TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- One Pin per Metric per Account. The uniqueness carries the idempotency of
-- pinning rather than a read-then-write in the handler, so a double click cannot
-- race into two rows.
CREATE UNIQUE INDEX pins_account_metric ON pins (account_id, metric);

-- List an Account's Pins in sidebar order. position is written at insert and
-- never exposed; it is here from day one so that reordering later is a handler
-- change rather than a migration.
CREATE INDEX pins_account_position ON pins (account_id, position);
