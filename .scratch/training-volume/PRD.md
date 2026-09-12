# PRD: Training volume

## Goal

The Workouts milestone kept every statistic Apple reports and built the entity
side of it: a filterable list, a detail view, a map with its profiles. ADR 0028
was explicit that a Session appears on no Panel, and it was right: a workout has
identity, so it gets a list and a page rather than a bucket.

The consequence nobody arranged is that **the aggregate side is missing
entirely**. "How much did I run this month", "did my cycling hours drop while my
strength sessions rose", "what happened to my volume during that phase" are all
questions about a time axis, and a Dashboard cannot ask any of them. The answer
is in `sessions`, already imported, already summed per workout, and reachable
only by reading a list and doing the arithmetic yourself.

The Workouts page has its own range and its own named totals, which answers "how
much, over this stretch". It cannot answer "how much, bucket by bucket, against
everything else on the Dashboard", because that is what a Panel is for.

## The concept

**Training volume is a Metric over Sessions, not a Session on a Panel.** ADR 0028
holds: a Session keeps its identity, its page and its list, and no bar on a chart
is a workout. What a Panel carries is a *fold* of the Sessions in a bucket, which
has no identity, no route and no detail view, exactly like a bucket of steps. The
tension is only apparent: a Metric read from the Sessions family is the same move
sleep already made from the States family (ADR 0027), and nobody thinks a sleep
bar is a stage.

**Two Metrics, not three.** `training_time` in minutes and `training_distance` in
kilometres, both `sum` folded per bucket. Energy is deliberately absent:
`active_energy_burned` already answers it for the whole day, a workout's energy is
a subset of that same expenditure, and putting both on a Dashboard invites an
Account to add them together. The one place a workout's energy belongs is the
workout, where it already is.

**The breakdown is the Activity, capped at the ramp.** A Point carries its total
and a per-Activity breakdown, the same shape a Night carries per Stage, so the
Panel renders one stacked bar per bucket. An Activity is open by construction
(ADR 0011) and Apple ships about eighty, so the read **keeps the five largest by
volume over the window and folds the rest into `other`**: six segments, which is
exactly the long ramp's length (ADR 0036) and exactly a Night's cardinality. The
cap is computed server-side over the whole window, not per bucket, so a segment
does not change meaning halfway across a chart, and it is server-side for the
same reason the Activity group filter is (ADR 0002): a client that had to
enumerate slugs would silently drop every activity added after it shipped.

**`sum_by_state` is the aggregation, beside `duration_by_state`.** The existing
rule names a duration, and `training_distance` is kilometres. Rather than lie in
the wire contract, the Catalog gains a second breakdown rule with the same
Point shape and a different unit story. The client stacks on *having states* and
formats on the unit, so the two rules render through one path.

**A bucket is the day the workout started.** No noon shift: a Night straddles
midnight by nature and a workout does not, so `date(start_at)` is the whole rule.
A workout that runs through midnight counts in the day it began, which is the day
its owner would name.

