-- +goose Up
-- The workouts of each user (Storage and aggregates FR-4), values in canonical units. The caller
-- computes the natural key, from the origin and external id or from the activity type, source,
-- start and end (Deduplication and source priority FR-1). The import id references no table, so
-- that purging import reports leaves workouts alone.
CREATE TABLE workouts (
    id             INTEGER PRIMARY KEY,
    user_id        INTEGER NOT NULL,
    source_id      INTEGER NOT NULL,
    origin         TEXT    NOT NULL CHECK (origin IN ('xml', 'hae', 'native', 'cli')),
    external_id    TEXT    NOT NULL,
    activity       TEXT    NOT NULL CHECK (activity <> ''), -- key of the activity type catalog
    start_at       INTEGER NOT NULL, -- Unix milliseconds, UTC
    end_at         INTEGER NOT NULL CHECK (end_at >= start_at), -- Unix milliseconds, UTC
    utc_offset     INTEGER NOT NULL, -- seconds east of UTC
    duration       REAL    NOT NULL CHECK (duration >= 0), -- s, pauses excluded
    distance       REAL, -- m
    active_energy  REAL, -- kcal
    total_energy   REAL, -- kcal
    elevation_gain REAL, -- m
    elevation_loss REAL, -- m
    avg_heart_rate REAL, -- count/min
    max_heart_rate REAL, -- count/min
    indoor         INTEGER CHECK (indoor IN (0, 1)),
    location_type  TEXT    NOT NULL CHECK (location_type IN ('unknown', 'indoor', 'outdoor', 'pool', 'open_water')),
    -- A JSON array of {"type", "start", "end"}, times in Unix milliseconds, UTC.
    events         TEXT    NOT NULL CHECK (json_valid(events) AND json_type(events) = 'array'),
    metadata       TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    import_id      INTEGER NOT NULL,
    natural_key    BLOB    NOT NULL CHECK (length(natural_key) > 0),
    FOREIGN KEY (user_id, source_id) REFERENCES sources (user_id, id) ON DELETE CASCADE,
    UNIQUE (user_id, natural_key)
) STRICT;

CREATE INDEX workouts_user_start ON workouts (user_id, start_at);
CREATE INDEX workouts_user_source ON workouts (user_id, source_id);
