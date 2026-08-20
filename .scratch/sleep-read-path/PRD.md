# PRD: Sleep read path

## Goal

Show the sleep Verve already holds. `HKCategoryTypeIdentifierSleepAnalysis` has
been imported since the core milestone — one `states` row per stage, with a
start and an end (`internal/connector/applehealth/families.go:11`) — and
nothing reads it. The query engine aggregates measurements only and rejects
`duration_by_state` outright (`internal/query/query.go:515`), no endpoint serves
a State, no screen shows one. This is the largest gap between what a Verve
database contains and what its owner can see.

## The concept

**Sleep is a Metric.** Not a family with its own page, its own endpoint and its
own time axis: a Catalog entry `sleep` (unit `min`, aggregation
`duration_by_state`), read by `GET /v1/series` like every other Metric, with a
per-bucket breakdown by stage carried on each Point.

That decision is the whole milestone. Everything a Metric already gets, sleep
gets for free and without a second implementation: the resolved time axis and
its bucket, the Baseline overlay, Panels and their layout, cross-metric
composition, the Panel summary, Pins, the Metric page, the Ledger. The
alternative — a `/v1/sleep` endpoint feeding a bespoke screen — would rebuild
range resolution, comparison and summaries a second time, for one family, and
would answer none of the questions those mechanics already answer.

What is genuinely new is therefore small and precise: an aggregation that reads
intervals instead of points, a grain that is the **Night** rather than the
calendar day, and a stack instead of a curve. The scaffolding for the last one
is already in the tree — `stacked_bar` is a valid chart type reserved to
`duration_by_state` (`internal/api/dashboardhandlers.go:560`) and
`panel-chart.tsx:261` holds a branch waiting for the values to stack.

### Night

A **Night** is the bucket a sleep interval falls in. It is not a calendar day,
because sleep crosses midnight by construction and a day-grain read would split
every night into two half-nights and call neither one a night.

A Night is a **noon-anchored day keyed on its waking morning**: an interval
belongs to the Night `date(start_at, '+12 hours')`. Both halves of a normal
night land in the same bucket, and the label is the morning you woke up on,
which is the day the rest of the dashboard is talking about — sleep against
that morning's resting heart rate is the comparison people actually make.

The anchor is applied in UTC, like every other bucket in the engine
(`normalizeTime` stores RFC 3339 UTC, `Bucket.sqlExpr` dates in UTC). A daytime
nap starting after 12:00 UTC is therefore attributed to the following Night,
and a far-eastern account's nights shift by one label. Both are consequences of
the engine being UTC-dated, which is a pre-existing property of every Metric and
not this milestone's fight.

### Stage

A **Stage** is a `state_value` of the `sleep` kind: `asleep_deep`,
`asleep_core`, `asleep_rem`, `asleep` (unspecified), `awake`, `in_bed`. A
Night's Point carries the minutes per Stage; the Point's own `value` is
**time asleep**, the sum of the `asleep*` Stages. `awake` is reported in the
breakdown, so the stack shows the interruptions, but never counted as sleep.

`in_bed` is not a Stage of the same nature: it is the container the others sit
inside, and an iPhone records it over the very same minutes a Watch records
stages in. Summing it would double every night. The rule is **per Night, the
richest evidence wins**: a Night with any `asleep*` row drops its `in_bed` rows;
a Night with none keeps `in_bed` as its single Stage, which is exactly the
iPhone-only account's whole sleep history. The grain is the Night for the same
reason the Manual overlay's grain is the day (CONTEXT.md: Manual overlay): it is
the grain at which the evidence actually changes.

This also settles Source resolution for sleep, which whole-window ranking
cannot: Watch and iPhone are complementary here rather than overlapping — one
has the stages, the other has the nights the Watch was on its charger. Ranking
one over the window would silently delete half the history. Per Night, the
Source with stages wins; among equals, the standing Source priority (ADR 0003)
breaks the tie.

## What this milestone does

- **A Catalog entry** `{"sleep", "min", DurationByState}`. Minutes, like
  `apple_exercise_time` and `apple_stand_time`; the client formats `7h 12m`.
- **An interval-aware aggregation** in `internal/query`, beside `aggregate`'s
  existing rules: sum each interval's duration per Night per Stage, over the
  `states` table, clipped to the requested window. It is the fourth branch of a
  switch that already names it, not a parallel engine.
- **A breakdown on the Point**: `states map[string]float64` (minutes per Stage),
  `omitempty` so no existing Metric's payload changes by a byte. `value` stays
  the one scalar every existing consumer reads — the Ledger, the delta, the
  tooltip, the summary — and for sleep it is time asleep.
- **`Nights` on the Series**: the count of Nights with any sleep data in the
  window, set only for a `duration_by_state` Metric. It is the honest
  denominator for "per night", exactly as `Days` is the honest denominator for a
  `sum` Metric's per-day figure, and for the same reason: a window of 90 days
  holding 61 nights of data must not divide by 90.