**Training volume**:
The aggregate of an Account's **Sessions** over a time axis, read as a **Metric**:
minutes or kilometres per bucket, broken down by **Activity**. It is the
aggregate side of the same data the Workouts page lists, and it carries none of a
Session's identity.
_Avoid_: Training load (it names a computed stress model, TSS or ACWR, which
Verve does not compute and will not, for the same reason it does not interpret),
Load, Volume unqualified (it collides with audio exposure), Activity time (it
reads as one Activity's), Workout metrics (Session stat already has that job).

## What this milestone does

- **Two Catalog entries**, `{"training_time", "min", SumByState}` and
  `{"training_distance", "km", SumByState}`, in their own section beside sleep's.
  Both are `Imported` in Nature: they are folded from rows a Connector wrote, not
  computed from a Formula.
- **`catalog.SumByState`**, a fourth Aggregation. `duration_by_state` stays sleep's
  and is not renamed: it is the honest name for minutes per Stage, and renaming it
  would churn the wire contract, `types.ts`, the sleep tests and ADR 0027's prose
  to save one enum value.
- **`internal/query/training.go`**, beside `sleep.go` for the same reason that file
  exists: same Series out, different storage in. One query reads the window's
  Sessions (`start_at`, `activity_type`, `duration`, `total_distance`, `source`),
  Go folds them into buckets and applies the cap. The measurement engine, the
  Source filter and the Manual overlay stay untouched.
- **The `Series.Source` says what it read.** As with sleep, the Sources behind the
  figure are reported rather than silently summed away.
- **A Session with no distance contributes nothing to `training_distance`** and
  still contributes its minutes. A strength session has no kilometres, and a zero
  would be a lie about a workout that happened.
- **`HasSessions`** on `SessionModel`, mirroring `HasStates`: the Ledger's row set
  comes from `DistinctMetrics` over measurements, which by construction cannot see
  a Metric read from another family (the shape ADR 0027 already needed).
- **The Ledger, the Metric page and the Panel get them for free**, because they are
  ordinary Metrics: one row, one page, one Panel option, the same time axis. The
  Series CSV also gets them free, and correctly: `csvRows` already emits one row
  per state, so a download reads `training_time.running`.
- **Refusals where the family cannot answer.** An Exclusion names a Measurement
  (ADR 0033) and a Manual entry writes one (ADR 0022); both refuse a
  `sum_by_state` slug with a 422 that says which, exactly as they refuse `sleep`.
  A derived Metric may not take one as an operand, which the Formula validator
  already enforces for `duration_by_state` and now covers both.
- **The web stacks any Series that carries states.** The sleep-specific branch
  (`lib/sleep.ts`'s `isDurationByState`, `stagesPresent`, `stageLabel`) generalizes
  to "a Series with a breakdown", with the labels coming from the Activity catalog
  the Workouts page already uses (`lib/activities.ts`) and the colours from the
  long ramp the Stages already read (ADR 0036). `other` takes the sixth slot.
- **A Panel summary is the window's total**, folded like any `sum`: hours over the
  range, not a mean of buckets.

## What this milestone does NOT do

- **No `training_energy`.** See the concept. `active_energy_burned` answers it, and
  a second figure over the same calories is how a Dashboard starts contradicting
  itself.
- **No per-Session drill-down from a bar.** A bar is a fold and has no identity;
  the Workouts list, filtered by the same activity over the same range, is the
  entity view and it already exists. A Panel that navigated to a list would be
  ADR 0028 reopened from the other end.
- **No per-Activity Metric** (`running_distance` and seventy-nine siblings). The
  Catalog is closed and curated (ADR 0002); the Activity set is open by
  construction (ADR 0011). Multiplying one by the other is how a closed set stops
  being one. The breakdown answers the same question inside one Metric.
- **No Source election between Sessions.** Measurements resolve per day (ADR 0034)
  because two devices count the same steps; no shipped Connector emits workouts
  twice, since Google Takeout has none. The day a second Connector does, two
  Sources recording one run will double the volume, and the fix is the day-grain
  election that already exists next door. It is named here rather than built,
  and the reported Source is what makes the problem visible instead of silent.
- **No pace, speed or heart rate per bucket.** A mean pace over a month is an
  average of averages weighted by nothing, and the Session stats hold the honest
  per-workout figures. Nothing stops a later Metric from being defined properly;
  this milestone does not guess one.
- **No cap the Account can change.** Five plus `other` is the ramp's length, not a
  preference, and a slider over it is a setting that has to be explained on every
  screen it touches.
- **No new storage, no migration.** Every figure is a column `sessions` already
  carries.

## Ordering

`01` is the Catalog, the read path and the ADR: after it, `/v1/series?metric=
training_time` answers and the CSV downloads. `02` is the API's refusals and the
Ledger's row set, which need `01`'s Catalog entries to refuse anything. `03` is the
rendering, which needs a Series to draw. `02` and `03` are independent of each
other.

## Docs

- **A new ADR** (`0040`): training volume is a Metric over Sessions, the Activity
  is the breakdown, and the breakdown is capped at the ramp. It is the sequel to
  ADR 0028 and says why reading Sessions as a Metric does not reopen it, records
  the `sum_by_state` addition beside `duration_by_state`, and lists what was turned
  down: a Metric per Activity, `training_energy`, an uncapped stack, a per-bucket
  cap, drill-down from a bar, and Source election between Sessions.
- **A CONTEXT.md entry** for **Training volume**, in the Data families section
  beside **Session** and **Session stat**, with its `_Avoid_` list. **Activity**
  gains one sentence: it is also a breakdown dimension, not only a filter and a
  label.
- **ADR 0028** gains a forward pointer: "a Session appears on no Panel" is still
  true, and what appears there is an aggregate that is not a Session.
- **README.md**: a bullet under "Look at it properly", beside the workouts one.
- **ROADMAP.md**: a shipped row, and the "Training volume" entry leaves Later.

## Issues

1. `01-training-metrics-and-the-sessions-read`, catalog and query: `SumByState`,
   the two Catalog entries, `internal/query/training.go` with the fold and the cap,
   `HasSessions`, the ADR and the CONTEXT.md entry.
2. `02-refusals-and-the-ledger-row`, api and data: the 422s on Exclusions and
   Manual entry for a `sum_by_state` slug, the Ledger row set seeing the two new
   Metrics, and the CSV's per-Activity rows pinned by test.
3. `03-web-any-series-can-stack`, web: the sleep-specific stack generalized to any
   Series carrying states, Activity labels and the long ramp, the Metric page and
   the Panel, and the `other` slot.
