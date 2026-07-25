# 02 — Data + API: Phases, targets and adherence

Status: done
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
  - `targetKcal = expenditure + (ratePctPerWeek/100 × massKg × 7700 / 7)` — an
    **addition**, because the rate is signed: a cut is negative, and adding a negative is
    what makes the deficit. Writing it as a subtraction turns every cut into a surplus.
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

## Comments

Implemented on branch `feat/manual-entry`, **together with issue 03's server side**.

### Why 02 and 03 shipped as one

`GET /v1/plan` must return a preselected equation, and preselection depends on the
body-composition trust setting — issue 03's column. Shipping 02 alone would have meant
hardcoding a preselection and then rewiring it, i.e. writing a stub and calling it done.
Issue 03's remaining scope is its UI, which was always specified to live on the Plan page
and therefore belongs to issue 04.

### The sign error, caught by a test

The PRD and this issue both wrote
`targetKcal = expenditure − (rate/100 × mass × 7700 / 7)`. With a **signed** rate that is
backwards: a cut is negative, so subtracting it *adds* calories and every deficit became a
surplus. The first run produced 2894 kcal for a −0.5 %/week cut against a 2390 expenditure.
Fixed to an addition; the spec above is corrected. `TestSurplusRaisesTheTarget` now asserts
the *ordering* (cut < maintenance < bulk) rather than any single figure, so the error class
cannot come back under a different constant.

### Two more defects the tests found

- **`Adherence` on a just-opened Phase hit a zero-length window.** The query engine rejects
  a non-positive range with `ErrInvalidRange`, so opening a Phase and reloading the page
  would have been a 500. Now the empty window returns the targets with absent actuals —
  honest, and the only correct answer.
- **The trust suggestion could not see a Manual entry.** It read `Series.Source`, which the
  Manual overlay deliberately keeps reporting as the *imported* Source so one corrected day
  does not rename a whole curve (ADR 0022). An Account with both a scale and a hand-entered
  body fat therefore read as "estimated" forever. It now asks the measurement store
  directly via `ListManual`.

### Notes

- `Open` closes the previous Phase and inserts the new one in one transaction, with the
  partial unique index as the backstop. `TestOnlyOnePhaseOpenPerAccount` bypasses `Open`
  to insert directly, so it tests the index rather than the Go code.
- `Close` is deliberately **not** idempotent: re-closing would move the end date and
  rewrite history, so it returns `ErrRecordNotFound`.
- The rate in force follows a precedence: explicit `?rate=` preview → open Phase → measured
  actual rate. The last is what makes the page open by stating what the Account is already
  doing instead of presenting an empty form.
- Guardrails gained a fourth code, `carb_squeezed_out`: at an extreme target the protein
  and fat floors can exceed the whole budget. Carbohydrate floors at zero rather than going
  negative, and this rail is what reports that the target cannot be met as stated.

### End-to-end against the binary

Throwaway DB, 28 seeded days matching the reference export's shape:

```
plan (no phase)  presel=katch_mcardle  exp=2577 observed  rate=−0.49  target=2078 kcal  protein=132 g
profile: dob+sex+trust=estimated  →  presel=mifflin_st_jeor (1922); katch still shown (1804)
trust=measured                    →  presel=katch_mcardle
rate=−2                           →  target 556 kcal, carb 0, rails=[target_below_basal,
                                     rate_unsustainable, carb_squeezed_out] — warned, not blocked
open phase −0.75  →  rate −0.75, adherence window 1 day, thin, actuals absent
open phase +0.25  →  history [+0.25 open, −0.75 closed] — exactly one open
manual body_fat   →  derived_trust flips estimated → measured
```

The first line is the strongest check in the set. The derived target (2078 kcal) equals the
seeded mean intake exactly — as it must: an Account eating 2078 and losing at −0.49 %/week
must eat 2078 to keep losing at −0.49 %/week. The cascade and the target derivation are
consistent with each other, which no single-figure assertion would have shown.
