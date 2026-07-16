# 03 — Web: combo Panels, dual axes, per-Series legend summaries

Status: done
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

## Comments

Implemented on branch `feat/panel-metrics`.

- SPA fully cut over to the `metrics` list: `Panel`/`PanelMetric` types, one
  `useSeries` call per Panel with repeated `metric` params (envelope normalized
  to a list client-side), `AddPanelDialog` posts `metrics: [{metric}]`.
- `PanelChart` merges the sparse Series by bucket date (one shared grid,
  ADR 0020/0014), renders each Series' own mark at its position color
  (`--chart-1..4`), grouped on left/right Y axes by unit (first Metric's unit
  left). Single-Metric rendering — including the index-aligned Baseline overlay
  and diverging-bar sign colors — is byte-identical in behavior; in a combo,
  identity wins over polarity (a diverging bar wears its series color, keeps
  its zero line).
- `PanelLegend` (multi only): swatch + name + per-Series summary in the legend
  band; single-Metric keeps the large headline (ADR 0019). With comparison on,
  a muted "no baseline" hint explains the cut instead of looking broken.
- Editor in the Panel settings popover: add (server-side default chart type by
  omitting `chart_type`), remove, chevron reorder, per-Metric chart-type
  select; candidates that would breach ≤4 metrics / ≤2 units are filtered out —
  the server stays the authority.
- 4th categorical color added (`--chart-4`, rose): validated with the dataviz
  six-checks script in both modes. Pre-existing note: dark-mode `--chart-2`/
  `--chart-3` sit above the validator's lightness band — untouched here to
  avoid repainting existing charts; worth a follow-up.
- Deliberate tension: the dataviz skill forbids dual-axis charts; ADR 0020
  chose dual axes over normalization to keep magnitude legible. The ADR wins.
- Verified end-to-end against the live binary (fresh DB → migration 0007 →
  bootstrap → seeded panel carries `metrics` → 3-Metric panel created with
  per-rule defaults → multi series, baseline cut, units kcal/kg → 3-unit
  rejection). Visual rendering not eyeballed (no browser in this session) —
  worth a quick human look at a combo panel in both themes.
- Not done here: dropping the legacy scalar `metric`/`chart_type` from the
  server's `panelView` and inputs (the SPA no longer uses them) — tiny server
  cleanup, left for a follow-up commit so this one stays web-only.
