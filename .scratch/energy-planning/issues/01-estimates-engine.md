# 01 — Server: the Estimates engine

Status: done
Blocked by: —

## Goal

Compute the **Basal estimate** and the **Expenditure estimate** on read, in one
module, with the **Estimate basis** carried alongside the number so no caller can
present a figure without saying where it came from (ADR 0023).

## Scope

New package `internal/estimate` (peer of `internal/timeaxis`, which is the model to
follow: pure resolution logic, no HTTP, heavily table-tested).

- **Basal equations** — declared as **data**, not a switch:
  ```go
  type Equation struct {
      ID      string   // "katch_mcardle", "cunningham", "mifflin_st_jeor", "harris_benedict"
      Needs   []Input  // leanMass | mass | height | age | sex
      Compute func(Inputs) float64
  }
  ```
  - Katch-McArdle `370 + 21.6·LBM`, Cunningham `500 + 22·LBM`
  - Mifflin-St Jeor `10·W + 6.25·H − 5·A + s` (s = +5 male, −161 female)
  - Harris-Benedict (revised / Roza), sex-specific coefficients
  - **`Needs` drives the UI's greying-out**, so the client never hardcodes which
    equation wants what. Return, per equation, whether it is computable and which
    input is missing.
- **Lean mass resolution** — prefer the `lean_body_mass` Metric; else
  `body_mass × (1 − body_fat_percentage)`. Note in a comment **why the fallback is
  rarely independent**: on the reference Account the two are algebraically identical
  to two decimals because the scale derives one from the other. Nothing to fix here,
  but the next reader will wonder.
- **`%` Metrics are stored as fractions.** `body_fat_percentage` is `0.27`, not `27`.
  `1 − value` is correct; `1 − value/100` is a 26-point error that will still produce
  a plausible-looking number. Pin it with a test.
- **Expenditure cascade** — returns `(kcal float64, basis Basis, detail)`:
  1. `observed` — over a 28-day window: mean daily `dietary_energy`, and the body-mass
     slope from an **ordinary least-squares regression** over daily values.
     `TDEE = meanIntake − (slopeKgPerDay × 7700)`. Requires enough coverage in *both*
     series — require intake on ≥ 70% of days and ≥ 10 distinct mass days, else fall
     through. Endpoint differencing is **wrong** here (±1.5 kg daily noise) and must
     not be used.
  2. `recorded` — mean daily `total_energy_expenditure` over the same window, via the
     existing derived-Metric path (`internal/query`), not a fresh SQL query.
  3. `predicted` — Basal estimate × activity factor (default 1.4).
- **Measured actual rate** — the same regression, expressed as % of body mass per
  week. The Plan page's slider opens on it (issue 04) and adherence compares against
  it (issue 02), so it belongs here, computed once.
- Reuse `internal/query` for every read. This package does arithmetic on Series; it
  does not talk to SQL.

## Out of scope

