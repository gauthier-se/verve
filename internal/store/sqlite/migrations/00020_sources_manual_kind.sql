-- +goose NO TRANSACTION
-- +goose Up
-- Adds the manual kind of the entries made by hand in the Health app (Deduplication and source
-- priority FR-2). SQLite cannot change a CHECK constraint, so the table is rebuilt as the SQLite
-- documentation of ALTER TABLE describes. Foreign keys are off during the rebuild, since dropping
-- the old table with them on would delete the aliases, samples and workouts of every source, and
-- the pragma has no effect inside a transaction: this migration runs outside the transaction of
-- goose and opens its own, on the single writer connection. The references of the other tables
-- name sources, so they point at the new table once it is renamed. The sequence of the ids is
-- carried over, so that the id of a deleted source is never reused. A failed statement leaves the
-- transaction open with foreign keys off, but migrations run after a pre-migration backup on a
-- connection opened for them, whose closing rolls the transaction back.
PRAGMA foreign_keys = OFF;

BEGIN IMMEDIATE;

CREATE TABLE sources_new (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id          INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL CHECK (name <> ''),
    device_model     TEXT    NOT NULL DEFAULT '',
    display_name     TEXT    NOT NULL CHECK (display_name <> ''),
    bundle_id        TEXT    NOT NULL,
    device_name      TEXT    NOT NULL,
    manufacturer     TEXT    NOT NULL,
    hardware_version TEXT    NOT NULL,
    software_version TEXT    NOT NULL,
    kind             TEXT    NOT NULL CHECK (kind IN ('watch', 'phone', 'scale', 'ring', 'app', 'manual', 'aggregated', 'unknown')),
    kind_set_by_user INTEGER NOT NULL CHECK (kind_set_by_user IN (0, 1)),
    first_seen_at    INTEGER NOT NULL, -- Unix milliseconds, UTC
    last_seen_at     INTEGER NOT NULL, -- Unix milliseconds, UTC
    UNIQUE (user_id, name, device_model),
    UNIQUE (user_id, id)
) STRICT;

INSERT INTO sources_new (id, user_id, name, device_model, display_name, bundle_id, device_name, manufacturer,
                         hardware_version, software_version, kind, kind_set_by_user, first_seen_at, last_seen_at)
SELECT id, user_id, name, device_model, display_name, bundle_id, device_name, manufacturer,
       hardware_version, software_version, kind, kind_set_by_user, first_seen_at, last_seen_at
FROM sources;

DELETE FROM sqlite_sequence WHERE name = 'sources_new';

INSERT INTO sqlite_sequence (name, seq)
SELECT 'sources_new', seq FROM sqlite_sequence WHERE name = 'sources';

DROP TABLE sources;

ALTER TABLE sources_new RENAME TO sources;

COMMIT;

PRAGMA foreign_keys = ON;
