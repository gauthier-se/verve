-- +goose Up
-- The longest duration of the samples of each user and type, in milliseconds, so that the samples
-- that overlap a range are read from the index on samples (user_id, type_id, start_at), starting
-- that long before the range (Storage and aggregates, Data and API notes). Inserts widen it, and
-- removals leave it as an upper bound.
ALTER TABLE sample_stats ADD COLUMN longest INTEGER NOT NULL DEFAULT 0 CHECK (longest >= 0);

UPDATE sample_stats
SET longest = (SELECT coalesce(max(s.end_at - s.start_at), 0)
               FROM samples AS s
               WHERE s.user_id = sample_stats.user_id
                 AND s.type_id = sample_stats.type_id);

-- The hourly and daily aggregates of each user and type, after source selection (Storage and
-- aggregates FR-7), keyed by the UTC start of their local hour or day (ADR 0012). The selected
-- source is empty for combined and mixed rows. It references no source: a merge deletes sources,
-- and recomputes the aggregates that held them.
CREATE TABLE agg_hourly (
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type_id      INTEGER NOT NULL REFERENCES metric_types (id),
    bucket_start INTEGER NOT NULL, -- Unix milliseconds, UTC
    sum          REAL    NOT NULL,
    count        INTEGER NOT NULL CHECK (count > 0),
    min          REAL,
    max          REAL,
    last         REAL,
    last_at      INTEGER, -- Unix milliseconds, UTC
    selection    TEXT    NOT NULL CHECK (selection IN ('source', 'combined', 'mixed')),
    source_id    INTEGER CHECK ((selection = 'source') = (source_id IS NOT NULL)),
    PRIMARY KEY (user_id, type_id, bucket_start)
) STRICT, WITHOUT ROWID;

CREATE TABLE agg_daily (
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type_id      INTEGER NOT NULL REFERENCES metric_types (id),
    bucket_start INTEGER NOT NULL, -- Unix milliseconds, UTC
    sum          REAL    NOT NULL,
    count        INTEGER NOT NULL CHECK (count > 0),
    min          REAL,
    max          REAL,
    last         REAL,
    last_at      INTEGER, -- Unix milliseconds, UTC
    selection    TEXT    NOT NULL CHECK (selection IN ('source', 'combined', 'mixed')),
    source_id    INTEGER CHECK ((selection = 'source') = (source_id IS NOT NULL)),
    hours        INTEGER NOT NULL CHECK (hours > 0),
    PRIMARY KEY (user_id, type_id, bucket_start)
) STRICT, WITHOUT ROWID;

-- The ranges of each user and type whose aggregates wait for their recomputation (Storage and
-- aggregates FR-9), from the first start to the last end of their samples.
CREATE TABLE aggregate_dirty_ranges (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type_id INTEGER NOT NULL REFERENCES metric_types (id),
    from_at INTEGER NOT NULL, -- Unix milliseconds, UTC
    to_at   INTEGER NOT NULL CHECK (to_at >= from_at), -- Unix milliseconds, UTC
    UNIQUE (user_id, type_id, from_at, id)
) STRICT;
