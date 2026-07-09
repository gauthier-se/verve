# 02 — internal/timeaxis module

Status: ready-for-agent
Blocked by: 01

## Goal

A pure, DB-free module that resolves a Dashboard's temporal tokens into concrete
windows and a bucket, and validates those tokens. One test surface for every
temporal rule currently split across client and server.

## Scope

- **Types**: `Tokens` (RangePreset, RangeFrom/To, BaselineRule, BaselineFrom/To,
  Bucket override), `Window{From, To}`, `Resolved{Current Window, Bucket
  query.Bucket, Baseline *Window}`.
- **`Validate(Tokens) error`**: closed sets for presets (7d/30d/3m/1y/all/custom)
  and baseline rules (none/previous/same_period_last_year/custom); custom needs
  ordered day-granularity bounds; non-custom carries no bounds; an override bucket
  is day/week/month. Returns field-keyed errors the API maps to 422.
- **`Resolve(Tokens, now) (Resolved, error)`**: validates, then resolves —
  preset→current window (`to = now`; `all` → `ALL_FLOOR`), span→bucket
  (`autoBucket`: ≤31d→day, ≤366d→week, else month), override wins when present;
  baseline rule→window (moved from `query.baselineWindow`), `none`/`all` →
  `Baseline=nil`; a rule with `range=all` is a validation error.
- Owns `ALL_FLOOR` and the `autoBucket` thresholds (moved from the SPA).
- Imports `internal/query` only for the `Bucket` type.

## Out of scope

Wiring into handlers or the engine (03). No SQL. No SPA change. `maxPoints` stays
in `query`.

## Acceptance

- `Resolve` unit-tested with token structs only — no HTTP, no DB: each preset and
  rule, custom bounds, override bucket, `all`→no baseline, rule+`all`→error,
  same_period_last_year leap-day normalization.
- `Validate` rejects unknown preset/rule, bounds on a non-custom rule, unordered
  custom bounds, bad override bucket — with the same field keys the current
  `validateRange`/`validateBaseline` use.
- Package is unused by the rest of the tree at this point (wired in 03).

## Refs

ADR 0012, 0015. CONTEXT.md: Time axis, Time range, Baseline, Baseline rule.
Sources to fold in: `web/src/lib/time-range.ts`, `internal/api/handlers.go`
(`parseRange`/`parseTimeRange`), `internal/query/baseline.go` (`baselineWindow`),
`internal/api/dashboardhandlers.go` (`validateRange`/`validateBaseline`).
