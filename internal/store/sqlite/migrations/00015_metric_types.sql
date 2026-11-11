-- +goose Up
-- The metric types (Storage and aggregates FR-1, FR-2): the types of the catalog, synchronized at
-- startup, and the uncurated types created on first sight. The table is instance-wide, without
-- user_id, and holds no health data. Ids never change, since samples reference them. The category
-- values stay in the catalog.
CREATE TABLE metric_types (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    key           TEXT    NOT NULL UNIQUE,
    identifier    TEXT    NOT NULL UNIQUE CHECK (identifier <> ''),
    kind          TEXT    NOT NULL CHECK (kind IN ('quantity', 'category')),
    unit          TEXT    NOT NULL,
    aggregation   TEXT    NOT NULL CHECK (aggregation IN ('cumulative', 'discrete')),
    display_group TEXT    NOT NULL,
    curated       INTEGER NOT NULL CHECK (curated IN (0, 1)),
    created_at    INTEGER NOT NULL -- Unix milliseconds, UTC
) STRICT;
