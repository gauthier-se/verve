# PRD — Métriques dérivées

## Goal

Ship the first differentiator after the v1 core: **derived Metrics** — canonical
Metrics computed on read from a declarative Formula over other Metrics. Deliver
the daily-useful set (calorie deficit/surplus, total energy expenditure, macro
shares, protein per kg) end-to-end, so a derived Metric is "just another slug"
you drop into a Panel.

Context, glossary, and the design decision: see `CONTEXT.md`
(**Metric → derived**, **Formula**) and `docs/adr/0014`.

## What this milestone does

- Adds a **declarative Formula** to the Catalog: a ratio of two weighted sums ×
  an optional constant — `(k · Σ aᵢ·numᵢ) / (Σ bⱼ·dénᵢ)`. Compiled in (added by
  PR), shaped as data so a user-defined editor can plug in later.
- Seeds the first derived Metrics:
  - `total_energy_expenditure` = `active_energy + basal_energy` (kcal)
  - `calorie_balance` = `dietary_energy − (active_energy + basal_energy)` (kcal,
    **signed**: deficit negative, surplus positive)
  - `protein_per_kg` = `dietary_protein / body_mass` (g/kg)
  - macro energy shares (Atwater ×4/×4/×9 over `dietary_energy`):
    `protein_energy_share`, `carb_energy_share`, `fat_energy_share` (%)
- Extends the **query engine** to resolve a derived Metric at the requested
  bucket: aggregate each operand by *its own* rule, apply the Formula per bucket
  in Go. Missing operand or zero denominator → **gap** (all operands required).
- Exposes derived Metrics through the **API** (`/v1/metrics` reports nature,
  Formula, unit, signed hint; `/v1/series` serves derived slugs) and the **SPA**
  (metric picker distinguishes derived; a signed Metric renders as a diverging
  bar around zero).

## What this milestone does NOT do

- Period comparison / trends (next milestone) — including the `body_mass`/`latest`
  vs "weekly average" tension and the rolling-average "trend" concept.
- User-defined formulas (runtime editor) — the shape stays open for it; the UI is
  not built.
- A general expression language (nesting, precedence, functions).
- Per-term "optional operand = 0" — every operand is required for now.
- Cross-metric Panels, annotations.

## Design invariants (from ADR 0014)

- A derived Metric has **no aggregation rule of its own** — operands recompute per
  bucket by their own rule; derived values are never re-aggregated.
- Every operand is **required**: any missing operand, or a zero/absent denominator,
  yields a gap, never a zero.
- `Series.Source` is empty for a derived Series; each operand still resolves its own
  winning Source (ADR 0003).
- Operand unit compatibility is checked at **build time** by a Catalog test.

## Slices (issues)

1. **Catalog + Formula**: declarative Formula type, the seed derived Metrics,
   build-time unit-compatibility validation.
2. **Query engine**: per-bucket derived resolution, gap semantics, per-operand
   Source resolution.
3. **API**: `/v1/metrics` exposes derived (nature, Formula, signed hint);
   `/v1/series` serves derived slugs; chart-type/baseline hints.
4. **SPA**: metric picker groups derived Metrics; diverging-bar rendering around
   zero for signed Metrics; Formula shown in a tooltip.
