# A Goal is a declared daily bound, and Verve only counts

_Amended by ADR 0048: a `latest` Metric is now eligible, judged on the day's
last reading, with no carry-forward._

## Context

The only targets Verve knows are the Plan's: a calorie and a protein figure
derived on read from a Phase's Target rate and the day's Expenditure estimate
(ADR 0023). There is nothing for "7 500 steps", "7 h of sleep" or "150 g of
protein", which is the first thing a person expects of a health dashboard: a
line on the Panel and a count of how often it was met.

Verve has refused this until now for a reason it states in three places (the
ROADMAP, the Panel summary, the Co-variation): it does not colour a change good
or bad, because it cannot know which direction is good for a Metric. A Goal
answers that objection exactly. The owner declares the direction; Verve does not
learn it, infer it or suggest it. What is left for Verve to do is count.

Four questions had an obvious answer that is wrong:

- **At which grain is a Goal judged?** The obvious answer is the Panel's bucket.
  But "7 500 steps" is a daily bound: a weekly sum against 7 500 is meaningless,
  and against 52 500 it answers a different question ("did the week add up?")
  from the one asked ("how many days did I get there?").
- **What happens to the past when a Goal changes?** The obvious answer is one
  mutable value. Then raising 7 000 to 7 500 rewrites March's count, and March
  has not changed.
- **Is a Plan target a Goal?** The obvious answer is yes, it is a number on a
  Metric. But a Plan target is recomputed on every read from today's estimate and
  is stored nowhere, so which target was in force on 3 March cannot be answered,
  and a count that cannot answer it is not honest.
- **What does a day without data count as?** The obvious answer is a miss. But a
  day at zero steps because the phone stayed on the table is not a day that fell
  short.

## Decision

**A Goal is a bound the owner declares on a Metric: a direction, a value, and
the date it starts.** It is judged against the Metric's **day bucket**, which is
the Night for `sleep` (ADR 0027), whatever bucket the Panel is drawn at. Weekly
Goals are out of scope; the grain is fixed at day and could become a declared
field later without reshaping anything.

**Two directions, both inclusive: `at_least` (≥) and `at_most` (≤).** A range
("7 to 9 h") would be a third direction carrying two values, never two Goals on
one Metric.

**Every Metric is eligible except a `latest` one, and the bound is on the
Point's value.** Eligibility is derived from the aggregation rule, not listed,
so a new Catalog entry needs no decision. A `latest` Metric is excluded on
meaning rather than mechanics: "75 kg" is a destination reached once, not a
bound held daily, and the question "am I heading there?" is the Phase's. A
by-state Metric is bounded on its total (time asleep, training time), never on
one State. A derived Metric is bounded like any other, including a negative
value (`calorie_balance ≤ −300`).

**Goals are a dated history, never overwritten.** At most one Goal is open per
(Account, Metric), enforced by a partial unique index as Phases are. Setting a
new Goal closes the open one; each day is judged against the Goal in force on
that day. A Goal is in force on `[started_on, ended_on)`, and its bounds are
**dates, not instants**: a Goal judges days, so an instant would only raise the
question of which Goal applies to the day it changed at 14:00. A new Goal must
start after the open one and after the last one closed, so segments never
overlap; the past is corrected by deleting an entry, not by inserting into it.

**Attainment is three counts, never a percentage alone.** Over a window:
**covered** days (a Goal was in force), **measured** days (covered, with a
value), **met** days (measured, and within the bound in force that day). A gap
is neither met nor missed (ADR 0014). **Today is never judged**: 3 000 steps at
noon is not a miss of `≥ 7 500`, and 800 mg of sodium before dinner is not a
success of `≤ 2 300`. The rule has no exception, not even for a Night that is
complete by morning.

**Attainment travels on the Series, computed server-side** from the day-grain
values the engine already resolves, so the Source election (ADR 0034), the
Manual overlay and Exclusions (ADR 0033) apply without a second rule. It carries
the Goal segments in force over the window, which are what the line is drawn
from. A Baseline carries its own Attainment, against the Goals in force over
*its* window.

