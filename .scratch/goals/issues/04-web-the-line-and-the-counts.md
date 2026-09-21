Status: ready-for-agent
Blocked by: 02

# 04: web: the line and the counts on a Panel

## What

- **The line** (`panel-chart.tsx`): drawn from `series.goal.segments`, only when
  the bucket is `day`. A dashed horizontal step in the Series' own colour (ADR
  0036, ADR 0042), on the Series' own axis on a dual-axis Panel (ADR 0020). It
  steps where the Goal changed and breaks where none was in force. It must not
  read as the Baseline, which is a recessed curve, not a plateau.
- **The value axis extends to include every segment's value**, so a Goal outside
  the data's range is still visible.
- **The counts** (`panel-summary.tsx`), in the legend beside each Series'
  summary: "18 of 24 measured days ≥ 7 500", the covered count available on
  hover or in the full legend. When the Goal changed within the window, the
  bound is not restated ("18 of 24 measured days at goal"). In comparison, the
  Baseline's counts sit beside the current ones, with no delta computed between
  them.
- **Above day grain**: no line, the counts stay.
- **The Metric page chart** gets both, through the same components.
- **No valence**: points keep their colour, no shading on either side of the
  line, no tick, no icon.

## Tests

- the line is absent at week grain and the counts present;
- a Goal far above the data widens the axis;
- two segments draw a step, a gap between segments draws nothing;
- the Baseline's counts are shown and no difference is computed.

## Why here

The line is the part people see first, and the one most likely to slide into
judgement. Keeping the rendering to a line and a count is what the ADR's
"Verve counts" means on screen.
