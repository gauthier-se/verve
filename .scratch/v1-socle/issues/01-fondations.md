# 01 — Foundations: skeleton, SQLite, migrations, Account, CLI

Status: ready-for-agent
Blocked by: —

## Goal

Lay the skeleton that unblocks everything else: the Go project, a SQLite
database opened with auto-applied embedded migrations, the Account model, and the
`verve` binary with its subcommands.

## Scope

- `go.mod` (module `verve`), layout `cmd/verve/`, `internal/…` per
  `good_practices.md`.
- `application` struct (injected deps: `*slog.Logger`, `*sql.DB`, config).
- Flags + env config: at minimum `VERVE_DATA_DIR` (holds `verve.db`,
  `artifacts/`, imports). Sensible default.
- Open SQLite via `modernc.org/sqlite` (pragmas: `journal_mode=WAL`,
  `foreign_keys=ON`, `busy_timeout`).
- **Embedded** SQL migrations (`go:embed`), applied on startup. First migration:
  `accounts` table (id, unique email, password_hash nullable for now, `Me`
  profile: dob, biological_sex, blood_type… nullable), + a schema-version table.
- CLI `verve` (stdlib arg parsing) with: `verve migrate` (no-op if auto),
  `verve account create --email=…` (creates an Account; password hashing lands
  in slice 05 — here accept without a password or set a basic one).
- Structured `log/slog`, graceful shutdown scaffolding ready (used once the
  server exists).

## Out of scope

HTTP server, ingestion, full auth (just the `password_hash` column).

## Acceptance

- `go build ./...` passes, `verve` binary produced.
- Running the binary creates/migrates `verve.db` in `VERVE_DATA_DIR` with no
  manual step; re-running is idempotent.
- `verve account create --email=x` inserts an Account, visible in the DB.

## Refs

ADR 0004 (SQLite), 0005 (binary), 0007 (Account/multi-user). `good_practices.md`.
