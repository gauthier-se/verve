Status: ready-for-agent
Blocked by: 01

# 03: web: the stack stops being about sleep

## What

- **`lib/sleep.ts` splits.** What is genuinely about sleep (the Stage order, the
  `awake` exclusion, the in-bed fallback's labelling) stays; what is about
  *drawing a Series that carries a breakdown* moves to `lib/breakdown.ts`:
  - `hasBreakdown(series)`: the Series' aggregation is `duration_by_state` or
    `sum_by_state`. The client branches on having a breakdown, never on the
    Metric's name.
  - `segmentsPresent(points)`: today's `stagesPresent`, renamed for what it does.
  - `segmentLabel(metric, key)`: a Stage label for `sleep`, and for a training
    Metric the Activity's curated label from `lib/activities.ts`, which the
    Workouts page already reads. An unknown Activity falls back to its prettified
    slug, the same rule the server's Activity table states (ADR 0002), and
    `other` is labelled "Other activities" rather than left as a slug, since it
    is Verve's own bucket rather than a source's word.
  - Its own `breakdown.test.ts` beside `sleep.test.ts`: the order is stable, an
    unknown Activity prettifies, `other` sorts last whatever its size, and a
    Series with no breakdown reports none.
- **`chart-data.ts` keeps one path.** Line 43 already writes `p.states` into the
  chart datum; it stops being commented as a sleep concern and starts being the
  breakdown path, with the stacking colours read from the long ramp both Stages
  and History events already use (ADR 0036). `other` takes the sixth slot by
  convention, so it is the same colour on every chart.
- **The value formatter follows the unit, not the Metric.** Minutes render
  through `formatDuration` (sleep and `training_time` both), kilometres through
  `formatExact` with the unit beside it. This is the one place where the two
  by-state rules genuinely differ, and the unit is what tells them apart.
- **The Metric page and the Panel need nothing new** beyond the above: both read
  `useSeries` and render what comes back, which is why this issue is a rename and
  a split rather than a feature. Confirm the tooltip, the legend and the
  `LedgerDetailTable`'s per-segment columns all read the new labels, since that
  table grows one column per segment present.
- **The empty state says what is missing.** An Account with no workouts asking
  for `training_time` gets an empty Series, and the Panel's existing empty state
  is the right one; check it does not say "no readings", which is a measurement
  word, or fix it to the neutral phrasing the other families use.

## Why here

The sleep milestone wrote the stack as a sleep feature because it was the only
one. The second by-state Metric is what turns that into a shape, and the rename
is the whole work: `stagesPresent` called from a training chart is a function
whose name lies about what it is drawing, which is how a reader learns the wrong
model of the code.

The labels come from the Activity catalog rather than from a second table in the
client for the reason the server keeps that table at all: a curated label, an
icon and a group are one set of facts, and the Workouts page already reads them.
