# 03 — API: baseline series + Dashboard payload

Status: ready-for-agent
Blocked by: 01, 02

## Goal

Expose period comparison over HTTP: serve both windows from one `/v1/series`
request, and read/write the Dashboard's baseline fields.

## Scope

- **`/v1/series` baseline param** (`internal/api`): accept
  `baseline=previous|same_period_last_year|custom` (+ `baseline_from`/
  `baseline_to` when `custom`). When present, call the engine's baseline path
  (02) and return, alongside the current series, a **baseline series** — equal
  length, index-aligned, each bucket carrying its own real date. Absent param →
  today's single-series response, unchanged.
- **Reject comparison for range `all`**: a baseline param with an `all` range is a
  validation error (nothing precedes "all"). `1y` is accepted (rules coincide).
- **Dashboard payload** (`internal/api/dashboardhandlers.go`): read and write
  `baseline_rule`/`baseline_from`/`baseline_to` in the Dashboard JSON, validated
  as in 01 (bounds only with `custom`).

## Out of scope

Engine internals (02), column storage (01), SPA (04). No delta field in the
response. No per-Panel baseline.

## Acceptance

- `GET /v1/series?metric=steps&range=30d&baseline=previous` returns current +
  baseline arrays of equal length, index-aligned, each baseline bucket with its
  own date; without `baseline` the response is byte-for-byte today's shape.
- `baseline=custom` requires both bounds; missing/ill-formed bounds → 400.
- Any `baseline` with `range=all` → 400.
- Creating/updating a Dashboard persists and returns its baseline fields; bounds
  supplied under a non-`custom` rule → 400.
- Derived slugs (`calorie_balance`) serve a baseline series too, with empty
  Source.

## Refs

ADR 0012 (bucket API), 0015 (comparison). CONTEXT.md: Baseline, Baseline rule.
`internal/api/handlers.go`, `internal/api/dashboardhandlers.go`.
