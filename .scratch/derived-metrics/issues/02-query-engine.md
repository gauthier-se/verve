# 02 — Query engine: derived Metric resolution

Status: ready-for-agent
Blocked by: 01

## Goal

Teach the query engine to answer a series request for a **derived** Metric,
computing it per bucket from its operands — replacing the current
`ErrUnsupportedAggregation` guard for derived Metrics.

## Scope

- **Per-bucket recompute** (`internal/query`): for a derived Metric, resolve each
  Formula operand as its own aggregated series at the requested bucket (reuse the
  existing sum/average/latest paths), then combine bucket-by-bucket in Go:
  `(k · Σ aᵢ·numᵢ) / (Σ bⱼ·dénᵢ)`.
- **Gap semantics**: a bucket is emitted only if *every* operand has a value; a
  zero or absent denominator → gap. No zero-filling.
- **Per-operand Source** (ADR 0003): each operand resolves its own winning Source
  independently; the resulting derived `Series.Source` is empty.
- **No band**: derived Points carry a single value (no min/max), even when an
  operand is an `average` Metric.
- Reuse `maxPoints` / bucket-cap guards unchanged.

## Out of scope

Catalog/Formula definition (01), API wiring (03), UI (04). Comparison/trends.

## Acceptance

- `Series` for `calorie_balance` over a range with food logged returns one signed
  value per bucket = `dietary − active − basal`; days with no `dietary_energy`
  are **absent** (gaps), not zero.
- `protein_per_kg` returns `Σ dietary_protein / body_mass(latest)` per bucket;
  buckets with no weigh-in are gaps.
- Weekly/monthly buckets recompute from operands (not from re-aggregated daily
  derived values) — verified on a sum-of-sums and on a ratio.
- A derived request never returns a min/max band and never a Source.

## Refs

ADR 0003 (source priority), 0014 (derived). internal/query/query.go.
