-- +goose Up
-- The quantity and category samples of each user (Storage and aggregates FR-3). The caller computes
-- the natural key, derived from the uuid when the sample has one, so that one unique index dedupes
-- both cases (Deduplication and source priority FR-1). The import id references no table, so that
-- purging import reports leaves samples alone. The id has no AUTOINCREMENT, which would write
-- sqlite_sequence on every insert of the largest table.
CREATE TABLE samples (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    type_id     INTEGER NOT NULL REFERENCES metric_types (id),
    source_id   INTEGER NOT NULL,
    start_at    INTEGER NOT NULL, -- Unix milliseconds, UTC
    end_at      INTEGER NOT NULL CHECK (end_at >= start_at), -- Unix milliseconds, UTC
    utc_offset  INTEGER NOT NULL, -- seconds east of UTC
    value       REAL    NOT NULL,
    min         REAL,
    max         REAL,
    category    TEXT    NOT NULL,
    origin      TEXT    NOT NULL CHECK (origin IN ('xml', 'hae', 'native', 'cli')),
    import_id   INTEGER NOT NULL,
    uuid        TEXT    NOT NULL,
    natural_key BLOB    NOT NULL CHECK (length(natural_key) > 0),
    metadata    TEXT CHECK (metadata IS NULL OR json_valid(metadata)),
    FOREIGN KEY (user_id, source_id) REFERENCES sources (user_id, id) ON DELETE CASCADE
) STRICT;

CREATE INDEX samples_user_type_start ON samples (user_id, type_id, start_at);
CREATE UNIQUE INDEX samples_user_natural_key ON samples (user_id, natural_key);
CREATE INDEX samples_user_source_type ON samples (user_id, source_id, type_id);

-- The data availability of each user and type: the first and last start and the count of their
-- samples, maintained in the transaction of every write that adds, removes or moves samples. A row
-- whose count drops to zero is deleted.
CREATE TABLE sample_stats (
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type_id  INTEGER NOT NULL REFERENCES metric_types (id),
    first_at INTEGER NOT NULL, -- Unix milliseconds, UTC
    last_at  INTEGER NOT NULL, -- Unix milliseconds, UTC
    count    INTEGER NOT NULL CHECK (count > 0),
    PRIMARY KEY (user_id, type_id)
) STRICT, WITHOUT ROWID;
