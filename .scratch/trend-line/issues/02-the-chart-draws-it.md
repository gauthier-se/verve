Status: done
Blocked by: 01

# 02: web: draw the trend and name the rate apart from it

## What

### `web/src/lib/types.ts`

`Point` gains `trend?: number`, `Series` gains `trend?: Trend`, both documented with
the same distinction the server makes: the line is an EWMA, the rate is a
least-squares fit, and they are not the same estimator.

### `web/src/components/panel-chart.tsx`

The chart is already a Recharts `ComposedChart`, so the line is one more child beside
the existing `<Line>`/`<Area>`/`<Bar>` branches:

```tsx
<Line
  yAxisId={yAxisId}
  type="monotone"
  dataKey={trendKey}
  stroke={color}
  strokeWidth={2}
  strokeDasharray="4 3"
  dot={false}
  connectNulls={false}
/>
```

Three things that matter more than they look:

- **`connectNulls={false}`** is the whole of ADR 0032 at this layer. Default Recharts
  bridges nulls, which would draw the smoothed line straight across two months
  nobody weighed.
- **Same hue, dashed, and the raw series demoted rather than the trend promoted.**
  The reading is still the evidence and must stay visible; drop it to a thin line or
  faint dots so the trend reads as the signal and the readings as the scatter around
  it. A second colour would say these are two Metrics (ADR 0020's grammar), and they
  are one.
- **The tooltip shows both**, labelled, because the gap between them on a given day
  is information: "90.4 kg · trend 89.7" tells the Account that this morning was a
  heavy one, which is exactly the reading it is meant to stop over-reacting to.

`mergeSeries` in `web/src/lib/chart-data.ts` needs the trend keyed per Metric the way
values already are, so a dual-axis Panel with two Latest Metrics keeps two trends
apart.

### `web/src/components/panel-summary.tsx` and `metric-page.tsx`

The rate is the number the Account came for, so it goes where the summary figure is,
in the Metric's unit and per week: **−0.65 kg/week**. Beside it, quieter, the fit
quality — `r² 0.86` or words for it. A rate with no r² is a claim with its evidence
removed, and the PRD's argument for showing a weak trend rather than hiding it only
works if the number that makes it weak is shown too.

Label the rate as spanning the window, not as the current gradient. "−0.65 kg/week
over this range" is one word longer and stops an Account from reading it as today's
speed when the line is visibly flattening.

### Interaction with the Plan page

`plan-page.tsx` already prints a rate from `ActualRate` over its own 28-day window.
Once a Panel prints one over the Account's chosen range, the two will differ
whenever the ranges differ, and that is fine — but each must say its window, or the
Account sees Verve contradict itself. Check the Plan page's copy says "over 28 days"
as plainly as the Panel will say "over this range".

## Tests

- A Vitest case that a series with a null trend on one bucket renders a broken line
  rather than a bridged one, since `connectNulls` defaulting back is a silent
  regression that no type would catch.
- A case that two Latest Metrics on one Panel keep distinct trend keys through
  `mergeSeries`.

## Comments

**Implemented.** One departure worth flagging:

- **The trend line is solid, not dashed.** The spec said dashed; at 2px over a
  three-month range a 4/3 dash reads as noise rather than as a line. What separates
  the two marks instead is weight and opacity: the readings drop to 1px at 35%, the
  trend keeps the full 2px, same hue. That is the arrangement the spec actually
  argued for ("the raw series demoted rather than the trend promoted"), and it is what
  weight-trend tools converged on.

`connectNulls={false}` is in place and covered by a chart-data test asserting that a
trendless bucket produces **no key** rather than a zero.

The rate lives under the hero figure on the Metric page as `TrendRate`, labelled
"across this range" with r² beside it, rather than inside `PanelSummary` — the
summary component is shared with Panels, which carry no trend, and threading an
optional through it would have put a Metric-page concern in a Panel's component.

The Plan page needed no change: its own rate copy already reads "from N weigh-ins over
M days", so the two windows are each named and cannot be read as one.
