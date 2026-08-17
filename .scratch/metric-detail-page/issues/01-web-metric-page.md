Status: done

# 01 — web: the Metric page

## What

- New route `/data/$metric` → `MetricPage` (`web/src/components/metric-page.tsx`).
- `MetricPage`:
  - Header: back-to-Data link, `MetricIcon` + `metricLabel`, `FormulaHint` if
    the Metric is derived.
  - Local range state (7D/30D/3M/1Y/All), same `RANGE_PRESETS` as the Data
    page; `bucket: null` so the server auto-derives it, same as a Panel.
  - `useSeries({ metrics: [metric], range, bucket: null })` for one Series.
  - `PanelSummary` band (reused, no baseline prop).
  - `PanelChart` (reused, single-Series `list`, chart type from
    `defaultChartType`).
  - "Highs & lows" row: max/min over `points[].value` in the current window,
    plain client-side reduce (a gap is already absent from `points`, so no
    special-casing).
  - `LedgerDetailTable` underneath, unchanged, fed the same `range`.
  - Unknown metric slug (bad deep link) → short "not in the Catalog" message
    with a link back to `/data`, no crash.
- Ledger `Scoreboard` row: drop the `expanded`/`onToggle` inline-toggle state
  and the inline `LedgerDetailTable`; each row becomes a `Link` to
  `/data/$metric`. Chevron becomes a plain disclosure indicator (→), not a
  toggle.

## Why here

Everything the page needs is already served by `GET /v1/series` (points,
`summary`, `mean`, `days`) and already has client components (`PanelSummary`,
`PanelChart`, `LedgerDetailTable`) built for the Dashboard and the Ledger. This
issue is composition, not new data plumbing.

## Comments
