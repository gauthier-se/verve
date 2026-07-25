# 02 — Data + API: Phases, targets and adherence

Status: ready-for-agent
Blocked by: 01

## Goal

Persist what the Account *meant* to do, so that what it actually did can be judged
against it. Without this the page can only compare the slider to reality — and the
slider opens on reality, so the comparison would be vacuous.

## Scope

- **Migration: `phases`** (`internal/data/migrations/`)
  ```
  id, account_id, rate_pct_per_week REAL, started_at, ended_at NULL, created_at
  ```
  - `ended_at IS NULL` means open. **At most one open Phase per Account** — enforce
    with a partial unique index, not application logic:
    `CREATE UNIQUE INDEX ... ON phases(account_id) WHERE ended_at IS NULL`.
  - History is never overwritten; a Phase is closed, not replaced.
- **`PhaseModel`** in `internal/data/phase.go`, registered in `models.go` — follow
  `DashboardModel` for the Account-scoped CRUD shape.
  - `Open(accountID, rate)` closes the current Phase (`ended_at = now`) and inserts
    the new one **in one transaction**; the index makes a race fail loudly rather
    than leaving two open.
  - `Current`, `List`, `Close`, `Delete` — all Account-scoped in the `WHERE`.
- **`GET /v1/plan`** — everything the page renders in one call:
  - Basal estimate per equation: value, computable, missing inputs (issue 01)
  - Expenditure estimate + basis + the window it used
  - measured actual rate
  - current Phase (or none)
  - derived targets for the current or requested rate: calories, protein floor, fat
    floor, carbohydrate remainder
  - adherence over the open Phase's real window
  - active guardrails
  - Accepts `?rate=` so the slider can preview a rate without opening a Phase. **The
    server derives the targets** — the client must not reimplement the arithmetic
    (same reasoning as ADR 0019).
- **`POST /v1/phases`** (open, closing the previous), **`GET /v1/phases`** (history),
  **`PATCH /v1/phases/{id}`** (close), **`DELETE /v1/phases/{id}`** (drop a mistake).
- **Target derivation**
  - `targetKcal = expenditure − (ratePctPerWeek/100 × massKg × 7700 / 7)`
  - protein floor: 1.6 g/kg lean mass at rate ≥ 0, rising to 2.4 at ≤ −1%/week,
    interpolated between; falls back to body mass when lean mass is unavailable, and
    **says so** in the response
  - fat floor: 0.6 g/kg body mass, and at least 20% of target calories
  - carbohydrates: the remainder — flagged in the payload as a **convention**, not a
    recommendation, so the UI can label it honestly
- **Adherence** over `[phase.started_at, min(now, phase.ended_at)]`: target vs actual
  for rate (the regression from issue 01), mean intake, mean protein. **No lean-mass
  adherence** — deliberately (see PRD).
- **Guardrails**, returned as structured warnings, never as errors:
  `target_below_basal`, `rate_unsustainable` (|rate| > 1%/week held > 8 weeks),
  `protein_below_floor_in_deficit`.

## Out of scope

- Blocking any input. Every guardrail is advisory.
- Phase annotations on Panels.
- Client-side recomputation of anything (issue 04 renders what this returns).

## Acceptance

- Opening a Phase closes the previous one; the history shows both with contiguous
  windows and exactly one open.
- The partial unique index rejects a second open Phase for the same Account.
- Two Accounts can each hold an open Phase.
- `GET /v1/plan?rate=-0.5` on the reference fixture returns ≈ 2 058 kcal against an
  expenditure of 2 559 and a mass of 91 kg, and a protein floor of ≈ 133 g.
- A target below the Basal estimate raises `target_below_basal` and still returns the
  target.
- Adherence spans the Phase's real window, not a fixed 28 days — a Phase opened 5 days
  ago is judged over 5 days, with the thin window signalled.
- No Phase → the endpoint still returns estimates and previewed targets, with the
  Phase and adherence blocks absent.
- Every query is Account-scoped; a cross-Account id yields 404.

## Refs

ADR 0023, ADR 0019 (server-side derivation), ADR 0015 (neutral, uncoloured feedback).
CONTEXT.md: **Phase**, **Target rate**.
`internal/data/dashboard.go` (CRUD shape), `internal/data/migrations/`,
`internal/data/models.go`, `internal/api/server.go`.
