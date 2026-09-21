-- 0016_goals: a Goal is a bound the owner declares on a Metric, judged against its
-- day bucket (CONTEXT.md, ADR 0044). The owner supplies the direction; Verve counts.
--
-- Goals are kept as a history and never overwritten, as Phases are: raising 7 000
-- steps to 7 500 must not rewrite March's count, so each day is judged against the
-- Goal in force on that day. Setting a new Goal closes the open one.
--
-- The bounds are dates and not instants, unlike a Phase's. A Goal judges days, and an
-- instant would only raise the question of which Goal applies to the day it changed
-- at 14:00. started_on is inclusive, ended_on exclusive, both YYYY-MM-DD, so lexical
-- comparison is chronological and a closed Goal ends exactly where the next begins.
CREATE TABLE goals (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    metric     TEXT NOT NULL,
    direction  TEXT NOT NULL,
    -- In the Metric's canonical unit. Signed: a derived Metric can be negative
    -- (calorie_balance at most -300).
    value      REAL NOT NULL,
    started_on TEXT NOT NULL,
    ended_on   TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (metric <> ''),
    CHECK (direction IN ('at_least', 'at_most')),
    CHECK (ended_on IS NULL OR ended_on > started_on)
) STRICT;

-- At most one open Goal per Metric of an Account, enforced by the database for the
-- reason phases_one_open_per_account is: Open() closes and inserts in a transaction,
-- and this index makes a concurrent second opener fail loudly.
CREATE UNIQUE INDEX goals_one_open_per_metric
    ON goals (account_id, metric) WHERE ended_on IS NULL;

-- The segments in force over a window, and the history on the Metric page.
CREATE INDEX goals_account_metric_started ON goals (account_id, metric, started_on);
