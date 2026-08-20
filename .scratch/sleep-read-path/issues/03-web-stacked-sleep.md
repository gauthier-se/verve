Status: done

# 03: web: the stacked Night, its numbers, and its headline

Depends on 01, 02.

## What

- **Types** (`web/src/lib/types.ts`): `Point.states?: Record<string, number>`,
  `Series.nights?: number`. `ChartType` and the `duration_by_state` arms of
  `defaultChartType` / `compatibleChartTypes` already exist and already say
  `stacked_bar` — nothing to add there, which is the point.

- **A Stage vocabulary** in `web/src/lib/metrics.ts` (or a small
  `web/src/lib/sleep.ts` if it crowds the file): the display order
  `asleep_deep`, `asleep_core`, `asleep_rem`, `asleep`, `awake`, `in_bed`,
  bottom to top, and their labels ("Deep", "Core", "REM", "Asleep", "Awake",
  "In bed"). Colours come from `SERIES_COLORS` by index in that fixed order, so
  a Stage keeps its colour whatever a given night contains and every Palette's
  verified four-way separation (ADR 0026) carries over unchanged.

- **`formatDuration(minutes)`** in `web/src/lib/format.ts`: `432 → "7h 12m"`,
  `47 → "47m"`, `0 → "0m"`. Used by every sleep figure — headline, tooltip,
  Ledger cell, axis tick — so one rule renders minutes everywhere. `sleep`'s
  unit is `min`, so this keys off the unit, not off the Metric slug.

- **`panel-chart.tsx`**, the branch at line 261 finally implemented:
  - when the Panel's sole Metric is `duration_by_state`, emit one `<Bar>` per
    Stage present in the window, sharing a `stackId`, in the fixed order, each
    reading its own `states` key off the datum;
  - when the Panel carries more than one Metric, keep the current single `<Bar>`
    of `value` in the Series' position colour (the PRD's rule: a stack owns the
    ramp only when it owns the Panel), and update the comment to say so instead
    of describing a deferral that no longer exists;
  - `ChartDatum` gains the per-Stage keys for the stacked case;
  - the Y axis formats with `formatDuration`;
  - the Baseline overlay is untouched: it stays one recessed line of total time
    asleep (ADR 0015), which is what a comparison of two windows can honestly
    show.

- **The tooltip**: for a stacked Night, one row per Stage with its swatch and
  duration, then the night's total asleep. Not the bare `value` a stacked chart
  would otherwise show under the cursor.

- **`panel-summary.tsx`**: a `per_night` `SummaryMode` beside `per_day`,
  selected for a `duration_by_state` Metric when the average toggle is on, and
  computing `summary.value / nights` — never `/ days`. The label reads
  "/ night". `isExtensive` returns true for `duration_by_state` (time asleep
  does scale with the window), so only the divisor differs.

- **`ledger-detail-table.tsx`**: an `isDurationByState` branch beside the
  existing `isAverage` one, adding one column per Stage present across the
  points, values rendered with `formatDuration`, and included in the TSV the
  table's Copy already puts on the clipboard (`copyTsv`). The header row uses
  the Stage labels.

- **The Metric page** needs nothing: it renders through `PanelChart` with
  `defaultChartType(meta)` (`web/src/components/metric-page.tsx:95`), which
  already answers `stacked_bar` for a `duration_by_state` Metric — so sleep gets
  its page, and a Pin to it, the moment the Catalog entry exists.

- **The Ledger overview row** for sleep needs no change beyond formatting: its
  week/month cells are minutes and must render as durations, not as "432".

- **Tests** (`web/src/**/*.test.tsx`, following the palette/contract tests'
  style):
  - `formatDuration` boundaries;
  - a single-Metric sleep Panel renders one stacked bar per Stage, in the fixed
    order, and a two-Metric Panel renders one plain bar for sleep;
  - the headline divides by `nights`, not `days` — a fixture where the two
    differ, so a wrong divisor cannot pass;
  - a Night whose only Stage is `in_bed` still renders a bar.

## Why here

The chart is where the milestone is either honest or decorative. Two specific
ways to make it decorative: stacking a Panel's other Metrics into a rainbow by
letting sleep claim the colour ramp, and dividing by `days` because the field is
right there and `nights` is one word away. Both produce a screen that looks
finished. Hence the fixture where nights and days differ, and hence the
two-Metric test.

The Ledger columns matter for the same reason they mattered in ADR 0021: a
stacked bar is the one chart where the values are least readable by eye, so the
promise that the numbers behind the curve can be read exactly and copied out is
load-bearing here rather than nice to have.

## Comments

**There is no JS test runner in this repo.** The spec asked for
`web/src/**/*.test.tsx`; `web/` has no vitest, no jest, and `make ci` runs no
front-end tests — the only "web" test is `internal/web/palette_test.go`, a Go
test that reads the stylesheet as text. Adding a runner is a real decision with
a CI cost and it does not belong inside a feature branch, so this issue did not
make it.

What it did instead is the one contract worth pinning across the two languages:
`internal/web/sleepstages_test.go`, in the palette test's style, reads
`families.go` and `sleep.ts` and fails if a Stage the Connector can store has no
place in the stack, no label, or no colour slot — the drift whose failure mode
is a segment silently missing from a chart. Verified by mutation: dropping
`asleep_rem` from `SLEEP_STAGES` fails it. The rest of the web work is covered
by `tsc --noEmit`, by the server-side figures it only formats, and by the API
tests behind them.

**Duration formatting reaches further than the chart.** A figure in minutes had
to read as a duration everywhere it surfaces or the Metric page would say "432"
beside a bar labelled "7h 12m": `panel-summary` (headline, legend, delta),
`ledger-detail-table` (value column, Stage columns, the TSV copy),
`data-page` (the overview's latest/week/month/delta cells), `metric-page`
(highest/lowest), and the chart's own Y axis and tooltip. `formatDuration` is
one function; keying it off `aggregation === "duration_by_state"` rather than
off the metric slug is what kept it from becoming six special cases.

**`FigureUnit` and `figureText` were extracted, not invented.** The summary band
and the legend had the same unit-suffix ternary duplicated; sleep would have
made it a third copy. Two small components now, with the per-night divisor as
one branch rather than two.
