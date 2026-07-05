# Verve — Engineering Practices

Architectural, development, and deployment conventions for Verve. These reflect
the decisions recorded in `CONTEXT.md` and `docs/adr/`. When a practice here and
an ADR disagree, the ADR wins — update this file.

## 1. Project structure

Standard Go layout for a web service that ships as a single binary:

- `cmd/verve/` — the entry point (`main.go`), CLI subcommands (`import`,
  `serve`, `account`, `migrate`), server wiring, routes, middleware, handlers.
- `internal/` — business logic and reusable components, not importable by other
  projects:
  - `internal/catalog/` — the canonical Metric Catalog (declarative mapping
    data + unit conversions + aggregation rules).
  - `internal/connector/` — source Connectors (`applehealth` first); reading a
    source and emitting canonical families.
  - `internal/data/` — the storage layer (SQLite access, DAO-style models per
    family: Measurement, State, Session, plus Account, Import).
  - `internal/query/` — the aggregated-bucket query engine.
  - `internal/validator/` — reusable validation for incoming data.

**Dependency injection.** No global state. Dependencies (`*sql.DB`,
`*slog.Logger`, config, Catalog) are injected into a central `application`
struct; HTTP handlers hang off it as methods. Keeps things testable.

## 2. Configuration and data

- **Flags + environment variables**, no mandatory config file. The key setting
  is `VERVE_DATA_DIR`, a single directory holding `verve.db`, `artifacts/`
  (GPX routes, future ECG waveforms), and import files. One volume to mount,
  one thing to back up.
- **SQLite** via the pure-Go driver `modernc.org/sqlite` (no CGo → clean
  cross-compilation). Open with sensible pragmas: `journal_mode=WAL`,
  `foreign_keys=ON`, a `busy_timeout`.
- **Context + timeouts** on every SQL query so one slow query can't block the
  server indefinitely.

## 3. HTTP server and routing

- **Routing:** standard-library `net/http` `ServeMux` (Go ≥ 1.22 method +
  pattern routing). No third-party router — add one only if middleware needs
  genuinely outgrow the stdlib.
- **Centralized routes** listed in one place for clarity.
- **Middleware chain**, modular: `recoverPanic` (never let a panic kill the
  server), `authenticate` (session cookie → Account), CORS as needed. The auth
  middleware is structured so a **forward-auth** mode (trusting a reverse-proxy
  identity header) can be added later without rework.
- **Graceful shutdown:** listen for `SIGINT`/`SIGTERM`, use
  `http.Server.Shutdown()` to drain in-flight requests; track background tasks
  with a `sync.WaitGroup`.

## 4. Data handling and validation

- **JSON helpers** (`writeJSON`, `readJSON`) centralize (de)serialization and
  return clear messages for malformed input (bad syntax, wrong type, unknown
  fields).
- **Structured validation:** a `Validator` accumulates errors and returns the
  whole set at once as `{"error": {"field": "message"}}`, rather than failing on
  the first.
- **Centralized error responses** (`notFoundResponse`, `serverErrorResponse`, …)
  for consistent JSON across the API.

## 5. Database

- **Embedded migrations:** schema lives in SQL files embedded via `go:embed` and
  is applied automatically on startup — zero manual step for the self-hosting
  user. Track applied version in a schema-version table.
- **Prepared statements / parameterized queries** everywhere (`?` placeholders)
  to immunize against SQL injection.
- **Account-scoped queries:** Verve is multi-user with strict isolation — every
  query filters by the owning Account; nothing is ever cross-Account.
- **Portable-ish SQL:** keep queries reasonably standard so a future move to
  Postgres/DuckDB stays possible, without paying that complexity now.

## 6. Ingestion

- **Streaming parse:** the Apple export is ~750 MB; parse with `xml.Decoder`
  token-by-token at constant memory. Never load the whole file.
- **Idempotent import:** dedup on a **content key** —
  hash of `(metric, source, start, end, value, unit)`, `creationDate` excluded.
  Re-importing a full snapshot adds only new rows. Each Import is recorded with
  added/skipped/unmapped counts.
- **Never discard data:** types the Catalog can't map go to the Unmapped bin,
  kept and inspectable.

## 7. Observability and tooling

- **Structured logs** via `log/slog`.
- **Runtime diagnostics** via `expvar` (goroutines, DB pool stats, build/version
  info) exposed in a standard way.
- **Makefile** for repetitive tasks: `make run`, `make audit` (format, vet,
  lint, test), `make build`.

## 8. Security

- **Passwords hashed with argon2id.** Opaque, signed session cookies for auth.
- **Strict per-Account isolation** on all data access (see §5).
- **Rate limiting** on auth-sensitive endpoints to blunt brute-forcing.

## 9. Front-end and packaging

- **Single binary:** the Go server serves a JSON API and a Vite-built React SPA
  embedded via `go:embed`. SQLite lives beside the binary in `VERVE_DATA_DIR`.
- **UI stack:** React + Vite + **shadcn/ui** (copied into the repo, no lock-in);
  **Recharts** for aggregated time-series. **uPlot** is reserved for the future
  ECG waveform viewer.
- **Aggregated-bucket API:** the API never returns raw series — the query engine
  aggregates server-side and returns at most a few hundred points per Panel.
- **Distribution:** a distroless/scratch Docker image (primary, homelab
  `compose.yml`) plus static per-OS/arch binaries. The CGo-free build makes both
  trivial.
