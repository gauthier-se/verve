# Derived Metrics: declarative Formula, recomputed per bucket

## Context

The Catalog has always carried a `derived` nature (ADR 0002) but held no derived
Metrics. The first differentiator after the v1 core adds them — `calorie_balance`,
`total_energy_expenditure`, `protein_per_kg`, macro energy shares — computed on
read from other Metrics. Two shapes had to be pinned: how a formula is *expressed*
and how a derived value is *computed* across time buckets and missing data.

## Decision

A derived Metric is defined by a **Formula**: a ratio of two weighted sums plus an
optional constant scale — `(k · Σ aᵢ·numᵢ) / (Σ bⱼ·dénᵢ)`. This is strictly more
than a plain weighted sum (it buys ratios like `dietary_protein / body_mass` and
macro shares via Atwater factors) and strictly less than a general expression
(no nesting, no operator precedence). The Formula is **declarative data**, not a
Go closure.

Formulas ship **compiled into the binary** (added by PR, like Connectors), but
because they are data the same representation can back a per-Account formula
editor later without a rewrite.

A derived Metric has **no aggregation rule of its own**. At the requested bucket,
each operand is aggregated by *its own* Catalog rule (`dietary_protein` summed,
`body_mass` taken `latest`), then the Formula is applied **per bucket** in Go —
never by re-aggregating already-derived values. Every operand is **required**: a
bucket where any operand, or the whole denominator (including a zero denominator),
has no data is a **gap**, not a zero.

## Why

- **Ratio-of-sums, not a general AST.** It covers every derived Metric on the
  near-term list while staying trivially serializable and validatable, with no
  parser, no precedence engine, and a future editor that is just adding/removing
  terms.
- **Declarative, compiled-first.** Curation-by-PR matches the closed-but-extensible
  Catalog (ADR 0002) and needs no runtime eval or sandbox; keeping the Formula as
  data preserves the option to open it to users without redoing the model.
- **Per-bucket recompute, own-rule operands.** The only definition that is correct
  for both `sum + sum − sum` and a `sum / latest` ratio at day/week/month. Storing a
  single aggregation rule on the derived Metric would be wrong for ratios (daily
  ratios do not re-aggregate).
- **Gap on missing operand.** Treating a missing operand as zero would paint a huge
  fake deficit on every day food was not logged; a gap is the honest reading —
  without a known intake there is no balance to show.

## Considered Options

- **Plain weighted sum (no denominator):** rejected — cannot express ratios or macro
  shares, which are among the most useful derived Metrics.
- **General arithmetic expression (AST + parser):** rejected for now — needs a real
  parser, evaluator, and editor, and reopens mixed-aggregation semantics with no
  near-term payoff.
- **A dedicated aggregation rule on the derived Metric:** rejected — incorrect for
  ratios and redundant once operands recompute per bucket by their own rule.
- **Missing operand = 0:** rejected — misleading for calorie tracking (see Why).

## Consequences

- The query engine's `duration_by_state`/derived guard is replaced by a derived
  path that resolves each operand independently (own Source priority per operand,
  ADR 0003) and combines in Go; `Series.Source` is empty for a derived Series.
- Operand unit compatibility (numerator terms share a unit; result unit is
  derived) is validated at build time by a Catalog test, not at query time.
- Derived values can be **signed** (`calorie_balance`); the API exposes a hint so a
  Panel renders them as a diverging bar around zero.
