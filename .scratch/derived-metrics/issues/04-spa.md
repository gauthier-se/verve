# 04 — SPA: derived Metrics in Panels

Status: ready-for-agent
Blocked by: 03

## Goal

Let a user add a derived Metric to a Panel and read it at a glance — including the
signed diverging bar for calorie balance.

## Scope

- **Metric picker**: group/label derived Metrics distinctly from imported ones, so
  `calorie_balance`, `total_energy_expenditure`, the macro shares, and
  `protein_per_kg` are discoverable when composing a Panel.
- **Diverging bar rendering**: a **signed** Metric renders as bars from a **zero
  baseline**, colored by sign (deficit vs surplus). Gaps in the series render as
  gaps (no bar), not zero.
- **Formula tooltip**: show the derived Metric's Formula (from `/v1/metrics`) so a
  user understands what the number is.
- Everything else (Dashboard, Time range, bucket override) works unchanged — a
  derived Panel is a normal Panel pointing at a derived slug.

## Out of scope

Backend (01–03). Comparison/trend overlays. Editing formulas in the UI.

## Acceptance

- A user can add `calorie_balance` to a Panel; it shows signed daily bars around a
  zero line, deficit and surplus visually distinct, unlogged days blank.
- The macro-share and `protein_per_kg` Panels render with sensible units (% , g/kg).
- Hovering a derived Panel surfaces its Formula.

## Refs

ADR 0014. web/ (metric picker, panel chart components).
