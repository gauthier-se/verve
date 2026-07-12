# 02 — Web: the Panel summary band

Status: done
Blocked by: 01

## Goal

Render the **Panel summary** on every Panel: a dedicated band between the header and
the curve, with the large primary figure, the small secondary figure, and — in
comparison mode — a neutral delta. FR formatting; graceful degradation on a narrow
Panel (ADR 0019, CONTEXT.md: Panel summary).

## Scope

- **Types** (`web/src/lib/types.ts`): add `summary?: Point` to `Series` (mirrors
  issue 01). Absent/undefined = gap.
- **Summary band** — a new element in `panel-card.tsx`, between the header
  (`border-b`) block and the chart body, universal on every Panel (no setting). The
  chart body keeps `flex-1`; the band takes its own fixed row (the `h-72` card loses
  ~40px of curve, as chosen).
  - **Primary (large)** — `series.summary`, formatted per aggregation rule (below),
    with the unit. Nil summary → "—".
  - **Secondary (small, beside)** — the last bucket value, `points[points.length-1]`
    (a plain read, no server field). Hidden when there are no points, or when it
    equals the primary (a `latest` Metric, where they coincide).
  - **Delta** — only when a `baseline` series is present: compare
    `series.summary` to `baseline.summary`. Direction (↑/↓) + magnitude, **neutral
    color** (never green/red). Percentage by default; **absolute** for a signed
    Metric (`metric.signed`, ADR 0014) where a percentage around zero is meaningless.
    Absolute value available in the summary tooltip. If either summary is a gap, show
    no delta.
- **Formatting** (`web/src/lib/`): a summary formatter, compact-but-honest, keyed by
  aggregation and FR locale (`Intl.NumberFormat('fr-FR')`): large `sum` abbreviated
  ("245 k"), `average`/small at full precision ("58"), `latest` with its decimals
  ("74,2 kg"). Comma decimal, space thousands. The **exact** value shows in the
  tooltip. Keep the terse axis `formatValue` as-is for the chart; the summary is a
  separate, richer formatter.
- **Narrow-Panel degradation** — on a 1-column Panel, when the row overflows, drop
  the **secondary** figure first; keep the delta; the primary figure never drops.
  (Priority: primary > delta > secondary.)

## Out of scope

- Server computation of the summary (issue 01).
- Data labels on marks; a "stat" Panel type; per-Panel toggle for the summary — all
  explicitly rejected (ADR 0019, PRD).
- Marking the partial current bucket — the secondary figure is shown unadorned.

## Acceptance

- Every Panel shows a summary band; a Panel with data shows a formatted primary
  figure, an empty range shows "—".
- A `latest` Metric shows a single figure (no redundant secondary).
- With comparison on, a neutral delta appears against the Baseline's summary;
  percentage for unsigned, absolute for signed; hidden when either side is a gap.
- Numbers render in FR locale (comma decimal, space thousands); the exact value is in
  the tooltip.
- On a 1-column Panel the secondary figure is the first to drop; the primary never
  drops.

## Refs

ADR 0019, ADR 0015, ADR 0014. CONTEXT.md: Panel summary.
`web/src/components/panel-card.tsx`, `web/src/components/panel-chart.tsx`,
`web/src/lib/types.ts`, `web/src/lib/metrics.ts`.
