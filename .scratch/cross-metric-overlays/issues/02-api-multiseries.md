# 02 — API: multi-Series /v1/series + Panel metrics contract

Status: ready-for-agent
Blocked by: 01

## Goal

Serve all of a Panel's Series in one call on one provably shared time axis, and
make the Panel save/read contract carry the Metric list with its constraints
(ADR 0020).

## Scope

- **`/v1/series`**: accept a repeatable `metric` query parameter (1–4 values).
  Resolve the time axis **once** (`internal/timeaxis`), then produce one Series
  per Metric via the existing engine; respond with a `series` array (each entry
  carrying its metric slug, unit, points, summary). Exactly one `metric` keeps
  the current single-object envelope (backward compatible) — or version the
  envelope cleanly if the SPA cutover makes the dual shape uglier than a
  uniform array; decide in-code, document in the handler.
- **Baseline cut**: when comparison is requested with >1 `metric`, return no
  baseline series — server-decided, like the `all`-range exclusion (ADR 0015).
- **Panel save/read** (dashboard handlers): the Panel payload carries
  `metrics: [{metric, chart_type}]` ordered; validate 1–4 entries, all slugs in
  the Catalog, **at most two distinct canonical units**, chart types compatible
  with each Metric's aggregation rule. Reject with a clear 4xx envelope.
- Validation lives at the API/save boundary because units come from the
  Catalog; the store (issue 01) only guarantees shape.

## Out of scope

- Rendering, legend, editor (issue 03).
- Any change to bucket computation, alignment, or summary math — the engine
  already does per-Series everything; this issue is fan-out + contract.
- `duration_by_state` Metrics (still unsupported by the engine).

## Acceptance

- `GET /v1/series?metric=steps&metric=body_mass&range_preset=30d` returns two
  Series with identical bucket timestamps and each its own `summary` and unit.
- Single-`metric` requests are byte-compatible with today's contract (existing
  API tests pass unchanged, or are updated only for a deliberate envelope
  version bump).
- Comparison mode + 2 metrics → no baseline series in the response; comparison
  mode + 1 metric → baseline unchanged.
- Panel save rejects: 5 Metrics; 3 distinct units; an unknown slug; an
  incompatible chart type — each with a distinct, readable error.
- Derived Metrics mix freely with imported ones in one request.

## Refs

ADR 0020, ADR 0015, ADR 0012. CONTEXT.md: Panel, Time axis.
`internal/api/handlers.go` (`handleSeries`), `internal/api/dashboardhandlers*.go`,
`internal/timeaxis`, `internal/catalog` (units, rules).
