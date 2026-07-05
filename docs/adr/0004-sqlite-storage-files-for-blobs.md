# SQLite storage, large artifacts as referenced files

## Context

Verve stores ~1.6M rows per export (mostly scalar Measurements), growing over
time, queried with time-based aggregations. Target is a self-hosted, single-user
homelab deployment where simplicity is an explicit goal.

## Decision

Use **SQLite** as the storage engine, via a pure-Go driver
(`modernc.org/sqlite`, no CGo). Large binary artifacts — GPX workout routes, ECG
waveforms — are stored as **files on disk, referenced by a row**, not as blobs in
the database. Write reasonably portable SQL to keep a later migration to
Postgres/DuckDB possible if scale or multi-user needs arise.

## Why

SQLite embodies "simplicity + homelab": one Go binary plus one `.db` file,
trivial to deploy and back up (copy the file). 1.6M rows/year is comfortable for
SQLite. The pure-Go driver avoids CGo, keeping clean cross-compilation for an
OSS binary. Keeping waveforms/routes out of the DB keeps it light and fast for
series queries; those artifacts are read whole anyway. Postgres/Timescale/DuckDB
were rejected as over-engineering or immature Go bindings for this scale today.
