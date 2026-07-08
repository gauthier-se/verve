# 04 — SPA: comparison control + baseline overlay

Status: ready-for-agent
Blocked by: 03

## Goal

Let a user turn on comparison for a Dashboard and see the baseline overlaid on
every Panel as one consistent recessed line.

## Scope

- **Comparison control** (Dashboard toolbar, next to the Time range): pick a
  **Baseline rule** — Off (`none`) / Previous period / Same period last year /
  Custom (with a date-range picker for the absolute bounds). Persists to the
  Dashboard (03) and is **disabled/greyed when the range is `all`**.
- **Request wiring**: each Panel's `/v1/series` call passes the Dashboard's
  `baseline` (+ bounds for `custom`); consume the returned baseline series.
- **Baseline rendering**: overlay the baseline as **one uniform muted/dashed
  line** on top of the Panel's native mark — the same treatment for `bar`,
  `line`, `area`, `band` (baseline = its centre line only, no min/max band),
  `stacked_bar` (baseline = the total), and the diverging bar (a line crossing
  zero). The x-axis is ordinal position within the period; the tooltip shows both
  the current and the baseline bucket's **own real dates**.
- **Empty baseline**: no baseline line drawn (gaps), no error state.

## Out of scope

Any computed delta / "+% vs" figure. Per-Panel comparison toggles. Per-chart-type
overlays (grouped bars). Trends.

## Acceptance

- Turning on "Previous period" on a Dashboard overlays a muted baseline line on
  every Panel; turning it off returns to a single series.
- The control is disabled when the range is `all`.
- "Custom" reveals a date-range picker; its bounds persist and reload with the
  Dashboard.
- A bar Panel, a band Panel (e.g. heart rate), and a diverging-bar Panel (e.g.
  `calorie_balance`) each render a single readable baseline line; the tooltip
  shows the current and baseline dates side by side.
- Reloading the Dashboard preserves the chosen baseline rule and bounds.

## Refs

ADR 0012 (charts), 0013 (client SPA), 0015 (comparison).
CONTEXT.md: Baseline, Baseline rule, Ordinal alignment.
