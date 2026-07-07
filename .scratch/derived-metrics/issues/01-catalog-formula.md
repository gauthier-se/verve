# 01 — Catalog: Formula type + seed derived Metrics

Status: resolved
Blocked by: —

## Goal

Give the Catalog a **declarative Formula** and seed the first derived Metrics, so
the rest of the stack has real `Nature.Derived` entries to resolve.

## Scope

- **Formula type** (`internal/catalog`): a ratio of two weighted sums × a constant
  — numerator terms, denominator terms (empty = 1), scale `k`. A term is
  `{metric slug, coefficient}`. Pure data (serializable), no closures.
- **Derived Metric entries** carry a Formula, a canonical unit, and a **signed**
  flag; they have **no aggregation rule** (unlike imported entries — reflect this
  in the `Metric` shape, don't fake a rule).
- **Seed set**:
  - `total_energy_expenditure` = `active_energy + basal_energy` (kcal)
  - `calorie_balance` = `dietary_energy − active_energy − basal_energy` (kcal, signed)
  - `protein_per_kg` = `dietary_protein / body_mass` (g/kg)
  - `protein_energy_share` = `4·dietary_protein / dietary_energy` (%, ×100 scale)
  - `carb_energy_share` = `4·dietary_carbohydrates / dietary_energy` (%)
  - `fat_energy_share` = `9·dietary_fat_total / dietary_energy` (%)
- **Build-time validation** (test): every operand slug exists in the Catalog;
  numerator terms share a unit; the declared result unit is consistent with
  num/den units (kcal for sums, g/kg for the ratio, % dimensionless).

## Out of scope

Engine computation (02), API/SPA (03/04). User-defined formulas.

## Acceptance

- `catalog.Lookup("calorie_balance")` returns a derived Metric with its Formula,
  unit `kcal`, and signed = true.
- A Catalog test fails if a derived Metric references an unknown slug or mixes
  incompatible numerator units.
- No derived Metric declares an aggregation rule.

## Refs

ADR 0002 (Catalog), 0014 (derived Formula). CONTEXT.md: Metric, Formula.
