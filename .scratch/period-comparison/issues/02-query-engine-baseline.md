# 02 — Query engine: baseline window + ordinal alignment

Status: ready-for-agent
Blocked by: —

## Goal

Teach the query engine to answer a series request that carries a **Baseline**:
resolve the baseline window from its rule and return it aligned to the current
series by ordinal position, truncated to the shorter window.

## Scope

- **Baseline-window resolver** (`internal/query`): given the current range and a
  baseline rule, compute the concrete baseline `from`/`to`, calendar-aware:
  - `previous` — shift back by the current range's own length.
  - `same_period_last_year` — shift back exactly one year.
  - `custom` — use the absolute `from`/`to` as given.
- **Reuse the existing bucket query** for the baseline window at the same bucket
  granularity as the current window — including the derived path unchanged (each
  operand resolves its own winning Source, ADR 0003; `Series.Source` empty).
- **Ordinal alignment**: align current and baseline buckets by position (index),
  not date, and **truncate both to the shorter** length. Each baseline bucket
  retains its own real bucket timestamp (for tooltips); it is *not* relabelled to
  the current window's date.
- **Gaps unchanged**: a baseline bucket with no data stays a gap; an entirely
  empty baseline window yields an all-gap (effectively absent) baseline series.

## Out of scope

Schema/columns (01), HTTP param + payload (03), rendering (04). The `all` range
carries no baseline (guarded upstream). No delta computation.

## Acceptance

- `previous` over a 30d range returns a baseline series for the prior 30 days;
  `same_period_last_year` returns the window one year earlier; both at the current
  bucket granularity.
- Current and baseline series come back **equal length**, index-aligned; a longer
  baseline (custom span, or a leap-day day-bucket boundary) is truncated to the
  current window's length.
- Each baseline bucket exposes its own real date, distinct from the aligned
  current bucket's date.
- A baseline window with no data returns a baseline series of gaps, not zeros; a
  derived Metric's baseline recomputes per bucket from operands (not from
  re-aggregated derived values).

## Refs

ADR 0003 (source priority), 0014 (derived), 0015 (comparison).
CONTEXT.md: Baseline, Baseline rule, Ordinal alignment. `internal/query/query.go`.