- Any HTTP surface (issue 02) or UI (issue 04).
- Persisting anything. Estimates are computed on read and never stored (ADR 0023).
- Adding anything to the Catalog. Deliberately (ADR 0023).
- Auditing whether the body-composition source is informative (see PRD, "does NOT
  do").

## Acceptance

- Each equation returns its published value for a worked example; a table test pins
  all four against hand-computed figures.
- `Needs` correctly reports Mifflin-St Jeor uncomputable when `date_of_birth` is
  NULL, and Katch-McArdle computable from lean mass alone.
- Body-fat handling: an account at 91 kg and `body_fat_percentage = 0.27` yields
  LBM ≈ 66.4 kg and Katch-McArdle ≈ 1 804 kcal. A test asserts the fraction scale
  explicitly.
- The cascade picks `observed` when both series are dense, `recorded` when intake is
  absent, `predicted` when both are — and **always** reports which.
- On a fixture reproducing the reference window (mean intake 2 078 kcal, mass
  92.75 → 91.00 over 28 days) the observed estimate is ≈ 2 559 kcal, and the recorded
  basis on the same fixture is materially higher — a test that documents the
  divergence rather than hiding it.
- The mass slope is a regression: a fixture with a noisy final reading yields nearly
  the same slope as one without, whereas endpoint differencing would not.
- Insufficient data returns no estimate and a reason, never a zero.

## Refs

ADR 0023, ADR 0014 (derived Metrics), ADR 0012 (server-side folding).
CONTEXT.md: **Estimate**, **Basal estimate**, **Basal equation**, **Expenditure
estimate**, **Estimate basis**.
`internal/timeaxis/` (package shape to follow), `internal/query/query.go`,
`internal/catalog/catalog.go`.

## Comments

Implemented on branch `feat/manual-entry`, in `internal/estimate`. 19 tests plus a
skipped-by-default probe.

- Equations are data (`Equations`, with `Needs` and a `compute` func), so `Basal` returns
  a per-equation result carrying either a figure or the list of inputs that would unlock
  it. The client never hardcodes which equation wants what.
- **`Basal` is pure and does not rank.** Preselection depends on the Account's declared
  trust in its body-composition data, which is issue 03's business; this package refuses
  to decide it, and the comment on `Equations` says why.
- Body inputs are **averaged over 28 days** rather than read as a single value — a scale
  swings more than a kilogram between mornings. Height uses a 30-year month-bucketed
  window, because it is measured once and never again and the engine caps a request at
  1000 points.
- The percent-fraction trap is pinned by `TestBodyFatIsAFractionNotAPercent`: dividing by
  100 would give a lean mass of 90.75 kg and ≈2330 kcal — wrong, and plausible-looking.
- `TestSlopeIsRegressionNotEndpoints` asserts both halves: the regression barely moves
  under a spiked final reading, *and* endpoint differencing is badly wrong on the same
  data. The second assertion is what stops someone "simplifying" the fit later.
- `ErrInsufficientData` rather than a zero. Zero is a number, and it would flow straight
  into a calorie target.

### Change from the spec

**`defaultActivityFactor` is 1.375, not 1.4.** The PRD's 1.4 was calibrated on the
reference Account's own observed ratio — but the predicted basis exists precisely for
Accounts with *no* history, so tuning it against somebody who has one is fitting a
default to a sample of one. 1.375 is the standard "lightly active" multiplier from the
literature. This closes the open question flagged when the PRD was written.

### Probe against the real 310k-row export

`probe_test.go`, skipped unless `VERVE_PROBE_DB` points at a **copy**. Run on the
reference export:

```
mass 91.64 kg · height 185 cm · lean 66.79 kg (measured, not derived)
Katch-McArdle 1813 · Cunningham 1969
Mifflin-St Jeor, Harris-Benedict: uncomputable, missing [age sex]
EXPENDITURE 2390 kcal, basis=observed
  mean intake 2068 kcal over 25 days · slope −0.0418 kg/day over 19 mass days
ACTUAL RATE −0.319 %/week (−0.293 kg/week) · TDEE/basal = 1.32
```

Two things worth recording.

**The observed figure is 2390, not the ≈2559 quoted in the PRD.** That earlier number came
from differencing the window's endpoints (92.75 → 91.00). The regression finds a shallower
true slope, so the endpoint method had overstated the deficit by roughly 160 kcal/day. The
engine is right and the PRD's illustration was optimistic — which is precisely the failure
`TestSlopeIsRegressionNotEndpoints` guards against. The ratio of 1.32 remains physiologically
ordinary, and still nowhere near the devices' 1.96.

**Height resolves to 185 cm, not the newest reading of 184 cm.** Not a bug in this package:
`height` has no Source-priority entry, so `ResolveSource` falls back to alphabetical order,
"Health" beats "iPhone de Gauthier", and the winning Source's latest value is the 2023 one.
Consistent with ADR 0003 and with every chart in the app, but it means "latest" means
"latest from the winning Source", not "newest overall". Worth knowing before it is reported
as a defect — and a Manual entry is the remedy, since it outranks both.
