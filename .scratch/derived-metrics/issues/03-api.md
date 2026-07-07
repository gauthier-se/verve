# 03 — API: expose derived Metrics

Status: resolved
Blocked by: 02

## Goal

Surface derived Metrics through the JSON API so the SPA can list them, show what
they mean, and graph them — with no special-casing beyond the metric metadata.

## Scope

- **`GET /v1/metrics`**: include derived Metrics. Each derived view reports
  `nature: "derived"`, its unit, a **signed** flag, and its **Formula** in a
  readable, structured form (operand slugs + coefficients + scale) for a tooltip.
  A derived Metric reports **no aggregation** (empty, not a fake rule).
- **`GET /v1/series`**: accept a derived slug and return the engine's per-bucket
  derived series (from issue 02) in the existing envelope. Same validation and
  error mapping as imported Metrics (unknown slug, bucket cap, range).
- **Chart hint**: expose enough for the SPA to default a signed Metric to a
  diverging bar with a zero baseline (e.g. the signed flag drives the default
  chart type, mirroring how aggregation drives it for imported Metrics).

## Out of scope

Engine internals (02), rendering (04). Editing formulas.

## Acceptance

- `GET /v1/metrics` lists `calorie_balance` with `nature=derived`, `signed=true`,
  unit `kcal`, and its Formula; no `aggregation`.
- `GET /v1/series?metric=calorie_balance&range=30d&bucket=day` returns signed
  daily buckets with gaps preserved.
- Creating a Panel on a derived slug defaults to a diverging bar for a signed
  Metric.

## Refs

ADR 0014. internal/api/handlers.go (handleMetrics, handleSeries), dashboardhandlers.go.
