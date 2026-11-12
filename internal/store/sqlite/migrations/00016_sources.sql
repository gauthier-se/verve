-- +goose Up
-- The sources of each user (Deduplication and source priority FR-2): the devices and apps that
-- produced data, identified by name and device model, empty when unknown. The other details are
-- those of the latest record that gave them. The unique key (user_id, id) lets samples and workouts
-- reference (user_id, source_id), so that a row can never point at the source of another user.
CREATE TABLE sources (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL CHECK (name <> ''),
    device_model     TEXT    NOT NULL DEFAULT '',
    display_name     TEXT    NOT NULL CHECK (display_name <> ''),
    bundle_id        TEXT    NOT NULL,
    device_name      TEXT    NOT NULL,
    hardware_version TEXT    NOT NULL,
    software_version TEXT    NOT NULL,
    kind             TEXT    NOT NULL CHECK (kind IN ('watch', 'phone', 'scale', 'ring', 'app', 'aggregated', 'unknown')),
    kind_set_by_user INTEGER NOT NULL CHECK (kind_set_by_user IN (0, 1)),
    first_seen_at    INTEGER NOT NULL, -- Unix milliseconds, UTC
    last_seen_at     INTEGER NOT NULL, -- Unix milliseconds, UTC
    UNIQUE (user_id, name, device_model),
    UNIQUE (user_id, id)
) STRICT;

-- The aliases of merged sources (FR-3): an identity, matched as those of sources, whose records
-- land in the source of source_id. No identity is both an alias and a source of the same user.
CREATE TABLE source_aliases (
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL CHECK (name <> ''),
    device_model TEXT    NOT NULL DEFAULT '',
    source_id    INTEGER NOT NULL,
    PRIMARY KEY (user_id, name, device_model),
    FOREIGN KEY (user_id, source_id) REFERENCES sources (user_id, id) ON DELETE CASCADE
) STRICT;

CREATE INDEX source_aliases_source_id ON source_aliases (user_id, source_id);
