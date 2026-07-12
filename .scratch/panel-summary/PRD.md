# PRD — Panel summary

## Goal

Make each Panel's **magnitude** legible at a glance. Today a Panel is curve-only:
the shape of the trend is visible, but the actual numbers live only in the hover
tooltip (and the Y axis is deliberately terse). You can see *that* steps rose, not
*how many*. Give every Panel a headline figure — the **Panel summary** — above its
curve.

Context, glossary, and the design decision: see `CONTEXT.md` (**Panel summary**)
and `docs/adr/0019`.

## What this milestone does

- Adds a **Panel summary** to every Panel: a persistent, universal headline figure
  in a dedicated band between the header and the curve. No per-Panel toggle.
- **Primary figure** — the Metric folded over the whole Time range, per its
  aggregation rule, shown large. Defined as **a single bucket spanning the range**:
  - `average` → a true count-weighted mean over the raw data (not a biased mean of
    per-bucket means).
  - `sum` → the range total; `latest` → the last value (primary and secondary
    coincide, one figure).
  - derived → each operand aggregated over the window, *then* the Formula applied
    once; an all-empty operand makes the summary a **gap** ("—"), never zero
    (ADR 0014).
- **Secondary figure** — the most recent bucket's value, shown small beside it. Not
  a summary: it is `points[last]`, a plain client-side read.
- **Delta** — in **period comparison** only, the primary figure carries a delta
  against the Baseline's own summary: direction + magnitude, **neutral** (never
  colored good/bad), percentage by default, absolute for a signed Metric.
- **Server** computes the summary for the current *and* Baseline series and returns
  it on `/v1/series` (new `summary` field). The client never re-folds buckets
  (ADR 0012, ADR 0019).
- **Format** — compact-but-honest, driven by the aggregation rule (large `sum`
  abbreviated "245 k", small/`latest` at full precision "74,2 kg", "58"); FR locale
  (comma decimal, space thousands); exact value in the tooltip.
- **Degradation** — empty range shows "—"; the partial current bucket is unmarked;
  on a narrow 1-column Panel the secondary figure drops before the delta, and the
  primary figure never drops.

## What this milestone does NOT do

- **Data labels on marks** — no numbers printed on individual bars/points. The
  headline figure is the fix, not per-bucket labels.
- **A "stat" Panel type** — the summary is universal, not an opt-in card variant.
- **Coloring the delta good/bad** — Verve does not know which direction is good for
  a Metric; the delta is neutral (consistent with the uncolored Baseline, ADR 0015).
- **`duration_by_state`** — sleep is not served by the query engine yet; its summary
  lands with the sleep slice.
- **Configurable primary/secondary** — the hierarchy (aggregate large, latest small)
  is fixed, not a user choice.

## Issues

1. `01-query-summary` — server: compute + serve the `summary` field (current +
   Baseline).
2. `02-web-panel-summary` — web: the summary band, hierarchy, delta, FR formatting,
   narrow-Panel degradation.