- **The Panel summary unchanged in rule**: ADR 0019 holds — one bucket spanning
  the whole range — so the summary is total time asleep over the window with its
  own Stage breakdown, and the client renders `total ÷ Nights` as the headline.
  No special case in the engine, and the Baseline delta keeps working because
  `Compare` goes through `Series` (`internal/query/baseline.go:19`).
- **The stacked bar**, at last: when sleep is a Panel's **sole** Metric, its bars
  are decomposed into Stages, bottom to top deep → core → REM → awake, on the
  Palette's categorical ramp. The tooltip lists every Stage with its duration
  and the night's total.
- **A plain bar in a cross-metric Panel.** With another Metric beside it, sleep
  renders as one bar of time asleep in its position colour. A stack is a
  decomposition, and a decomposition can only own the colour ramp when it owns
  the Panel — otherwise ADR 0020's "colour by position in the Panel" stops
  being true the moment a sleep Metric joins.
- **The Ledger lists sleep.** The overview's row set comes from
  `Measurements.DistinctMetrics`, which by construction cannot see a State, so
  sleep would be missing from the one page that promises the numbers behind the
  curves. The States store gains the read that fixes it, and sleep's week/month
  figures are per-night means.
- **Stage columns in the Ledger detail table.** One column per Stage present,
  branching on the aggregation exactly as `isAverage` already branches min/max
  columns in (`web/src/components/ledger-detail-table.tsx:44`). The stack's
  numbers must be readable and copyable, not trapped in a tooltip.
- **Two guards**, both cheap, both preventing a nonsense that is now reachable:
  the Manual entry dialog and `POST /v1/measurements` reject a
  `duration_by_state` Metric — a typed sleep row would land in `measurements`
  where the sleep read path would never look at it again — and a Formula operand
  may not be a `duration_by_state` Metric, checked at Catalog build time beside
  the existing formula checks.

## What this milestone does NOT do

- **No sleep page.** Sleep gets the Metric page every Metric has, reached from a
  Panel title, a legend entry or a Pin. A dedicated page would be a Dashboard
  nobody can arrange.
- **No hypnogram.** The stage-by-stage shape of a single night, on a time-of-day
  axis, is a different chart answering a different question ("how was last
  night") from the one every Panel answers ("how has this been trending"). It
  needs an intra-day axis the API deliberately does not serve (ADR 0012 caps
  resolution at the day). Wanted, not here.
- **No sleep onset, wake time, or efficiency.** Bedtime drift and
  asleep ÷ in-bed are derived questions worth answering once the durations are
  on screen and trusted. They are also not expressible as the current Formula
  shape (a ratio of weighted sums of *Metrics*), so they would drag ADR 0014
  into this milestone.
- **No `stand` states.** The same `states` table holds stand hours, and the
  aggregation being written would serve them. They stay unread: a stand hour is
  a flag, not a duration, and `apple_stand_time` already covers the question in
  minutes as an ordinary Measurement.
- **No overlap merging inside one Source.** Two overlapping rows from the same
  Source are summed, not merged. That is the same limitation the roadmap already
  records for Sources ("merging sources rather than only ranking them") and it
  does not become a different problem here.
- **No manual sleep entry.** Blocked, per the guard above, rather than
  half-supported. Typing a night is a real want; it needs its own writable path
  into `states` and its own Manual-overlay-at-Night-grain rule, which is a
  milestone, not a checkbox.
- **No workouts.** The Sessions read path is the roadmap's next item and shares
  nothing with this one but the sentence that introduced them both.

## Docs

- **ADR 0027**, whose decision is the pair the milestone rests on: sleep is a
  Catalog Metric served by the existing series endpoint, and its bucket is the
  noon-anchored Night rather than the calendar day. Record the rejected
  alternatives: a `/v1/sleep` endpoint with a bespoke page; splitting intervals
  at midnight so a night counts in two days; attributing an interval by its end
  instant (which scatters a fragmented night across two buckets); whole-window
  Source ranking (which deletes the nights the winning Source missed); a
  per-family `Recording`-style model for States.
- **CONTEXT.md entries** for **Night** and **Stage** with their `_Avoid_` lists,
  under a Sleep heading in the data-families section, plus a line on the
  existing **State** entry pointing at them now that the family is read.
- **README and ROADMAP**: sleep moves from "Next" to "Shipped", and the feature
  list gains it. The roadmap's own framing of the gap goes away with it.

## Issues

1. `01-sleep-metric-and-night-aggregation`, catalog and query: the Catalog
   entry, the Night grain, the Stage resolution, `Point.States`,
   `Series.Nights`, the summary, the ADR and the CONTEXT.md entries.
2. `02-sleep-across-the-api`, data and api: the States store read the Ledger
   needs, the sleep row in the overview, and the two guards.
3. `03-web-stacked-sleep`, web: the stacked bar and its tooltip, the duration
   format, the Stage columns in the Ledger detail, the per-night headline.
