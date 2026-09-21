Status: done
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

## Comments

Shipped. `goalLine` in `panel-chart.tsx`, the `goal${i}` key in
`lib/chart-data.ts`, `GoalCounts` under the single-Metric headline and
`LegendGoal` in a combo's legend (`panel-summary.tsx`), and the text rules in
`lib/goals.ts` (`goalAt`, `drawsGoalLine`, `goalBound`, `attainmentText`), with
tests in `chart-data.test.ts` and `goals.test.ts`.

What differs from the spec:

- **No axis code.** The spec asked for the value axis to extend to the Goal.
  Recharts already fits an axis to every series on it, and the Goal line is one,
  so the fit reaches it for free. `axisDomain` keeps only its diverging rule, and
  its comment now says why the Goal needs nothing.
- **The line is a `Line` with `type="stepAfter"` over the chart's own rows**,
  not reference segments: Recharts matches a reference `x` by category equality,
  and a segment's `from` is often a day with no point. The consequence is that
  the line runs between the first and last *drawn* days of a segment, and a
  segment with a single drawn day shows no line (a one-point line has no length).
- **On a combo, a Goal is set on every row**, including rows only another Series
  brought, so the plateau does not break at each of its own Series' gaps.
- **The counts read "18 of 24 days ≥ 7 500"**, "at goal" in place of the bound
  when the Goal changed inside the window, and never "0 of 0": "goal set, nothing
  to count yet" or "no measured day under the goal". The covered count, the
  unmeasured days and "Today is not counted" are in the title.
- **In comparison the Baseline's count reads "(then 12 of 20 days ≥ 7 000)"**
  beside the current one, with no difference computed.

Not driven in a browser (login needs a password typed). `tsc`, the vitest suite
and the production build pass.
