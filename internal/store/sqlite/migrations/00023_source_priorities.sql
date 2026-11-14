-- +goose Up
-- The source priorities that each user set per metric type (Deduplication and source priority FR-4
-- to FR-7): a row per type holds the combine flag, and an entry per source its position, among the
-- ordered or among the ignored sources. A type has no row until the user sets its priorities: the
-- defaults are resolved at read time. The entries reference (user_id, id) of sources, so that an
-- entry never names the source of another user, and a deleted source, as a merge deletes, leaves
-- every list without code.
CREATE TABLE source_priorities (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type_id INTEGER NOT NULL REFERENCES metric_types (id),
    combine INTEGER NOT NULL CHECK (combine IN (0, 1)),
    PRIMARY KEY (user_id, type_id)
) STRICT;

CREATE TABLE source_priority_entries (
    user_id   INTEGER NOT NULL,
    type_id   INTEGER NOT NULL,
    source_id INTEGER NOT NULL,
    ignored   INTEGER NOT NULL CHECK (ignored IN (0, 1)),
    position  INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (user_id, type_id, source_id),
    UNIQUE (user_id, type_id, ignored, position),
    FOREIGN KEY (user_id, type_id) REFERENCES source_priorities (user_id, type_id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, source_id) REFERENCES sources (user_id, id) ON DELETE CASCADE
) STRICT;

-- Deleting a source finds its entries through this index.
CREATE INDEX source_priority_entries_source_id ON source_priority_entries (user_id, source_id);
