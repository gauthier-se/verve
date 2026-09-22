-- 0017_source_freshness: an index for reading each Source's last datum (CONTEXT.md:
-- Freshness, ADR 0045).
--
-- Now opens on every visit and names the last day each Source recorded something.
-- measurements_account_metric_start leads on (account_id, metric), so a GROUP BY
-- source cannot seek and reads every row the Account holds: a second on a real
-- history of 1.7 million rows, paid by the first screen the app shows.
--
-- Leading on (account_id, source, start_at) turns the question into a loose index
-- scan, one seek to find each next Source and one to find its last start_at, which
-- is a dozen seeks for a dozen Sources: 5 ms on the same history. The price is the
-- index itself, about 17% of the database, paid once at import.
--
-- 0015 declined a full index of this shape because the Manual list only reads a
-- handful of rows. This read is the one that needs every Source, which is the case
-- 0015 left open. States are not indexed here: sleep is a few thousand rows per
-- year and states_account_kind_start already narrows them to one kind.
CREATE INDEX measurements_account_source_start
    ON measurements (account_id, source, start_at);
