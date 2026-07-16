# 02 — API: multi-Series /v1/series + Panel metrics contract

Status: done
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

## Comments

Implemented on branch `feat/panel-metrics`.

- `/v1/series` takes 1–4 `metric` params; >1 → `series` is an **array** (one
  `query.Series` per metric, request order), the time axis resolved once.
  Exactly one metric keeps today's envelope byte-for-byte (object, baseline as
  before) — no version bump needed.
- **Baseline cut** server-side for >1 metric even when `baseline_rule` is set
  (`TestSeriesMultiMetricCutsBaseline`).
- **Bucket alignment nuance vs the acceptance text**: Series stay *sparse* (a
  data-less bucket is a gap, ADR 0014 — never a padded zero), so the guarantee
  is "one shared bucket grid", not "equal-length point lists"; a date present
  in two Series names the same bucket. Pinned by
  `TestSeriesMultiMetricSharedBuckets`.
- Panel contract: `metrics: [{metric, chart_type?}]` on create and update
  (update replaces the whole list); `validatePanelMetrics` enforces 1–4
  entries, Catalog membership, ≤2 canonical units, chart-type compatibility,
  with defaults from each aggregation rule. The legacy scalar `metric`/
  `chart_type` shape still works and keeps its historical error keys;
  `panelView` exposes both the `metrics` list and the legacy scalar mirror of
  the first entry until the SPA cutover (issue 03, where the scalars drop).
- Read path (`/v1/series`) enforces only the cap of 4, not the unit cap — the
  unit constraint is about rendering axes, and the editor needs free preview.
- Review fixes applied: cap error messages derive from the constants; an
  explicit `{"metrics":[]}` errors under `metrics` (nil = legacy shape, empty =
  list shape) on create and update.
- Review judgements left as-is: duplicate slugs are accepted (harmless, spec
  silent); `Validator` keeps one message per key so multi-entry errors surface
  one at a time; a co-sent legacy `chart_type` is ignored when `metrics` is
  present (list wins, commented in the handler); the three fetch/write blocks
  in `handleSeries` stay separate — the envelope shapes genuinely differ.
