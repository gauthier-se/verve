# PRD — Cross-metric overlays

## Goal

Let a Panel show **several Metrics on one chart** — sleep vs resting heart rate,
nutrition vs weight — so correlations across sources become visible. This is the
roadmap's v1.2 and the last missing brick of the "powerful dashboards" promise.
Today a Panel is single-Metric in the schema even though the glossary already
says "one or more Metrics".

Context, glossary, and the design decision: see `CONTEXT.md` (**Panel**) and
`docs/adr/0020`.

## What this milestone does

- A Panel carries **one to four Metrics**, constrained to **at most two
  distinct units**, validated server-side at Panel save (against the Catalog's
  canonical units). Derived Metrics participate like any other.
- **Dual Y axes**: Metrics sharing a unit share an axis; the first Metric's
  unit group takes the left axis, the other (if any) the right. The axis side
  is derived from units, never stored.
- **Chart type per Metric**, defaulted by its aggregation rule exactly as
  today (sum→bar, average→line, latest→line…); the user may switch among
  compatible types per Metric. Rendered as a composed chart (bar + line mixes).
- **Storage**: new `panel_metrics` join table
  (`panel_id, account_id, metric, chart_type, position`); migration copies each
  existing `panels.metric`/`chart_type` into one row, then drops those columns.
- **API**: `/v1/series` accepts a **repeatable `metric` parameter** and returns
  one Series per Metric; the time axis is resolved once so every Series shares
  identical buckets. Single `metric` = today's behavior (backward compatible).
- **Baseline is cut** on multi-Metric Panels: with period comparison on, only
  single-Metric Panels render their Baseline (same exclusion mechanic as the
  `all` range, ADR 0015). Decided server-side.
- **Panel summary per Series in the legend**: each legend entry shows its
  Series' folded value + unit. The large headline figure remains the
  single-Metric rendering; summaries stay universal (ADR 0019).
- **Panel editor**: add/remove/reorder Metrics, per-Metric chart type, live
  feedback when a Metric would introduce a third unit or a fifth Metric.

## What this milestone does NOT do

- **Normalized/indexed scales** — magnitude must stay legible (ADR 0019); a
  Metric never renders on someone else's scale.
- **Baseline on multi-Metric Panels** — no "primary Metric" notion; cut
  entirely.
- **Per-Metric colors as user choice** — the categorical palette assigns by
  position in the Panel, deterministic, not configurable.
- **A new domain noun** — no "Overlay", "Trace", or "Layer"; the Panel simply
  carries N Metrics.
- **`duration_by_state` in overlays** — sleep is still not served by the query
  engine; it joins overlays when it lands.
- **Stacking Metrics** (`stacked_bar` across Metrics) — stacking is
  within-Metric semantics; out of scope here.

## Issues

1. `01-data-panel-metrics` — schema: `panel_metrics` join table, migration from
   the scalar columns, store CRUD.
2. `02-api-multiseries` — API: repeatable `metric` on `/v1/series` (one
   time-axis resolution, N Series), Panel save/read contract with the metrics
   list, unit/cap validation, Baseline cut.
3. `03-spa-combo-panels` — web: composed chart with dual axes, legend with
   per-Series summaries, Panel editor for the Metric list.
