# PRD — Verve v1 core

## Goal

Ship the daily-usable core: import an Apple Health export, store it in the
canonical model (SQLite), and visualize it as single-metric dashboards. None of
the four differentiators (period comparison, cross-metric, annotations, derived
metrics) are in v1 — they follow.

Context, glossary, and decisions: see `CONTEXT.md` and `docs/adr/0001`–`0012`.

## What v1 does

- `verve import --account=… export.zip`: streaming parse, Catalog mapping, unit
  normalization, idempotent dedup, import report.
- Ingests **Measurements** (nutrition included), **State** (sleep), **Sessions**
  (workouts + referenced GPX routes). ECG deferred.
- Multi-user from day one (strict Account isolation), local argon2id auth.
- **Aggregated-bucket** JSON API (never raw series), capped resolution.
- Embedded React SPA (Vite + shadcn/Recharts) via `go:embed`: **Dashboards** of
  single-metric **Panels**, global **Time range**.
- One binary, one `VERVE_DATA_DIR`, auto-applied embedded migrations. Docker +
  binaries.

## What v1 does NOT do

- Rich workout UI (list / GPS map) → v1.x
- Web upload of the export (self-service) → v1.x; the CLI is enough
- Period comparison, cross-metric, annotations, derived metrics
- ECG viewer/ingestion, connectors other than Apple Health
- Forward-auth SSO (the middleware only needs to stay extensible)

## Technical conventions

See `good_practices.md`: `application` struct + dependency injection,
`cmd/`/`internal/`, flags+env config, `log/slog`, JSON helpers, `Validator`,
graceful shutdown. Stack per the ADRs: SQLite (`modernc.org/sqlite`), argon2id,
embedded migrations, `net/http` stdlib router (no chi).

## Slices (issues)

1. Foundations: skeleton, SQLite, migrations, Account, CLI
2. **Ingestion core**: Catalog + XML parser + Measurements + dedup + report
3. State (sleep) + Sessions (workouts + routes) ingestion
4. Query engine + aggregated-bucket JSON API
5. Local argon2id auth + sessions + Account scoping
6. Embedded SPA: Dashboards / Panels / Time range
7. Packaging: Docker, goreleaser, compose, docs
