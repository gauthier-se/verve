-- 0002_measurements: the import core — Measurements, the Unmapped bin, and
-- Import runs (see CONTEXT.md and ADRs 0002, 0003, 0006, 0011).
--
-- Every row is owned by exactly one Account (ADR 0007); Accounts never see each
-- other's data, so all reads filter by account_id and dedup identity is scoped
-- per account.

-- An Import is one run of a Connector over one Source file, recorded with its
-- counts of Measurements added vs skipped and Unmapped records kept.
CREATE TABLE imports (
    id             INTEGER PRIMARY KEY,
    account_id     INTEGER NOT NULL REFERENCES accounts(id),
    source_file    TEXT NOT NULL,
    added_count    INTEGER NOT NULL,
    skipped_count  INTEGER NOT NULL,
    unmapped_count INTEGER NOT NULL,
    imported_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- A Measurement is a scalar value of a canonical Metric at (or over) a point in
-- time. value is normalized to the Metric's canonical unit; original_unit keeps
-- what the Source reported (metadata). start_at/end_at are RFC 3339 (UTC) so
-- they index and bucket cleanly. content_key is the dedup identity: a hash of
-- (metric, source, start, end, value, unit) with creationDate excluded, unique
-- per account, making re-import idempotent (ADR 0006).
CREATE TABLE measurements (
    id            INTEGER PRIMARY KEY,
    account_id    INTEGER NOT NULL REFERENCES accounts(id),
    metric        TEXT NOT NULL,
    value         REAL NOT NULL,
    original_unit TEXT NOT NULL,
    start_at      TEXT NOT NULL,
    end_at        TEXT NOT NULL,
    source        TEXT NOT NULL,
    content_key   TEXT NOT NULL,
    UNIQUE (account_id, content_key)
) STRICT;

-- Primary read pattern: one Metric's series for an Account over a time window,
-- collapsed into buckets.
CREATE INDEX measurements_account_metric_start
    ON measurements (account_id, metric, start_at);

-- The Unmapped bin: incoming records whose type the Connector cannot map to a
-- Catalog Metric (ADR 0002). Kept and inspectable, never discarded — no source
-- data is lost even when the Catalog does not yet cover it. value is stored as
-- the raw source text (it may be non-numeric, e.g. a sleep category). Deduped
-- per account by the same content-key mechanism so re-import stays idempotent.
CREATE TABLE unmapped_records (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id),
    source_type TEXT NOT NULL,
    value       TEXT,
    unit        TEXT,
    start_at    TEXT NOT NULL,
    end_at      TEXT NOT NULL,
    source      TEXT NOT NULL,
    content_key TEXT NOT NULL,
    UNIQUE (account_id, content_key)
) STRICT;

CREATE INDEX unmapped_account_type
    ON unmapped_records (account_id, source_type);
