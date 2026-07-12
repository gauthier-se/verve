# 01 — Query engine + API: the Panel summary field

Status: done
Blocked by: —

## Goal

Compute the **Panel summary** server-side and carry it on the `Series`, for the
current window and — in comparison mode — the Baseline, so the client renders the
headline figure and the delta without ever re-folding buckets (ADR 0012, ADR 0019).

## Scope

- **`Series` contract** (`internal/query/query.go`, mirrored in `web/src/lib/types.ts`
  under issue 02): add `Summary *Point` (`json:"summary,omitempty"`). A **nil**
  Summary is a gap ("—"): no data in the window, or a derived Metric missing a
  required operand over the window. Reusing `Point` means an `average` summary can
  carry the window's overall `min`/`max` band, just like a bucket.
- **Definition — "a single bucket spanning the range".** The summary is the Metric
  aggregated over `[req.From, req.To]` with **no bucketing** (a single row over the
  whole window), *not* a fold of `out.Points`:
  - `sum` → `SUM` over the window; `latest` → the last value in the window;
    `average` → `AVG`/min/max over the **raw rows** (a true count-weighted mean —
    this is the whole reason it is server-side).
  - **Derived** (`seriesDerived`): resolve each operand as a **single** window value
    (its own rule over `[from,to]`), then apply the Formula **once**. Any required
    operand (or the whole denominator) empty over the window → nil Summary (the
    ADR 0014 gap rule, at window scope). Factor the per-bucket combine so the same
    Formula code serves one window value.
- **Wire-up**: `Series` sets `out.Summary` before returning; `Compare` sets it on
  both `cmp.Current` and `cmp.Baseline` so the delta has both operands. The
  `handleSeries` envelope is unchanged (summary rides inside each series object).

## Out of scope

- The delta arithmetic and formatting — computed client-side from the two summaries
  (issue 02). The server ships the two numbers, not their difference.
- `duration_by_state` — still unsupported by the engine; no summary for it.
- Any change to bucket points, alignment, or the comparison envelope shape.

## Acceptance

- `sum`/`latest`/`average` return a `Summary` equal to the Metric aggregated over the
  whole window; the `average` summary is count-weighted, **not** the mean of
  `Points[].value` (a test with unequal per-day sample counts pins the difference).
- A derived Metric's summary equals `Formula(operands aggregated over the window)`;
  a window with a required operand absent yields a nil Summary, never zero.
- An empty range yields a nil Summary (and still an empty, non-nil `Points`).
- In comparison mode both `Current.Summary` and `Baseline.Summary` are populated
  (or nil by the same rule).
- Existing series/compare tests still pass; JSON omits `summary` when nil.

## Refs

ADR 0019, ADR 0012, ADR 0014. CONTEXT.md: Panel summary.
`internal/query/query.go` (`Series`, `seriesDerived`, `aggregate`, `resolveOperand`,
`Compare`), `internal/api/handlers.go` (`handleSeries`).

## Comments

Implemented on branch `feat/panel-summary`.

- `Series` gained `Summary *Point` (`json:"summary,omitempty"`); nil = gap ("—").
- `summarize` folds the whole window with no GROUP BY: `SUM` / last-row / `AVG,MIN,MAX`
  over the raw rows — the `average` case is a true count-weighted mean, verified by
  `TestSummaryAverageIsCountWeightedNotMeanOfMeans` (90, not the biased 120).
- `summarizeDerived` folds each operand over the window then applies the Formula once;
  a missing required operand yields nil (`TestSummaryDerivedMissingOperandIsNil`). The
  period-ratio case is pinned by `TestSummaryDerivedRatioIsWindowRatioNotMeanOfRatios`
  (6, not 2.5).
- `Compare` gets both summaries for free (it calls `Series` for each window);
  `TestCompareCarriesBothSummaries`.
- Full suite green; `summary` is omitted when nil, so the existing API contract tests
  pass unchanged.
