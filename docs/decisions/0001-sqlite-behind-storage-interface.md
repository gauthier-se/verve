# 0001. SQLite behind a storage interface

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Verve targets homelab users who want a single binary and as little operational work as possible. A multi-year Apple Health history typically holds 5 to 30 million samples per user, and a household has a handful of users. Writes come from imports and periodic syncs, reads from dashboards.

## Decision

- Use SQLite in WAL mode as the only supported database, through the pure Go driver `modernc.org/sqlite` (no CGO).
- Keep dashboards fast with precomputed hourly and daily aggregates instead of scanning raw samples.
- Hide the database behind repository interfaces in `internal/store`. Domain code never imports the SQLite implementation.
- Write queries with sqlc, which also supports Postgres, so a second implementation remains a realistic path.

## Consequences

- Deployment is one binary plus one data directory. Backups are a single consistent file (`VACUUM INTO`).
- Cross-compilation and `scratch` or `distroless` images are trivial because there is no CGO.
- One writer at a time: writers must use short, bounded transactions. This is acceptable at household scale.
- The storage interface adds some indirection, accepted as the price of keeping the Postgres option open.

## Alternatives considered

- **Postgres (optionally with TimescaleDB)**: better time-series aggregation and concurrency, but requires running and upgrading a database container, which contradicts the single binary goal.
- **Support both from day one**: doubles migrations and test matrices for little benefit to self-hosters.
- **`mattn/go-sqlite3`**: mature but requires CGO, which complicates cross-compilation and minimal images.
