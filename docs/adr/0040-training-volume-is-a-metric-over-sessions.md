# Training volume is a Metric over Sessions, capped at the ramp

## Context

ADR 0028 settled what a workout is: an entity. It has identity, so it gets a
list, a detail view and a map, and it appears on no Panel. That was right and it
still is.

What it left behind is the other half of the same data. "How much did I run this
month", "did my cycling hours fall while my strength sessions rose", "what
happened to my volume during that cut" are questions about a time axis, and a
Dashboard could not ask any of them. The figures are already imported: a Session
carries its duration and, when the source reported one, its distance. Reading
them meant opening the Workouts list and doing the arithmetic by eye.

The Workouts page has a range of its own and totals it names rather than sums
from the page (ADR 0028), which answers "how much, over this stretch". It cannot
answer "how much, bucket by bucket, beside everything else on the Dashboard",
because that is what a Panel is.

## Decision

**Training volume is a Metric over Sessions, not a Session on a Panel.** Two
Catalog entries, `training_time` in minutes and `training_distance` in
kilometres, folded per bucket from the Sessions family. ADR 0028 holds
unchanged: a Session keeps its identity, and a bar on this chart is not a
workout but a fold of the workouts in a bucket, with no identity, no route and
no detail view. It is the same move sleep made from the States family
(ADR 0027), and nobody mistakes a sleep bar for a stage.

**The breakdown is the Activity, capped at five plus `other`.** A Point carries
its total and a per-Activity breakdown, the shape a Night already uses for its
Stages, so a Panel draws one stacked bar per bucket. The Activity set is open by
construction (ADR 0011) and Apple ships about eighty, so the read keeps the five
largest by volume and folds the rest into `other`: six segments, the long ramp's
length (ADR 0036) and a Night's cardinality.

**The cap is decided once over the window, and never changes a total.** Per
bucket, a segment would mean "the largest that week" and would be a different
quantity on every bar. And the capped-away Activities still count towards every
Point's value: the cap is how the bar is drawn, not what it says.

**`sum_by_state` joins `duration_by_state` rather than replacing it.** The
existing rule is the honest name for minutes per Stage, and kilometres are not a
duration. A second rule with the same Point shape costs an enum value; renaming
the first would churn the wire contract, `types.ts`, the sleep tests and
ADR 0027's prose. `Aggregation.ByState()` is what every caller asks, so no
branch can learn about one rule and forget the other.

**A bucket is the day the workout started.** A Night is dated by the morning it
wakes into because it straddles midnight by nature (ADR 0027). A workout that
runs through midnight is one its owner names by the evening it began, so
`date(start_at)` is the whole rule and there is no shift.

**No Source election between Sessions, yet.** Measurements resolve per day
(ADR 0034) because two devices count the same steps. No shipped Connector emits
a workout twice, since a Takeout carries none, so there is nothing to resolve.
The Series reports the Sources it read, which is what will make a doubled volume
visible the day a second Connector changes that.

## Why

- **The Catalog stays closed and the Activity set stays open.** A Metric per
  Activity (`running_distance` and seventy-nine siblings) would multiply one by
  the other, and the closed set would stop being one. One Metric with a
  breakdown answers the same question and leaves both properties intact.
- **The cap belongs in the read.** ADR 0002 already argues this for the Activity
  group filter: a client that enumerates slugs to decide what to show silently
  drops every Activity added after it shipped.
- **The total must not depend on the display.** If the cap dropped rows, a bar
  would answer a different question from the Metric's own summary, and the
  legend would be the only place the difference was visible.
- **One more family, the same Series.** `sleep.go` established that a Metric can
  be read from another table without the measurement engine learning about it.
  A second such family is the test of that, and it passed: the Source filter,
  the Manual overlay and the per-bucket SQL were not touched.

## Considered Options

- **A Metric per Activity.** Rejected: see above. It also breaks the Ledger,
  whose row set would grow by every activity an Account ever recorded.
- **`training_energy`.** Rejected. `active_energy_burned` already answers it for
  the whole day, a workout's energy is a subset of that same expenditure, and two
  figures over the same calories on one Dashboard is an invitation to add them.
  The place a workout's energy belongs is the workout, where it already is.
- **Renaming `duration_by_state` to `sum_by_state` and using one rule.** Tempting
  and rejected: the churn is in four places and the saving is one constant, and
  "duration by state" remains the true description of what sleep does.
- **Capping per bucket instead of per window.** Rejected: a segment that means
  something different on each bar is not a breakdown, it is a ranking drawn as
  one.
- **An uncapped stack.** Rejected: eighty segments is a legend nobody reads and
  colours repeating around a six-slot ramp.
- **A cap the Account can set.** Rejected: it is a setting that must be explained
  on every screen it touches, to tune a number whose right value is the ramp's
  length.
- **Drill-down from a bar to the workouts behind it.** Deferred, and not
  obviously wanted. A bar is a fold with no identity; the Workouts list filtered
  by the same activity over the same range is the entity view and it exists. A
  Panel that navigated into it would reopen ADR 0028 from the other end.
- **Pace, speed or heart rate per bucket.** Rejected for now: a mean pace over a
  month is an average of averages weighted by nothing. The Session stats hold the
  honest per-workout figures, and a properly defined Metric can be added later
  without guessing one here.
- **Electing a Source per day for Sessions.** Deferred with its trigger named
  above, the same posture ADR 0034 took before a second export existed.

## Consequences

- `internal/query/training.go` joins `sleep.go` as a read path that is not
  measurement-shaped. `Series` dispatches on the aggregation, which is now a
  two-case switch rather than an `if`.
- `Point.Count` on a training Point is the number of Sessions behind the bucket,
  as it is nights for sleep. A Session with no figure for the Metric asked (a
  strength session under `training_distance`) is not counted: it is not evidence
  of a distance.
- Every caller that special-cased `duration_by_state` now asks
  `Aggregation.ByState()`: the Formula fence, the Exclusion and Manual-entry
  refusals, the Ledger's per-bucket divisor, the default chart type and the
  `stacked_bar` validation.
- The Ledger's row set needs a `hasSessions` probe beside the `hasStates` one,
  because a `DISTINCT` over measurements structurally cannot see either family.
- `TestCatalogHasNoOrphan` gains a `familyBacked` exemption. The two slugs are
  claimed by no mapping table because no source type maps to them; what writes
  them is a workout import, one level up from a slug.
- The Series CSV needed nothing: `csvRows` already emits one row per breakdown
  key, so a download reads `training_time.running`.