**Plan targets are not Goals.** The Panel draws declared Goals only; the Plan
page is unchanged. Both may coexist on `dietary_protein`, and if they disagree,
that is a disagreement between what the owner decided and what Verve computes,
shown as such.

**No valence anywhere.** The line is a dashed step in the Series' own colour on
its own axis. Points are not recoloured, the area beyond the line is not shaded,
nothing is ticked, there is no streak and nothing is pushed. The counts sit in
the legend beside the Panel summary. Above day grain there is no line, only the
counts. The value axis extends to include the Goal, because a line clipped out
of view reads as no Goal at all.

## Why

- **The owner supplies the direction, so the ROADMAP's objection is met, not
  bypassed.** Verve still judges nothing. It compares a number with a number the
  owner wrote down, on the day the owner said it applied.
- **Counting days is more honest than a mean.** A 150 g protein mean can hide
  half the days at 80 g. The existing Adherence compares a mean with a target
  because a rate of body-mass change is a mean; a daily bound is not.
- **The dated history is the Phase's reasoning, one grain down.** "Was I doing
  what I meant to be doing?" has to stay answerable about the past, and it only
  does if the past keeps the Goal it had.
- **The denominator is shown because a gap is not a zero.** "18 of 24 measured
  days (30)" says what happened; "75 %" hides whether the missing quarter was
  missed or unmeasured.
- **One read path.** Attainment computed from anything other than the engine's
  own day buckets would be a second definition of a day's value, waiting to
  disagree with the bar drawn above it.

## Considered Options

- **Judging at the Panel's bucket.** Rejected: it changes what the Goal means
  every time the bucket changes.
- **A declared grain (`day | week`) now.** Deferred: "150 min of activity a
  week" is real, but it doubles every rule (the line, the denominator, a partial
  week at the edge of the window) for a first version.
- **A single mutable Goal per Metric.** Rejected: it rewrites past counts.
- **Goals on `latest` Metrics.** Rejected: a count of weighed days under 75 kg
  answers a question nobody asked, and the one they ask is a Phase's.
- **A `between` direction now.** Deferred: most self-set Goals have one bound,
  and adding it later is a third direction, not a new shape.
- **Deriving Goals from the Plan, or a button that copies a Plan target into a
  Goal.** Rejected: the first needs the targets historised per day, which is a
  rebuild of Adherence; the second is a frozen copy that looks like a live
  target.
- **A separate `GET /v1/goals/attainment` joined client-side.** Rejected for the
  reason ADR 0027 refused a sleep endpoint: two reads that must agree on the
  window, the Source and the Night.
- **Colour, a tick, a streak, a notification.** Rejected, and named here because
  a Goal is precisely where every other health app puts them. The owner declared
  a direction, not a wish to be graded.
- **Goals scoped to a Panel.** Rejected: a Goal belongs to the Metric and the
  Account. Editing it from a Panel would suggest it is the Panel's own.

## Consequences

- A `goals` table, a model mirroring `PhaseModel`, and routes mirroring the
  Phases': `GET /v1/goals`, `POST /v1/goals`, `PATCH /v1/goals/{id}` to close,
  `DELETE /v1/goals/{id}`. Not under `/v1/metrics`, which is the public Catalog.
- Attainment needs day-grain values over windows longer than `maxPoints` days
  (an `all` range of eight years), so the count is folded over chunks rather
  than lifting the cap.
- A Goal is set on the Metric page only. The Day page shows the bound in force
  beside the value, as a fact, without a verdict. The Ledger gains a Goal column
  at day grain. Cross-metric ignores Goals.
- **Goal** and **Attainment** enter CONTEXT.md, and the Phase and Target rate
  entries keep "Goal" among their avoided terms with a pointer to its meaning.
