-- 0003_states_sessions: the non-scalar ingestion families — States (sleep and
-- stand hours) and Sessions (workouts) with their GPX Routes (see CONTEXT.md and
-- ADR 0001 for the families, ADR 0004 for artifacts-as-files, ADR 0006 for
-- idempotency). Every row is owned by exactly one Account (ADR 0007).

-- A State is a categorical value that holds over an interval — a sleep stage or
-- a stand hour. kind groups the family of state ("sleep", "stand"); state_value
-- is the neutral phase within it ("asleep_rem", "in_bed", "stood"…), never an
-- Apple identifier. content_key is the dedup identity: a hash of
-- (kind, state_value, source, start, end), unique per account, making re-import
-- idempotent (ADR 0006).
CREATE TABLE states (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id),
    kind        TEXT NOT NULL,
    state_value TEXT NOT NULL,
    start_at    TEXT NOT NULL,
    end_at      TEXT NOT NULL,
    source      TEXT NOT NULL,
    content_key TEXT NOT NULL,
    UNIQUE (account_id, content_key)
) STRICT;

-- Primary read pattern: one kind's intervals for an Account over a time window
-- (e.g. a night's sleep stages), collapsed by duration_by_state.
CREATE INDEX states_account_kind_start ON states (account_id, kind, start_at);

-- A Session is a rich, bounded activity (a workout): an activity_type over an
-- interval, with duration and optional totals. duration is seconds,
-- total_distance km, total_energy kcal (canonical units). total_distance and
-- total_energy are nullable — a strength-training session has no distance.
-- content_key hashes (activity_type, source, start, end): a workout's stable
-- identity across re-exports (creationDate excluded, ADR 0006).
CREATE TABLE sessions (
    id             INTEGER PRIMARY KEY,
    account_id     INTEGER NOT NULL REFERENCES accounts(id),
    activity_type  TEXT NOT NULL,
    start_at       TEXT NOT NULL,
    end_at         TEXT NOT NULL,
    duration       REAL NOT NULL,
    total_distance REAL,
    total_energy   REAL,
    source         TEXT NOT NULL,
    content_key    TEXT NOT NULL,
    UNIQUE (account_id, content_key)
) STRICT;

CREATE INDEX sessions_account_start ON sessions (account_id, start_at);

-- A Route is a GPS track (GPX) attached to a Session. The .gpx is copied into
-- VERVE_DATA_DIR/artifacts/ as <content_key>.gpx and referenced by artifact,
-- never stored as a blob (ADR 0004). content_key is the sha256 of the file
-- contents, so the artifact is content-addressed and re-import is idempotent:
-- identical contents hash to the same name and the same deduped row (ADR 0006).
CREATE TABLE routes (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id),
    session_id  INTEGER NOT NULL REFERENCES sessions(id),
    artifact    TEXT NOT NULL,
    start_at    TEXT NOT NULL,
    end_at      TEXT NOT NULL,
    source      TEXT NOT NULL,
    content_key TEXT NOT NULL,
    UNIQUE (account_id, content_key)
) STRICT;

CREATE INDEX routes_session ON routes (session_id);
