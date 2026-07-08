# 01 — Dashboard schema + model: the Baseline fields

Status: resolved
Blocked by: —

## Goal

Give the Dashboard a persisted **Baseline** — the second window comparison reads
against — so the rest of the stack has real baseline state to resolve and render.

## Scope

- **Migration** (`internal/data/migrations`, new `0006_*`): add to `dashboards`
  three nullable-friendly columns mirroring the range columns —
  `baseline_rule TEXT NOT NULL DEFAULT 'none'`, `baseline_from TEXT`,
  `baseline_to TEXT` (day-granularity `YYYY-MM-DD`, like `range_from`/`range_to`).
- **Dashboard model** (`internal/data/dashboard.go`): add `BaselineRule string`,
  `BaselineFrom *string`, `BaselineTo *string`; carry them through Insert/Get/
  List/Update SQL and scans, exactly as `RangePreset`/`RangeFrom`/`RangeTo` are.
- **Validation**: `baseline_rule` ∈ `{none, previous, same_period_last_year,
  custom}`; `baseline_from`/`baseline_to` required and well-formed **only** when
  rule = `custom`, and must be nil otherwise; same date-format/order checks the
  range custom bounds already get.

## Out of scope

Query-engine resolution (02), API param + payload wiring (03), SPA (04). The
`all`-disables-comparison rule is enforced at the API/SPA edge, not in the column
constraint.

## Acceptance

- A Dashboard round-trips through Insert/Get/Update preserving `BaselineRule` and,
  for `custom`, `BaselineFrom`/`BaselineTo`.
- A new Dashboard defaults to `baseline_rule = 'none'` with nil bounds.
- Validation rejects `custom` without both bounds, and rejects bounds supplied
  under a non-`custom` rule.
- Existing dashboards migrate to `baseline_rule = 'none'` (no comparison).

## Refs

ADR 0015. CONTEXT.md: Baseline, Baseline rule.
`internal/data/migrations/0005_dashboards.sql`, `internal/data/dashboard.go`.
