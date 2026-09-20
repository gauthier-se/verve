-- 0015_manual_entries_by_day: an index for reading Manual entries by the day they
-- fall on (ADR 0022, ADR 0043).
--
-- measurements_account_metric_start leads on (account_id, metric), so a read that
-- filters by source and date without naming a Metric cannot seek: it scans every
-- row the Account holds. On a real history that is 1.6 million rows and two and a
-- half seconds, paid by the Data page's entry list already and by every Day page
-- load once one exists.
--
-- The index is partial, on the reserved Manual source alone, because that is the
-- whole of the question: Manual rows are a handful per Account and imported rows
-- are never read this way. A full (account_id, source, start_at) index would cover
-- 1.6 million rows to serve a few dozen.
--
-- The literal must stay in step with catalog.SourceManual, which is the same
-- constant the purge predicate and the overlay already spell in SQL.
CREATE INDEX measurements_manual_account_start
    ON measurements (account_id, start_at)
    WHERE source = 'Manual';
