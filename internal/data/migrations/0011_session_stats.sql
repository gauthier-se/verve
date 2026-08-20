-- 0011_session_stats: the summary figures a workout carries, one row per
-- (Session, Metric, aggregate). See CONTEXT.md (Session stat) and ADR 0011
-- (capture broadly, so anything can be shown retroactively over data already
-- stored).
--
-- Apple reports a workout's statistics as <WorkoutStatistics> elements, each
-- with a type and up to four aggregates: sum, average, minimum, maximum. The
-- import used to keep two of them, the distance sum and the active-energy sum,
-- and drop heart rate, step count and the rest on the floor, which made ADR
-- 0011 promise false for this family. All four are kept now: an average heart
-- rate and a maximum heart rate are different answers, and collapsing them to
-- one loses the one people actually look at.
--
-- metric is a canonical Catalog slug (heart_rate, never HKQuantityType…) and
-- value is in that Metric's canonical unit, so a Session stat and a Measurement
-- of the same quantity are directly comparable (ADR 0002).
--
-- No account_id: a stat is reachable only through the Session that owns it, and
-- that Session is owned by exactly one Account (ADR 0007). ON DELETE CASCADE
-- means a stat cannot outlive its Session.
--
-- The primary key is the dedup identity, so re-importing an export upserts a
-- stat rather than duplicating it: a corrected export corrects the stored
-- figure. For a Session, idempotent means convergent, not inert (ADR 0006).
CREATE TABLE session_stats (
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    metric     TEXT NOT NULL,
    stat       TEXT NOT NULL,
    value      REAL NOT NULL,
    PRIMARY KEY (session_id, metric, stat)
) STRICT;
