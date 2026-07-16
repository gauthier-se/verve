# 03 — Web: combo Panels, dual axes, per-Series legend summaries

Status: ready-for-agent
Blocked by: 02

## Goal

Render a multi-Metric Panel as a readable combo chart and let the user compose
one in the Panel editor (ADR 0020).

## Scope

- **Chart**: Recharts `ComposedChart` — each Series rendered with its own mark
  (its per-Metric chart type), on the Y axis of its unit group (`yAxisId`);
  first Metric's unit group left, the other right, each axis labeled with its
  unit. Colors assigned by position in the Panel from the categorical palette,
  deterministic. Tooltip lists every Series' value for the hovered bucket.
- **Legend with per-Series summaries**: each entry = color swatch, Metric name,
  its Series' folded `summary` + unit (smaller type than the single-Metric
  headline figure). Single-Metric Panels keep today's large summary band
  untouched.
- **Baseline**: multi-Metric Panels render no baseline overlay (the API sends
  none) — show the comparison control state honestly (e.g. a muted hint on the
  Panel), never a client-side re-derivation.
- **Panel editor**: manage the ordered Metric list (add/remove/reorder, 1–4),
  per-Metric chart type among compatible types; disable/explain choices that
  would introduce a third unit or a fifth Metric *before* save, mirroring the
  server rule (server stays the authority).
- **Types** (`web/src/lib/types.ts`): mirror the multi-Series envelope and the
  Panel `metrics` list from issue 02.

## Out of scope

- Normalized scales, user-chosen colors, baseline on multi-Metric Panels.
- Stacked bars across Metrics.
- Touching single-Metric rendering beyond routing it through the same data path.

## Acceptance

- A Panel "dietary_energy + total_energy_expenditure (kcal) vs body_mass (kg)"
  renders: two kcal Series on the left axis (bars/line per their types), mass
  as a line on the right axis, three legend entries each with its summary.
- Hovering one bucket shows all Series' values with units in one tooltip.
- With period comparison on, a single-Metric Panel still shows its Baseline and
  delta; a multi-Metric Panel shows none and communicates why.
- The editor cannot produce a Panel the server would reject (and server
  rejections, if forced, surface readably).
- Existing single-Metric Panels look unchanged.

## Refs

ADR 0020, ADR 0019, ADR 0015. CONTEXT.md: Panel, Panel summary, Baseline.
`web/src/` panel components, panel editor, `web/src/lib/types.ts`.
