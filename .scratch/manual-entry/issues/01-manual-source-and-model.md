# 01 — Data: the Manual Source, single-row insert, guarded delete

Status: done
Blocked by: —

## Goal

Give `MeasurementModel` its first human writer and its first `Delete`, with the
"only `Manual` rows are deletable" invariant enforced in SQL rather than in a
handler (ADR 0022).

## Scope

- **Reserved Source constant** (`internal/data/measurement.go` or
  `internal/catalog`): `SourceManual = "Manual"`. One definition, referenced by the
  model, the API and the priority table — never a bare string literal.
- **`InsertOne(ctx, *Measurement) error`** — inserts a single row and returns its
  generated `id`. Distinct from the existing bulk import path: that one batches and
  swallows duplicates, this one must tell the caller what happened. Content key is
  computed by the **same** helper the import uses (ADR 0006) — do not reimplement the
  hash. A content-key collision is **not** an error: return the existing row's id so
  submitting the same entry twice is idempotent, matching import semantics.
- **`Delete(ctx, accountID, id int64) error`** — the guard is in the statement:
  ```sql
  DELETE FROM measurements
  WHERE id = ? AND account_id = ? AND source = 'Manual'
  ```
  Zero rows affected → `ErrRecordNotFound`. The handler distinguishes "absent" from
  "not yours" from "not Manual" (issue 02) by reading the row first; the model's job
  is that no caller can ever delete an imported row, however it is invoked.
- **`ListManual(ctx, accountID, metric string, limit int)`** — manual rows, newest
  first, optionally filtered by Metric. Returns rows **with their ids**; the Ledger's
  aggregated Series (ADR 0021) carry none, so this is the only address a client has.
- **No Source-priority entry.** `Manual` deliberately does not compete: priority
  elects one whole-range winner, so ranking it first would collapse a chart to the
  single day that was typed. Displacement is the **Manual overlay**, issue 04.

## Out of scope

- Any HTTP surface (issue 02) and any UI (issue 03).
- `PATCH` / editing. There is none.
- Tombstones for imported rows. Deliberately absent (ADR 0022).

## Acceptance

- `InsertOne` populates the id; the row is readable through the normal query path and
  is indistinguishable from an imported row apart from its Source.
- Inserting the same (metric, value, timestamp) twice yields **one** row and the same
  id both times.
- `Delete` on a row whose Source is `Apple Watch` (or anything but `Manual`) removes
  nothing and returns `ErrRecordNotFound` — pinned by a test that inserts an imported
  row directly and attempts to delete it.
- `Delete` cannot cross Accounts: another Account's manual row is untouched.
- Existing import tests still pass; the bulk path is untouched.

Read-side behaviour (a manual value winning its own day without blanking the rest)
is issue 04's acceptance, not this one's.

## Refs

ADR 0022, ADR 0003 (Source priority), ADR 0006 (content key). CONTEXT.md: **Manual
entry**, **Source priority**.
`internal/data/measurement.go`, `internal/catalog/priority.go`.

## Comments

Implemented on branch `feat/manual-entry`.

- **The content-key helper had to move first.** `contentKey` was unexported inside
  `internal/connector/applehealth`, so the API layer could not reach it and issue 01's
  "do not reimplement the hash" was impossible as written. Moved to
  `internal/data/contentkey.go` as exported `ContentKey` / `StateContentKey` /
  `SessionContentKey` — its real home, since a content key identifies a stored row
  rather than anything Apple-specific. Four call sites in the Connector updated;
  `applehealth` already imported `data`, so no new dependency.
- `catalog.SourceManual` holds the reserved name, with a comment on why it is
  deliberately absent from `sourcePriority`.
- `Measurement` gained `ID`, populated by `InsertOne` and `GetByID`/`ListManual` and
  left zero by the bulk import path (nothing addresses those rows individually).
- `InsertOne` uses `ON CONFLICT (account_id, content_key) DO NOTHING RETURNING id`,
  then resolves the existing id on `sql.ErrNoRows` — so a repeat entry is a no-op that
  still reports where the value lives, matching import idempotence.
- `Delete` carries `AND source = ?` in the statement; `GetByID` exists so the handler
  can tell 404 from 403 (issue 02), a distinction `Delete` alone cannot express since
  both cases match no row.
- Tests in `internal/data/manualentry_test.go`, 6 cases. The load-bearing one is
  `TestDeleteRefusesImportedRow`, which asserts both the error *and* that the row
  survives.
- Full suite green, including the Connector after the content-key move.
